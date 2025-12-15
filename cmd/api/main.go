package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"catchup-feed/internal/common/pagination"
	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/infra/db"

	artUC "catchup-feed/internal/usecase/article"
	srcUC "catchup-feed/internal/usecase/source"

	hhttp "catchup-feed/internal/handler/http"
	harticle "catchup-feed/internal/handler/http/article"
	hauth "catchup-feed/internal/handler/http/auth"
	"catchup-feed/internal/handler/http/middleware"
	"catchup-feed/internal/handler/http/requestid"
	hsrc "catchup-feed/internal/handler/http/source"
	authservice "catchup-feed/internal/service/auth"

	_ "catchup-feed/docs" // swagger docs
)

// @title           Catchup Feed API
// @version         1.0
// @description     RSS/Atom フィード自動クロール・AI要約システムの REST API
// @description     記事とRSSソースの管理、AI による記事要約機能を提供します。

// @contact.name   API Support
// @contact.url    https://github.com/yujitsuchiya/catchup-feed
// @contact.email  support@example.com

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description JWT トークンによる認証。ヘッダーに "Bearer {token}" 形式で指定してください。

func main() {
	logger := initLogger()
	validateAdminCredentials(logger)
	validateViewerCredentials(logger)
	validateJWTSecret(logger)
	database := initDatabase(logger)
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("failed to close database", slog.Any("error", err))
		}
	}()

	version := getVersion()
	handler := setupServer(logger, database, version)

	runServer(logger, handler, version)
}

// initLogger initializes and returns a structured logger based on environment configuration.
func initLogger() *slog.Logger {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)
	return logger
}

// validateAdminCredentials validates the admin credentials at startup.
// This prevents the server from starting with empty or weak admin credentials.
func validateAdminCredentials(logger *slog.Logger) {
	if err := hauth.ValidateAdminCredentials(); err != nil {
		logger.Error("admin credentials validation failed", slog.Any("error", err))
		os.Exit(1)
	}
}

// validateViewerCredentials validates the viewer credentials at startup.
// Unlike admin validation, this implements graceful degradation:
// if viewer credentials are misconfigured, the viewer role is disabled
// but the application continues to run in admin-only mode.
func validateViewerCredentials(logger *slog.Logger) {
	_ = hauth.ValidateViewerCredentials(logger)
}

// validateJWTSecret validates the JWT_SECRET environment variable for security requirements.
func validateJWTSecret(logger *slog.Logger) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		logger.Error("JWT_SECRET must be set")
		os.Exit(1)
	}
	// セキュリティ: 最小32文字（256ビット）を強制
	if len(secret) < 32 {
		logger.Error("JWT_SECRET must be at least 32 characters (256 bits)")
		os.Exit(1)
	}
	// セキュリティ: よくある弱い秘密鍵を拒否
	weakSecrets := []string{"secret", "password", "test", "admin", "default"}
	for _, weak := range weakSecrets {
		if secret == weak || secret == weak+"123" {
			logger.Error("JWT_SECRET must not be a common weak value", slog.String("weak_value", weak))
			os.Exit(1)
		}
	}
}

// initDatabase opens the database connection and runs migrations.
func initDatabase(logger *slog.Logger) *sql.DB {
	database := db.Open()
	if err := db.MigrateUp(database); err != nil {
		logger.Error("failed to migrate database", slog.Any("error", err))
		os.Exit(1)
	}
	return database
}

// getVersion returns the application version from environment or default.
func getVersion() string {
	version := os.Getenv("VERSION")
	if version == "" {
		version = "dev"
	}
	return version
}

