package main

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
)

type Server struct {
	cfg   Config
	store *Store
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config", s.handleConfig)

	mux.HandleFunc("GET /api/games/draw", s.handleDraw)
	mux.HandleFunc("POST /api/games", s.handleCreateGame)
	mux.HandleFunc("POST /api/games/{id}/guess", s.handleGuess)
	mux.HandleFunc("POST /api/games/{id}/feedback", s.handleFeedback)
	mux.HandleFunc("DELETE /api/games/{id}", s.handleAbandon)

	mux.HandleFunc("GET /api/records", s.handleRecords)

	mux.HandleFunc("GET /api/admin/locations", s.handleListLocations)
	mux.HandleFunc("POST /api/admin/locations", s.handleCreateLocation)
	mux.HandleFunc("DELETE /api/admin/locations/{id}", s.handleDeleteLocation)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"googleMapsApiKey": s.cfg.MapsAPIKey,
		"adminRatio":       s.cfg.AdminRatio,
	})
}

// handleDraw 는 이번 게임의 출제 방식을 결정한다. (랜덤 : 관리자 지정 = 8 : 2 기본)
// 랜덤인 경우 브라우저가 Street View 서비스로 실제 위치를 찾은 뒤 POST /api/games 로 등록한다.
func (s *Server) handleDraw(w http.ResponseWriter, r *http.Request) {
	if rand.Float64() < s.cfg.AdminRatio {
		loc, err := s.store.RandomAdminLocation(r.Context())
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"source": "admin", "location": loc})
			return
		}
		if !errors.Is(err, ErrNotFound) {
			log.Printf("draw admin location: %v", err)
		}
		// 관리자 위치가 없으면 랜덤으로 대체
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": "random"})
}

func (s *Server) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocationID *int64         `json:"locationId"`
		Location   *LocationInput `json:"location"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var (
		loc Location
		err error
	)
	switch {
	case req.LocationID != nil:
		loc, err = s.store.GetLocation(r.Context(), *req.LocationID)
	case req.Location != nil:
		in, verr := normalizeLocation(*req.Location)
		if verr != nil {
			writeError(w, http.StatusBadRequest, verr.Error())
			return
		}
		loc, err = s.store.InsertLocation(r.Context(), in, "random")
	default:
		writeError(w, http.StatusBadRequest, "locationId 또는 location 이 필요합니다")
		return
	}
	if err != nil {
		s.storeError(w, err)
		return
	}

	game, err := s.store.CreateGame(r.Context(), loc.ID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"gameId":    game.ID,
		"startedAt": game.StartedAt,
		"location":  loc,
	})
}

func (s *Server) handleGuess(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var guess LatLng
	if err := decodeJSON(w, r, &guess); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validLatLng(guess.Lat, guess.Lng) {
		writeError(w, http.StatusBadRequest, "잘못된 좌표입니다")
		return
	}
	res, err := s.store.FinishGame(r.Context(), id, guess)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Value int `json:"value"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Value != 1 && req.Value != -1 {
		writeError(w, http.StatusBadRequest, "value 는 1 또는 -1 이어야 합니다")
		return
	}
	total, err := s.store.SubmitFeedback(r.Context(), id, req.Value)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locationFeedback": total})
}

func (s *Server) handleAbandon(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.AbandonGame(r.Context(), id); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 100, 1, 1000)
	records, stats, err := s.store.ListRecords(r.Context(), limit)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats, "records": records})
}

func (s *Server) handleListLocations(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source != "" && source != "admin" && source != "random" {
		writeError(w, http.StatusBadRequest, "source 는 admin 또는 random 이어야 합니다")
		return
	}
	limit := queryInt(r, "limit", 300, 1, 2000)
	locs, err := s.store.ListLocations(r.Context(), source, limit)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": locs})
}

func (s *Server) handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	var in LocationInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, err := normalizeLocation(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	loc, err := s.store.InsertLocation(r.Context(), in, "admin")
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, loc)
}

func (s *Server) handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteLocation(r.Context(), id); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (s *Server) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "대상을 찾을 수 없습니다")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "이미 처리된 요청입니다")
	case errors.Is(err, ErrGone):
		writeError(w, http.StatusGone, "출제 위치가 삭제되었습니다")
	default:
		log.Printf("store error: %v", err)
		writeError(w, http.StatusInternalServerError, "서버 오류가 발생했습니다")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("잘못된 요청 본문입니다: " + err.Error())
	}
	return nil
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "잘못된 id 입니다")
		return 0, false
	}
	return id, true
}

func queryInt(r *http.Request, key string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return min(max(v, lo), hi)
}
