package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrGone     = errors.New("gone")
)

type Store struct{ pool *pgxpool.Pool }

func OpenStore(ctx context.Context, url string) (*Store, error) {
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return &Store{pool: pool}, nil
			}
			pool.Close()
		}
		lastErr = err
		log.Printf("database not ready (attempt %d): %v", attempt, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("connect database: %w", lastErr)
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Migrate(ctx context.Context, schema string) error {
	_, err := s.pool.Exec(ctx, schema)
	return err
}

// ---------- locations ----------

type Location struct {
	ID        int64     `json:"id"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	PanoID    string    `json:"panoId"`
	Heading   float64   `json:"heading"`
	Address   string    `json:"address"`
	Feedback  int       `json:"feedback"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
	Plays     *int64    `json:"plays,omitempty"`
}

type LocationInput struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	PanoID  string  `json:"panoId"`
	Heading float64 `json:"heading"`
	Address string  `json:"address"`
}

const locationCols = `id, lat, lng, pano_id, heading, address, feedback, source, created_at`

type rowScanner interface{ Scan(dest ...any) error }

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanLocation(row rowScanner, extra ...any) (Location, error) {
	var l Location
	dest := append([]any{&l.ID, &l.Lat, &l.Lng, &l.PanoID, &l.Heading, &l.Address, &l.Feedback, &l.Source, &l.CreatedAt}, extra...)
	err := row.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

func getLocation(ctx context.Context, q querier, id int64) (Location, error) {
	return scanLocation(q.QueryRow(ctx, `SELECT `+locationCols+` FROM locations WHERE id = $1`, id))
}

func (s *Store) GetLocation(ctx context.Context, id int64) (Location, error) {
	return getLocation(ctx, s.pool, id)
}

// RandomAdminLocation 은 관리자 지정 위치 중 하나를 무작위로 반환. 없으면 ErrNotFound.
func (s *Store) RandomAdminLocation(ctx context.Context) (Location, error) {
	return scanLocation(s.pool.QueryRow(ctx,
		`SELECT `+locationCols+` FROM locations WHERE source = 'admin' ORDER BY random() LIMIT 1`))
}

func (s *Store) InsertLocation(ctx context.Context, in LocationInput, source string) (Location, error) {
	return scanLocation(s.pool.QueryRow(ctx,
		`INSERT INTO locations (lat, lng, pano_id, heading, address, source)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+locationCols,
		in.Lat, in.Lng, in.PanoID, in.Heading, in.Address, source))
}

func (s *Store) ListLocations(ctx context.Context, source string, limit int) ([]Location, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+locationCols+`,
		        (SELECT count(*) FROM game_records g WHERE g.location_id = l.id AND g.finished_at IS NOT NULL)
		   FROM locations l
		  WHERE ($1 = '' OR source = $1)
		  ORDER BY created_at DESC
		  LIMIT $2`, source, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Location{}
	for rows.Next() {
		var plays int64
		l, err := scanLocation(rows, &plays)
		if err != nil {
			return nil, err
		}
		l.Plays = &plays
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) DeleteLocation(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- games ----------

type Game struct {
	ID        int64     `json:"gameId"`
	StartedAt time.Time `json:"startedAt"`
}

func (s *Store) CreateGame(ctx context.Context, locationID int64) (Game, error) {
	var g Game
	err := s.pool.QueryRow(ctx,
		`INSERT INTO game_records (location_id) VALUES ($1) RETURNING id, started_at`, locationID).
		Scan(&g.ID, &g.StartedAt)
	return g, err
}

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type GuessResult struct {
	GameID    int64    `json:"gameId"`
	DistanceM float64  `json:"distanceM"`
	DurationS float64  `json:"durationS"`
	Guess     LatLng   `json:"guess"`
	Location  Location `json:"location"`
	ScoreBreakdown
}

func (s *Store) FinishGame(ctx context.Context, id int64, guess LatLng) (GuessResult, error) {
	var res GuessResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)

	var (
		finished *time.Time
		locID    *int64
		duration float64
	)
	err = tx.QueryRow(ctx,
		`SELECT finished_at, location_id, EXTRACT(EPOCH FROM (now() - started_at))::float8
		   FROM game_records WHERE id = $1 FOR UPDATE`, id).Scan(&finished, &locID, &duration)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, ErrNotFound
	} else if err != nil {
		return res, err
	}
	if finished != nil {
		return res, ErrConflict
	}
	if locID == nil {
		return res, ErrGone
	}
	loc, err := getLocation(ctx, tx, *locID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return res, ErrGone
		}
		return res, err
	}

	dist := HaversineMeters(loc.Lat, loc.Lng, guess.Lat, guess.Lng)
	duration = max(duration, 0)
	sc := ComputeScore(dist, duration)

	if _, err = tx.Exec(ctx,
		`UPDATE game_records
		    SET finished_at = now(), guess_lat = $2, guess_lng = $3,
		        distance_m = $4, duration_s = $5, score = $6
		  WHERE id = $1`, id, guess.Lat, guess.Lng, dist, duration, sc.Score); err != nil {
		return res, err
	}
	if err = tx.Commit(ctx); err != nil {
		return res, err
	}
	return GuessResult{GameID: id, DistanceM: dist, DurationS: duration, Guess: guess, Location: loc, ScoreBreakdown: sc}, nil
}

// AbandonGame 은 아직 끝나지 않은 게임 기록을 삭제한다 (게임 종료 버튼).
func (s *Store) AbandonGame(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM game_records WHERE id = $1 AND finished_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SubmitFeedback 은 게임당 1회 피드백을 기록하고 위치의 feedback 누적값(+1/-1)을 갱신한다.
func (s *Store) SubmitFeedback(ctx context.Context, gameID int64, value int) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var locID *int64
	err = tx.QueryRow(ctx,
		`UPDATE game_records SET feedback = $2
		  WHERE id = $1 AND finished_at IS NOT NULL AND feedback IS NULL
		  RETURNING location_id`, gameID, value).Scan(&locID)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM game_records WHERE id = $1)`, gameID).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, ErrNotFound
		}
		return 0, ErrConflict
	} else if err != nil {
		return 0, err
	}

	total := 0
	if locID != nil {
		err = tx.QueryRow(ctx,
			`UPDATE locations SET feedback = feedback + $2 WHERE id = $1 RETURNING feedback`, *locID, value).Scan(&total)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	return total, tx.Commit(ctx)
}

// ---------- records ----------

type Record struct {
	ID         int64     `json:"id"`
	LocationID *int64    `json:"locationId"`
	Address    *string   `json:"address"`
	Source     *string   `json:"source"`
	LocLat     *float64  `json:"locationLat"`
	LocLng     *float64  `json:"locationLng"`
	GuessLat   float64   `json:"guessLat"`
	GuessLng   float64   `json:"guessLng"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	DistanceM  float64   `json:"distanceM"`
	DurationS  float64   `json:"durationS"`
	Score      float64   `json:"score"`
	Feedback   *int16    `json:"feedback"`
}

type RecordStats struct {
	Games        int64   `json:"games"`
	AvgScore     float64 `json:"avgScore"`
	BestScore    float64 `json:"bestScore"`
	AvgDistanceM float64 `json:"avgDistanceM"`
	AvgDurationS float64 `json:"avgDurationS"`
}

func (s *Store) ListRecords(ctx context.Context, limit int) ([]Record, RecordStats, error) {
	var st RecordStats
	err := s.pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(avg(score), 0), COALESCE(max(score), 0),
		        COALESCE(avg(distance_m), 0), COALESCE(avg(duration_s), 0)
		   FROM game_records WHERE finished_at IS NOT NULL`).
		Scan(&st.Games, &st.AvgScore, &st.BestScore, &st.AvgDistanceM, &st.AvgDurationS)
	if err != nil {
		return nil, st, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT g.id, g.location_id, l.address, l.source, l.lat, l.lng,
		        g.guess_lat, g.guess_lng, g.started_at, g.finished_at,
		        g.distance_m, g.duration_s, g.score, g.feedback
		   FROM game_records g
		   LEFT JOIN locations l ON l.id = g.location_id
		  WHERE g.finished_at IS NOT NULL
		  ORDER BY g.finished_at DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, st, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.LocationID, &r.Address, &r.Source, &r.LocLat, &r.LocLng,
			&r.GuessLat, &r.GuessLng, &r.StartedAt, &r.FinishedAt,
			&r.DistanceM, &r.DurationS, &r.Score, &r.Feedback); err != nil {
			return nil, st, err
		}
		out = append(out, r)
	}
	return out, st, rows.Err()
}
