package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrGone     = errors.New("gone")
)

type Store struct{ db *gorm.DB }

func OpenStore(ctx context.Context, dsn string) (*Store, error) {
	gormLogger := logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
		SlowThreshold:             500 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
	})

	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger:               gormLogger,
			DisableAutomaticPing: true, // 재시도 루프에서 직접 Ping 한다
		})
		if err == nil {
			sqlDB, derr := db.DB()
			if derr != nil {
				return nil, derr
			}
			if err = sqlDB.PingContext(ctx); err == nil {
				sqlDB.SetMaxOpenConns(10)
				sqlDB.SetMaxIdleConns(5)
				sqlDB.SetConnMaxLifetime(time.Hour)
				return &Store{db: db}, nil
			}
			_ = sqlDB.Close()
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

func (s *Store) Close() {
	if sqlDB, err := s.db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// Migrate 는 schema.sql 을 그대로 실행한다.
// (CHECK 제약, ON DELETE SET NULL 등을 정확히 유지하기 위해 AutoMigrate 대신 DDL 을 사용)
func (s *Store) Migrate(ctx context.Context, schema string) error {
	return s.db.WithContext(ctx).Exec(schema).Error
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// ---------- models ----------

type Location struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	PanoID    string    `json:"panoId"`
	Heading   float64   `json:"heading"`
	Address   string    `json:"address"`
	Feedback  int       `json:"feedback"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
	Plays     *int64    `gorm:"->" json:"plays,omitempty"` // 조회 전용 (ListLocations 서브쿼리)
}

type GameRecord struct {
	ID         int64     `gorm:"primaryKey"`
	LocationID *int64    // 위치 삭제 시 NULL
	StartedAt  time.Time `gorm:"default:now()"`
	FinishedAt *time.Time
	GuessLat   *float64
	GuessLng   *float64
	DistanceM  *float64
	DurationS  *float64
	Score      *float64
	Feedback   *int16
}

// ---------- locations ----------

type LocationInput struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	PanoID  string  `json:"panoId"`
	Heading float64 `json:"heading"`
	Address string  `json:"address"`
}

func getLocation(db *gorm.DB, id int64) (Location, error) {
	var l Location
	err := db.Take(&l, id).Error
	return l, notFound(err)
}

func (s *Store) GetLocation(ctx context.Context, id int64) (Location, error) {
	return getLocation(s.db.WithContext(ctx), id)
}

// RandomAdminLocation 은 관리자 지정 위치 중 하나를 무작위로 반환. 없으면 ErrNotFound.
func (s *Store) RandomAdminLocation(ctx context.Context) (Location, error) {
	var l Location
	err := s.db.WithContext(ctx).Where("source = ?", "admin").Order("random()").Take(&l).Error
	return l, notFound(err)
}

func (s *Store) InsertLocation(ctx context.Context, in LocationInput, source string) (Location, error) {
	l := Location{
		Lat:     in.Lat,
		Lng:     in.Lng,
		PanoID:  in.PanoID,
		Heading: in.Heading,
		Address: in.Address,
		Source:  source,
	}
	err := s.db.WithContext(ctx).Create(&l).Error
	return l, err
}

func (s *Store) ListLocations(ctx context.Context, source string, limit int) ([]Location, error) {
	q := s.db.WithContext(ctx).Model(&Location{}).
		Select(`locations.*,
		        (SELECT count(*) FROM game_records g
		          WHERE g.location_id = locations.id AND g.finished_at IS NOT NULL) AS plays`)
	if source != "" {
		q = q.Where("source = ?", source)
	}
	out := []Location{}
	err := q.Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

func (s *Store) DeleteLocation(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Delete(&Location{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
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
	g := GameRecord{LocationID: &locationID}
	if err := s.db.WithContext(ctx).Create(&g).Error; err != nil {
		return Game{}, err
	}
	return Game{ID: g.ID, StartedAt: g.StartedAt}, nil
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
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		type pending struct {
			FinishedAt *time.Time
			LocationID *int64
			Elapsed    float64
		}
		var g pending
		err := tx.Model(&GameRecord{}).
			Select("finished_at, location_id, EXTRACT(EPOCH FROM (now() - started_at))::float8 AS elapsed").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).
			Take(&g).Error
		if err != nil {
			return notFound(err)
		}
		if g.FinishedAt != nil {
			return ErrConflict
		}
		if g.LocationID == nil {
			return ErrGone
		}
		loc, err := getLocation(tx, *g.LocationID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrGone
			}
			return err
		}

		dist := HaversineMeters(loc.Lat, loc.Lng, guess.Lat, guess.Lng)
		duration := max(g.Elapsed, 0)
		sc := ComputeScore(dist, duration)

		err = tx.Model(&GameRecord{ID: id}).Updates(map[string]any{
			"finished_at": gorm.Expr("now()"),
			"guess_lat":   guess.Lat,
			"guess_lng":   guess.Lng,
			"distance_m":  dist,
			"duration_s":  duration,
			"score":       sc.Score,
		}).Error
		if err != nil {
			return err
		}
		res = GuessResult{GameID: id, DistanceM: dist, DurationS: duration, Guess: guess, Location: loc, ScoreBreakdown: sc}
		return nil
	})
	return res, err
}

// AbandonGame 은 아직 끝나지 않은 게임 기록을 삭제한다 (게임 종료 버튼).
func (s *Store) AbandonGame(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Where("finished_at IS NULL").Delete(&GameRecord{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SubmitFeedback 은 게임당 1회 피드백을 기록하고 위치의 feedback 누적값(+1/-1)을 갱신한다.
func (s *Store) SubmitFeedback(ctx context.Context, gameID int64, value int) (int, error) {
	total := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var g GameRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&g, gameID).Error; err != nil {
			return notFound(err)
		}
		if g.FinishedAt == nil || g.Feedback != nil {
			return ErrConflict
		}
		if err := tx.Model(&g).Update("feedback", value).Error; err != nil {
			return err
		}
		if g.LocationID == nil {
			return nil
		}
		var loc Location
		err := tx.Model(&loc).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "feedback"}}}).
			Where("id = ?", *g.LocationID).
			Update("feedback", gorm.Expr("feedback + ?", value)).Error
		total = loc.Feedback
		return err
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// ---------- records ----------

type Record struct {
	ID         int64     `json:"id"`
	LocationID *int64    `json:"locationId"`
	Address    *string   `json:"address"`
	Source     *string   `json:"source"`
	LocLat     *float64  `gorm:"column:location_lat" json:"locationLat"`
	LocLng     *float64  `gorm:"column:location_lng" json:"locationLng"`
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
	db := s.db.WithContext(ctx)

	var st RecordStats
	err := db.Model(&GameRecord{}).
		Select(`count(*) AS games,
		        COALESCE(avg(score), 0) AS avg_score,
		        COALESCE(max(score), 0) AS best_score,
		        COALESCE(avg(distance_m), 0) AS avg_distance_m,
		        COALESCE(avg(duration_s), 0) AS avg_duration_s`).
		Where("finished_at IS NOT NULL").
		Scan(&st).Error
	if err != nil {
		return nil, st, err
	}

	out := []Record{}
	err = db.Table("game_records AS g").
		Select(`g.id, g.location_id, l.address, l.source,
		        l.lat AS location_lat, l.lng AS location_lng,
		        g.guess_lat, g.guess_lng, g.started_at, g.finished_at,
		        g.distance_m, g.duration_s, g.score, g.feedback`).
		Joins("LEFT JOIN locations AS l ON l.id = g.location_id").
		Where("g.finished_at IS NOT NULL").
		Order("g.finished_at DESC").
		Limit(limit).
		Scan(&out).Error
	return out, st, err
}