// setupServer configures and returns the HTTP handler with all routes and middleware.
func setupServer(logger *slog.Logger, database *sql.DB, version string) http.Handler {
	srcSvc := srcUC.Service{Repo: pgRepo.NewSourceRepo(database)}
	artSvc := artUC.Service{Repo: pgRepo.NewArticleRepo(database)}

	// Load trusted proxy configuration for rate limiting
	proxyConfig, err := middleware.LoadTrustedProxyConfig()
	if err != nil {
		logger.Error("failed to load trusted proxy configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Create appropriate IPExtractor based on configuration
	var ipExtractor middleware.IPExtractor
	if proxyConfig.Enabled {
		ipExtractor = middleware.NewTrustedProxyExtractor(*proxyConfig)
		logger.Info("rate limiting: trusted proxy mode enabled",
			slog.Int("trusted_proxies_count", len(proxyConfig.AllowedCIDRs)))
	} else {
		ipExtractor = &middleware.RemoteAddrExtractor{}
		logger.Info("rate limiting: using RemoteAddr (secure mode, proxy headers ignored)")
	}

	rootMux := setupRoutes(database, version, srcSvc, artSvc, ipExtractor, logger)
	return applyMiddleware(logger, rootMux)
}

// setupRoutes registers all HTTP routes (public and protected).
func setupRoutes(database *sql.DB, version string, srcSvc srcUC.Service, artSvc artUC.Service, ipExtractor middleware.IPExtractor, logger *slog.Logger) *http.ServeMux {
	// レート制限: 認証エンドポイントは1分間に5リクエストまで
	authRateLimiter := middleware.NewRateLimiter(5, 1*time.Minute, ipExtractor)

	// レート制限: 検索エンドポイントは1分間に100リクエストまで（バースト10）
	// Note: Current implementation uses sliding window without explicit burst size,
	// but limit of 100 req/min allows bursts naturally within the time window
	searchRateLimiter := middleware.NewRateLimiter(100, 1*time.Minute, ipExtractor)

	// Initialize AuthService with MultiUserAuthProvider
	weakPasswords := []string{"password", "123456", "admin", "test", "secret"}
	authProvider := hauth.NewMultiUserAuthProvider(12, weakPasswords)
	publicEndpoints := []string{"/auth/token", "/health", "/ready", "/live", "/metrics", "/swagger/"}
	authService := authservice.NewAuthService(authProvider, publicEndpoints)

	publicMux := http.NewServeMux()
	publicMux.Handle("/auth/token", authRateLimiter.Middleware(hauth.TokenHandler(authService)))

	// ヘルスチェックエンドポイント（認証不要）
	publicMux.Handle("/health", &hhttp.HealthHandler{DB: database, Version: version})
	publicMux.Handle("/ready", &hhttp.ReadyHandler{DB: database})
	publicMux.Handle("/live", &hhttp.LiveHandler{})
	publicMux.Handle("/metrics", hhttp.MetricsHandler())

	// Swagger UI（認証不要）
	publicMux.Handle("/swagger/", httpSwagger.WrapHandler)

	// Load pagination configuration
	paginationCfg := pagination.LoadFromEnv()

	privateMux := http.NewServeMux()
	hsrc.Register(privateMux, srcSvc, searchRateLimiter)
	harticle.Register(privateMux, artSvc, paginationCfg, logger, searchRateLimiter)

	protected := hauth.Authz(privateMux)

	rootMux := http.NewServeMux()
	rootMux.Handle("/auth/token", publicMux)
	rootMux.Handle("/health", publicMux)
	rootMux.Handle("/ready", publicMux)
	rootMux.Handle("/live", publicMux)
	rootMux.Handle("/metrics", publicMux)
	rootMux.Handle("/swagger/", publicMux)
	rootMux.Handle("/", protected)

	return rootMux
}

// applyMiddleware wraps the handler with middleware chain.
func applyMiddleware(logger *slog.Logger, handler http.Handler) http.Handler {
	// Load CORS configuration from environment variables
	corsConfig, err := middleware.LoadCORSConfig()
	if err != nil {
		logger.Error("failed to load CORS configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Inject SlogAdapter for logging
	corsConfig.Logger = &middleware.SlogAdapter{Logger: logger}

	// Log CORS startup configuration
	logger.Info("CORS enabled",
		slog.Int("allowed_origins_count", len(corsConfig.Validator.GetAllowedOrigins())),
		slog.Any("allowed_origins", corsConfig.Validator.GetAllowedOrigins()),
		slog.Any("allowed_methods", corsConfig.AllowedMethods),
		slog.Any("allowed_headers", corsConfig.AllowedHeaders),
		slog.Int("max_age", corsConfig.MaxAge))

	// ミドルウェアの適用: CORS（最外層） → リクエストID → リカバリ → ロギング → ボディサイズ制限(1MB) → メトリクス
	return middleware.CORS(*corsConfig)(
		requestid.Middleware(
			hhttp.Recover(logger)(
				hhttp.Logging(logger)(
					hhttp.LimitRequestBody(1 << 20)(
						hhttp.MetricsMiddleware(handler))))))
}

// runServer starts the HTTP server and handles graceful shutdown.
func runServer(logger *slog.Logger, handler http.Handler, version string) {
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
		BaseContext: func(_ net.Listener) context.Context {
			return context.Background()
		},
	}

	go func() {
		logger.Info("server starting",
			slog.String("addr", ":8080"),
			slog.String("version", version))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", slog.Any("error", err))
	}
	logger.Info("server stopped")
}
