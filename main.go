package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

//go:embed schema.sql
var schemaSQL string

type Config struct {
	Port        string
	DatabaseURL string
	MapsAPIKey  string
	AdminRatio  float64 // 관리자 지정 위치 출제 비율 (기본 0.2 → 랜덤:관리자 = 8:2)
}

func loadConfig() Config {
	cfg := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://geoworker:geoworker@localhost:5432/geoworker?sslmode=disable"),
		MapsAPIKey:  strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")),
		AdminRatio:  0.2,
	}
	if v, err := strconv.ParseFloat(os.Getenv("ADMIN_LOCATION_RATIO"), 64); err == nil && v >= 0 && v <= 1 {
		cfg.AdminRatio = v
	}
	return cfg
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := loadConfig()
	if cfg.MapsAPIKey == "" {
		log.Println("WARNING: GOOGLE_MAPS_API_KEY 가 비어 있습니다. .env 에 키를 입력하세요.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := OpenStore(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx, schemaSQL); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	(&Server{cfg: cfg, store: store}).routes(mux)
	mux.Handle("GET /", noCache(http.FileServerFS(static)))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("GeoWorker listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}
