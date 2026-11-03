package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Server struct {
	cfg   Config
	store *Store
}

func (s *Server) routes(e *echo.Echo) {
	api := e.Group("/api", middleware.BodyLimit("64K"), noStore)

	api.GET("/config", s.handleConfig)

	api.GET("/games/draw", s.handleDraw)
	api.POST("/games", s.handleCreateGame)
	api.POST("/games/:id/guess", s.handleGuess)
	api.POST("/games/:id/feedback", s.handleFeedback)
	api.DELETE("/games/:id", s.handleAbandon)

	api.GET("/records", s.handleRecords)

	admin := api.Group("/admin")
	admin.GET("/locations", s.handleListLocations)
	admin.POST("/locations", s.handleCreateLocation)
	admin.DELETE("/locations/:id", s.handleDeleteLocation)
}

func (s *Server) handleConfig(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"googleMapsApiKey": s.cfg.MapsAPIKey,
		"adminRatio":       s.cfg.AdminRatio,
	})
}

// handleDraw 는 이번 게임의 출제 방식을 결정한다. (랜덤 : 관리자 지정 = 8 : 2 기본)
// 랜덤인 경우 브라우저가 Street View 서비스로 실제 위치를 찾은 뒤 POST /api/games 로 등록한다.
func (s *Server) handleDraw(c echo.Context) error {
	if rand.Float64() < s.cfg.AdminRatio {
		loc, err := s.store.RandomAdminLocation(c.Request().Context())
		if err == nil {
			return c.JSON(http.StatusOK, map[string]any{"source": "admin", "location": loc})
		}
		if !errors.Is(err, ErrNotFound) {
			log.Printf("draw admin location: %v", err)
		}
		// 관리자 위치가 없으면 랜덤으로 대체
	}
	return c.JSON(http.StatusOK, map[string]any{"source": "random"})
}

