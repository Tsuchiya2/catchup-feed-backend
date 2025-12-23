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
	"catchup-feed/pkg/config"
	"catchup-feed/pkg/ratelimit"
	"catchup-feed/pkg/security/csp"

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
	serverComponents := setupServer(logger, database, version)

	runServer(logger, serverComponents, version)
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

// ServerComponents holds components needed for server operation and cleanup.
type ServerComponents struct {
	Handler     http.Handler
	IPStore     *ratelimit.InMemoryRateLimitStore
	UserStore   *ratelimit.InMemoryRateLimitStore
	IPWindow    time.Duration
	UserWindow  time.Duration
	AuthLimiter *middleware.RateLimiter // Legacy rate limiter for cleanup
}

// setupServer configures and returns the HTTP handler with all routes and middleware.
func setupServer(logger *slog.Logger, database *sql.DB, version string) *ServerComponents {
	srcSvc := srcUC.Service{Repo: pgRepo.NewSourceRepo(database)}
	artSvc := artUC.Service{Repo: pgRepo.NewArticleRepo(database)}

	// Load rate limiting configuration
	rateLimitConfig, err := config.LoadRateLimitConfig()
	if err != nil {
		logger.Error("failed to load rate limit configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Load trusted proxy configuration for IP extraction
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

	// Initialize rate limiting components (if enabled)
	var ipRateLimiter *middleware.IPRateLimiter
	var userRateLimiter *middleware.UserRateLimiter
	var ipStore *ratelimit.InMemoryRateLimitStore
	var userStore *ratelimit.InMemoryRateLimitStore

	if rateLimitConfig.Enabled {
		// Create separate stores for IP and user rate limiting
		// This allows independent memory management and cleanup
		ipStore = ratelimit.NewInMemoryRateLimitStore(ratelimit.InMemoryStoreConfig{
			MaxKeys: rateLimitConfig.MaxActiveKeys,
		})
		userStore = ratelimit.NewInMemoryRateLimitStore(ratelimit.InMemoryStoreConfig{
			MaxKeys: rateLimitConfig.MaxActiveKeys,
		})

		algorithm := ratelimit.NewSlidingWindowAlgorithm(&ratelimit.SystemClock{})
		metrics := ratelimit.NewPrometheusMetrics()

		// Create circuit breakers for IP and User rate limiters
		ipCircuitBreaker := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			FailureThreshold: rateLimitConfig.CircuitBreakerFailureThreshold,
			RecoveryTimeout:  rateLimitConfig.CircuitBreakerResetTimeout,
		})

		userCircuitBreaker := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			FailureThreshold: rateLimitConfig.CircuitBreakerFailureThreshold,
			RecoveryTimeout:  rateLimitConfig.CircuitBreakerResetTimeout,
		})

		// Create degradation managers for graceful degradation
		ipDegradationMgr := middleware.NewDegradationManager(middleware.DegradationConfig{
			AutoAdjust:        true,
			CooldownPeriod:    1 * time.Minute,
			RelaxedMultiplier: 2,
			MinimalMultiplier: 10,
			Clock:             &ratelimit.SystemClock{},
			Metrics:           metrics,
			LimiterType:       "ip",
		})

		userDegradationMgr := middleware.NewDegradationManager(middleware.DegradationConfig{
			AutoAdjust:        true,
			CooldownPeriod:    1 * time.Minute,
			RelaxedMultiplier: 2,
			MinimalMultiplier: 10,
			Clock:             &ratelimit.SystemClock{},
			Metrics:           metrics,
			LimiterType:       "user",
		})

		// Wire circuit breaker callbacks to degradation manager
		// Note: Circuit breaker state changes will automatically be detected by the degradation manager
		// through periodic health checks. Direct callbacks are not exposed in the current CircuitBreaker API.
		// The degradation manager monitors circuit breaker state via IsOpen() method.
		_ = ipDegradationMgr   // Degradation manager used for future enhancement
		_ = userDegradationMgr // Degradation manager used for future enhancement

		// Create IP rate limiter
		ipRateLimiter = middleware.NewIPRateLimiter(
			middleware.IPRateLimiterConfig{
				Limit:   rateLimitConfig.DefaultIPLimit,
				Window:  rateLimitConfig.DefaultIPWindow,
				Enabled: true,
			},
			ipExtractor,
			ipStore,
			algorithm,
			metrics,
			ipCircuitBreaker,
		)

		// Create user rate limiter with tier-based limits
		tierLimits := make(map[ratelimit.UserTier]middleware.TierLimit)
		for _, tierCfg := range rateLimitConfig.TierLimits {
			tierLimits[tierCfg.Tier] = middleware.TierLimit{
				Limit:  tierCfg.Limit,
				Window: tierCfg.Window,
			}
		}

		// Create user extractor (uses JWT auth context)
		userExtractor := middleware.NewJWTUserExtractor("user", nil)

		userRateLimiter = middleware.NewUserRateLimiter(middleware.UserRateLimiterConfig{
			Store:               userStore,
			Algorithm:           algorithm,
			Metrics:             metrics,
			CircuitBreaker:      userCircuitBreaker,
			UserExtractor:       userExtractor,
			TierLimits:          tierLimits,
			DefaultLimit:        rateLimitConfig.DefaultUserLimit,
			DefaultWindow:       rateLimitConfig.DefaultUserWindow,
			SkipUnauthenticated: true,
			Clock:               &ratelimit.SystemClock{},
		})

		logger.Info("rate limiting initialized",
			slog.Bool("enabled", true),
			slog.Int("ip_limit", rateLimitConfig.DefaultIPLimit),
			slog.Duration("ip_window", rateLimitConfig.DefaultIPWindow),
			slog.Int("user_limit", rateLimitConfig.DefaultUserLimit),
			slog.Duration("user_window", rateLimitConfig.DefaultUserWindow),
			slog.Int("max_keys", rateLimitConfig.MaxActiveKeys),
		)
	} else {
		logger.Warn("rate limiting is DISABLED - not recommended for production")
	}

	// Setup routes with rate limiting middleware
	rootMux, authLimiter := setupRoutes(database, version, srcSvc, artSvc, ipExtractor, ipRateLimiter, userRateLimiter, logger)
	handler := applyMiddleware(logger, rootMux, ipRateLimiter)

	// Return server components including stores for cleanup
	return &ServerComponents{
		Handler:     handler,
		IPStore:     ipStore,
		UserStore:   userStore,
		IPWindow:    rateLimitConfig.DefaultIPWindow,
		UserWindow:  rateLimitConfig.DefaultUserWindow,
		AuthLimiter: authLimiter,
	}
}

