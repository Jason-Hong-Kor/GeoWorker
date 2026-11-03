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

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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

	e := newEcho(&Server{cfg: cfg, store: store}, static)
	e.Server.ReadHeaderTimeout = 10 * time.Second

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e.Shutdown(shutdownCtx)
	}()

	log.Printf("GeoWorker listening on :%s", cfg.Port)
	if err := e.Start(":" + cfg.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newEcho(s *Server, static fs.FS) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = httpErrorHandler

	e.Use(middleware.Recover())
	e.Use(requestLogger())
	e.Use(noCache)

	s.routes(e)
	e.StaticFS("/", static)
	return e
}

// requestLogger 는 /api 요청만 "METHOD PATH STATUS LATENCY" 형식으로 기록한다.
func requestLogger() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		Skipper: func(c echo.Context) bool {
			return !strings.HasPrefix(c.Request().URL.Path, "/api/")
		},
		HandleError: true, // 에러 핸들러가 정한 실제 상태 코드를 기록
		LogMethod:   true,
		LogURIPath:  true,
		LogStatus:   true,
		LogLatency:  true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			log.Printf("%s %s %d %s", v.Method, v.URIPath, v.Status, v.Latency.Round(time.Millisecond))
			return nil
		},
	})
}

// noCache: 정적 파일은 매번 재검증 (API 응답은 noStore 가 덮어씀)
func noCache(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-cache")
		return next(c)
	}
}

// noStore: API 응답은 캐시하지 않음
func noStore(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		return next(c)
	}
}