func (s *Server) handleCreateGame(c echo.Context) error {
	var req struct {
		LocationID *int64         `json:"locationId"`
		Location   *LocationInput `json:"location"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}

	ctx := c.Request().Context()
	var (
		loc Location
		err error
	)
	switch {
	case req.LocationID != nil:
		loc, err = s.store.GetLocation(ctx, *req.LocationID)
	case req.Location != nil:
		in, verr := normalizeLocation(*req.Location)
		if verr != nil {
			return badRequest(verr.Error())
		}
		loc, err = s.store.InsertLocation(ctx, in, "random")
	default:
		return badRequest("locationId 또는 location 이 필요합니다")
	}
	if err != nil {
		return storeError(err)
	}

	game, err := s.store.CreateGame(ctx, loc.ID)
	if err != nil {
		return storeError(err)
	}
	return c.JSON(http.StatusCreated, map[string]any{
		"gameId":    game.ID,
		"startedAt": game.StartedAt,
		"location":  loc,
	})
}

func (s *Server) handleGuess(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var guess LatLng
	if err := bindJSON(c, &guess); err != nil {
		return err
	}
	if !validLatLng(guess.Lat, guess.Lng) {
		return badRequest("잘못된 좌표입니다")
	}
	res, err := s.store.FinishGame(c.Request().Context(), id, guess)
	if err != nil {
		return storeError(err)
	}
	return c.JSON(http.StatusOK, res)
}

func (s *Server) handleFeedback(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var req struct {
		Value int `json:"value"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Value != 1 && req.Value != -1 {
		return badRequest("value 는 1 또는 -1 이어야 합니다")
	}
	total, err := s.store.SubmitFeedback(c.Request().Context(), id, req.Value)
	if err != nil {
		return storeError(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"locationFeedback": total})
}

func (s *Server) handleAbandon(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.store.AbandonGame(c.Request().Context(), id); err != nil {
		return storeError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleRecords(c echo.Context) error {
	limit := queryInt(c, "limit", 100, 1, 1000)
	records, stats, err := s.store.ListRecords(c.Request().Context(), limit)
	if err != nil {
		return storeError(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"stats": stats, "records": records})
}

func (s *Server) handleListLocations(c echo.Context) error {
	source := c.QueryParam("source")
	if source != "" && source != "admin" && source != "random" {
		return badRequest("source 는 admin 또는 random 이어야 합니다")
	}
	limit := queryInt(c, "limit", 300, 1, 2000)
	locs, err := s.store.ListLocations(c.Request().Context(), source, limit)
	if err != nil {
		return storeError(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"locations": locs})
}

func (s *Server) handleCreateLocation(c echo.Context) error {
	var in LocationInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	in, err := normalizeLocation(in)
	if err != nil {
		return badRequest(err.Error())
	}
	loc, err := s.store.InsertLocation(c.Request().Context(), in, "admin")
	if err != nil {
		return storeError(err)
	}
	return c.JSON(http.StatusCreated, loc)
}

func (s *Server) handleDeleteLocation(c echo.Context) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := s.store.DeleteLocation(c.Request().Context(), id); err != nil {
		return storeError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ---------- helpers ----------

func normalizeLocation(in LocationInput) (LocationInput, error) {
	if !validLatLng(in.Lat, in.Lng) {
		return in, errors.New("잘못된 좌표입니다")
	}
	in.PanoID = strings.TrimSpace(in.PanoID)
	in.Address = strings.TrimSpace(in.Address)
	if len(in.PanoID) > 256 {
		return in, errors.New("panoId 가 너무 깁니다")
	}
	if r := []rune(in.Address); len(r) > 500 {
		in.Address = string(r[:500])
	}
	in.Heading = math.Mod(math.Mod(in.Heading, 360)+360, 360)
	return in, nil
}

func validLatLng(lat, lng float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lng) && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

func badRequest(msg string) *echo.HTTPError {
	return echo.NewHTTPError(http.StatusBadRequest, msg)
}

// storeError 는 저장소 에러를 HTTP 에러로 변환한다.
func storeError(err error) *echo.HTTPError {
	switch {
	case errors.Is(err, ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "대상을 찾을 수 없습니다")
	case errors.Is(err, ErrConflict):
		return echo.NewHTTPError(http.StatusConflict, "이미 처리된 요청입니다")
	case errors.Is(err, ErrGone):
		return echo.NewHTTPError(http.StatusGone, "출제 위치가 삭제되었습니다")
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, "서버 오류가 발생했습니다").SetInternal(err)
	}
}

// httpErrorHandler 는 모든 에러를 프론트엔드가 기대하는 {"error": "..."} 형식으로 응답한다.
func httpErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	he := &echo.HTTPError{}
	if !errors.As(err, &he) {
		he = echo.NewHTTPError(http.StatusInternalServerError, "서버 오류가 발생했습니다").SetInternal(err)
	}
	if he.Internal != nil {
		log.Printf("%s %s: %v", c.Request().Method, c.Request().URL.Path, he.Internal)
	}

	msg := fmt.Sprint(he.Message)
	if m, ok := he.Message.(string); ok {
		msg = m
	}
	if c.Request().Method == http.MethodHead {
		err = c.NoContent(he.Code)
	} else {
		err = c.JSON(he.Code, map[string]string{"error": msg})
	}
	if err != nil {
		log.Printf("write error response: %v", err)
	}
}

// bindJSON 은 알 수 없는 필드를 거부하는 엄격한 JSON 바인딩이다.
// (echo 기본 Bind 는 빈 본문을 에러 없이 통과시키므로 직접 디코딩한다.)
func bindJSON(c echo.Context, v any) error {
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("잘못된 요청 본문입니다: " + err.Error())
	}
	return nil
}

func pathID(c echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest("잘못된 id 입니다")
	}
	return id, nil
}

func queryInt(c echo.Context, key string, def, lo, hi int) int {
	v, err := strconv.Atoi(c.QueryParam(key))
	if err != nil {
		return def
	}
	return min(max(v, lo), hi)
}