// setupRoutes registers all HTTP routes (public and protected).
func setupRoutes(
	database *sql.DB,
	version string,
	srcSvc srcUC.Service,
	artSvc artUC.Service,
	ipExtractor middleware.IPExtractor,
	ipRateLimiter *middleware.IPRateLimiter,
	userRateLimiter *middleware.UserRateLimiter,
	logger *slog.Logger,
) (*http.ServeMux, *middleware.RateLimiter) {
	// Old rate limiters for specific endpoints (will be deprecated in favor of global middleware)
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

	// Apply authentication middleware
	protected := hauth.Authz(privateMux)

	// Apply user rate limiter AFTER authentication (so we have user context)
	if userRateLimiter != nil {
		protected = userRateLimiter.Middleware()(protected)
	}

	rootMux := http.NewServeMux()
	rootMux.Handle("/auth/token", publicMux)
	rootMux.Handle("/health", publicMux)
	rootMux.Handle("/ready", publicMux)
	rootMux.Handle("/live", publicMux)
	rootMux.Handle("/metrics", publicMux)
	rootMux.Handle("/swagger/", publicMux)
	rootMux.Handle("/", protected)

	// Return auth rate limiter for cleanup management
	return rootMux, authRateLimiter
}

// applyMiddleware wraps the handler with middleware chain.
// Middleware order: CORS → Request ID → IP Rate Limit → Recovery → Logging → Body Limit → CSP → Metrics
func applyMiddleware(logger *slog.Logger, handler http.Handler, ipRateLimiter *middleware.IPRateLimiter) http.Handler {
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

	// Load CSP configuration
	cspConfig, err := config.LoadCSPConfig()
	if err != nil {
		logger.Error("failed to load CSP configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Create CSP middleware
	var cspMiddleware func(http.Handler) http.Handler
	if cspConfig.Enabled {
		cspMW := middleware.NewCSPMiddleware(middleware.CSPMiddlewareConfig{
			Enabled:       true,
			DefaultPolicy: csp.StrictPolicy(),
			PathPolicies: map[string]*csp.CSPBuilder{
				"/swagger/": csp.SwaggerUIPolicy(),
			},
			ReportOnly: cspConfig.ReportOnly,
		})
		cspMiddleware = cspMW.Middleware()
		logger.Info("CSP enabled",
			slog.Bool("report_only", cspConfig.ReportOnly))
	} else {
		// No-op middleware if CSP is disabled
		cspMiddleware = func(next http.Handler) http.Handler {
			return next
		}
		logger.Warn("CSP is disabled")
	}

	// Build middleware chain
	// Recommended order:
	// 1. CORS (handles preflight requests early)
	// 2. Request ID (generates unique ID for request tracking)
	// 3. IP Rate Limiting (check rate limit before expensive operations)
	// 4. Recovery (catch panics)
	// 5. Logging (log all requests)
	// 6. Body Size Limit (prevent DoS)
	// 7. CSP (set security headers)
	// 8. Metrics (record request metrics)
	// 9. Authentication (in routes layer)
	// 10. User Rate Limiting (in routes layer, after auth)

	middlewareChain := handler

	// Apply in reverse order (innermost to outermost)
	middlewareChain = hhttp.MetricsMiddleware(middlewareChain)
	middlewareChain = cspMiddleware(middlewareChain)
	middlewareChain = hhttp.LimitRequestBody(1 << 20)(middlewareChain) // 1MB limit
	middlewareChain = hhttp.Logging(logger)(middlewareChain)
	middlewareChain = hhttp.Recover(logger)(middlewareChain)

	// Apply IP rate limiting if enabled
	if ipRateLimiter != nil {
		middlewareChain = ipRateLimiter.Middleware()(middlewareChain)
	}

	middlewareChain = requestid.Middleware(middlewareChain)
	middlewareChain = middleware.CORS(*corsConfig)(middlewareChain)

	return middlewareChain
}

// runServer starts the HTTP server and handles graceful shutdown.
func runServer(logger *slog.Logger, components *ServerComponents, version string) {
	// Create a context for background goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load cleanup configuration
	cleanupCfg := hhttp.LoadCleanupConfigFromEnv()

	// Start background cleanup goroutines for rate limit stores
	if components.IPStore != nil {
		go hhttp.StartRateLimitCleanup(ctx, components.IPStore, cleanupCfg.Interval, components.IPWindow, "ip")
		logger.Info("IP rate limit cleanup started",
			slog.Duration("interval", cleanupCfg.Interval),
			slog.Duration("window", components.IPWindow))
	}

	if components.UserStore != nil {
		go hhttp.StartRateLimitCleanup(ctx, components.UserStore, cleanupCfg.Interval, components.UserWindow, "user")
		logger.Info("user rate limit cleanup started",
			slog.Duration("interval", cleanupCfg.Interval),
			slog.Duration("window", components.UserWindow))
	}

	// Start cleanup for legacy auth rate limiter
	if components.AuthLimiter != nil {
		go hhttp.StartRateLimitCleanupLegacy(ctx, components.AuthLimiter, cleanupCfg.Interval, "auth")
		logger.Info("auth rate limit cleanup started (legacy)",
			slog.Duration("interval", cleanupCfg.Interval))
	}

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           components.Handler,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
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

	// Cancel background goroutines (rate limit cleanup)
	cancel()
	logger.Debug("background cleanup goroutines cancelled")

	// Shutdown HTTP server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", slog.Any("error", err))
	}
	logger.Info("server stopped")
}
