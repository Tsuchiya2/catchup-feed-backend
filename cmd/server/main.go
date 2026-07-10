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
	"catchup-feed/internal/feed"
	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/infra/db"
	learncore "catchup-feed/internal/learning"
	"catchup-feed/pkg/config"
	"catchup-feed/pkg/security/csp"

	alUC "catchup-feed/internal/usecase/accesslog"
	artUC "catchup-feed/internal/usecase/article"
	learnUC "catchup-feed/internal/usecase/learning"
	srcUC "catchup-feed/internal/usecase/source"
	subUC "catchup-feed/internal/usecase/subscriber"

	hhttp "catchup-feed/internal/handler/http"
	haccesslog "catchup-feed/internal/handler/http/accesslog"
	harticle "catchup-feed/internal/handler/http/article"
	hauth "catchup-feed/internal/handler/http/auth"
	hlearning "catchup-feed/internal/handler/http/learning"
	"catchup-feed/internal/handler/http/middleware"
	"catchup-feed/internal/handler/http/requestid"
	hsrc "catchup-feed/internal/handler/http/source"
	hsub "catchup-feed/internal/handler/http/subscriber"
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
	Handler      http.Handler
	RateLimiters []*middleware.RateLimiter // Endpoint rate limiters needing periodic cleanup

	// PrivateFeedHandler / PrivateFeedAddr describe the tailnet-only
	// feed listener (§3.1, C-5). An empty addr disables the listener.
	PrivateFeedHandler http.Handler
	PrivateFeedAddr    string
}

// setupServer configures and returns the HTTP handler with all routes and middleware.
func setupServer(logger *slog.Logger, database *sql.DB, version string) *ServerComponents {
	srcSvc := srcUC.Service{Repo: pgRepo.NewSourceRepo(database)}
	artSvc := artUC.Service{Repo: pgRepo.NewArticleRepo(database)}

	// 友人・トークン・アクセスログ管理(§5.1 admin API)。フィードトークン
	// リポジトリは公開フィード配信(feedServer)と同じテーブルを共有する。
	subSvc := subUC.Service{
		Subscribers: pgRepo.NewSubscriberRepo(database),
		Tokens:      pgRepo.NewFeedTokenRepo(database),
	}
	logSvc := alUC.Service{Logs: pgRepo.NewFeedAccessLogRepo(database)}

	// 学習ループ管理 API(Phase 3 §8.1)。採点遷移のラダーは radio 側の
	// 自動解決と同じ QUIZ_LADDER_DAYS(D-18)を読む — 両者が同じ
	// learning.Transition を同じパラメータで適用する。
	learnSvc := learnUC.Service{
		Repo:   pgRepo.NewLearningAdminRepo(database),
		Ladder: learncore.LoadConfig(logger).Ladder,
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

	// Feed delivery (§5): repositories + config shared by the public
	// routes and the tailnet-only private listener.
	feedCfg := feed.LoadConfig()
	if feedCfg.PrivateAddr != "" {
		// C-5: the private feed has no authentication, so a wildcard bind
		// would expose it to the whole LAN. Refuse to start the private
		// listener (縮退: the public side keeps running).
		if err := feed.ValidatePrivateAddr(feedCfg.PrivateAddr); err != nil {
			logger.Error("private feed listener disabled: unsafe PRIVATE_FEED_ADDR",
				slog.String("addr", feedCfg.PrivateAddr), slog.Any("error", err))
			feedCfg.PrivateAddr = ""
		}
	}
	feedServer := feed.NewServer(
		feedCfg,
		pgRepo.NewEpisodeRepo(database),
		pgRepo.NewFeedTokenRepo(database),
		pgRepo.NewFeedAccessLogRepo(database),
		logger,
	)

	// Setup routes with per-endpoint rate limiting
	rootMux, rateLimiters := setupRoutes(database, version, srcSvc, artSvc, subSvc, logSvc, learnSvc, ipExtractor, logger, feedServer, feedCfg.PublicBaseURL)
	handler := applyMiddleware(logger, rootMux)

	// The private feed handler skips CORS/CSP/auth entirely: physical
	// boundary (tailnet bind) is the authentication (C-5). Recovery and
	// logging still apply.
	privateHandler := requestid.Middleware(
		hhttp.Recover(logger)(hhttp.Logging(logger)(feedServer.PrivateHandler())))

	return &ServerComponents{
		Handler:            handler,
		RateLimiters:       rateLimiters,
		PrivateFeedHandler: privateHandler,
		PrivateFeedAddr:    feedCfg.PrivateAddr,
	}
}

// setupRoutes registers all HTTP routes (public and protected).
func setupRoutes(
	database *sql.DB,
	version string,
	srcSvc srcUC.Service,
	artSvc artUC.Service,
	subSvc subUC.Service,
	logSvc alUC.Service,
	learnSvc learnUC.Service,
	ipExtractor middleware.IPExtractor,
	logger *slog.Logger,
	feedServer *feed.Server,
	publicBaseURL string,
) (*http.ServeMux, []*middleware.RateLimiter) {
	// レート制限: 認証エンドポイントは1分間に5リクエストまで
	authRateLimiter := middleware.NewRateLimiter(5, 1*time.Minute, ipExtractor)

	// レート制限: 検索エンドポイントは1分間に100リクエストまで
	searchRateLimiter := middleware.NewRateLimiter(100, 1*time.Minute, ipExtractor)

	// レート制限: 公開フィードは per-IP で1分間に60リクエストまで(§5.2、
	// 無効トークン連打対策程度の軽いもの。ポッドキャストアプリの巡回は
	// フィード1回+mp3数回なので通常運用では到達しない)
	feedRateLimiter := middleware.NewRateLimiter(60, 1*time.Minute, ipExtractor)

	// 単一管理者の資格情報検証(環境変数+bcrypt、C-7/C-20)
	authService := authservice.NewAuthService(hauth.NewAdminAuthProvider())

	publicMux := http.NewServeMux()
	publicMux.Handle("/auth/token", authRateLimiter.Middleware(hauth.TokenHandler(authService)))

	// ヘルスチェックエンドポイント（認証不要）
	publicMux.Handle("/health", &hhttp.HealthHandler{DB: database, Version: version})
	publicMux.Handle("/ready", &hhttp.ReadyHandler{DB: database})
	publicMux.Handle("/live", &hhttp.LiveHandler{})

	// Swagger UI（認証不要）
	publicMux.Handle("/swagger/", httpSwagger.WrapHandler)

	// Load pagination configuration
	paginationCfg := pagination.LoadFromEnv()

	privateMux := http.NewServeMux()
	hsrc.Register(privateMux, srcSvc, searchRateLimiter)
	harticle.Register(privateMux, artSvc, paginationCfg, logger, searchRateLimiter)
	// 友人管理・トークン発行/失効・アクセスログ(§5.1)。管理 API は
	// すべて単一管理者の JWT 必須(C-20)。トークン発行レスポンスの
	// 購読 URL は publicBaseURL(D-6)から組み立てる。
	hsub.Register(privateMux, subSvc, publicBaseURL)
	haccesslog.Register(privateMux, logSvc)
	// 学習ループ管理 API(Phase 3 §8.1、C-21 フラット構成)。全ルート
	// JWT 必須 — 理解状態は私的データ(§10)。
	hlearning.Register(privateMux, learnSvc)

	// Apply authentication middleware
	protected := hauth.Authz(privateMux)

	rootMux := http.NewServeMux()
	rootMux.Handle("/auth/token", publicMux)
	rootMux.Handle("/health", publicMux)
	rootMux.Handle("/ready", publicMux)
	rootMux.Handle("/live", publicMux)
	rootMux.Handle("/swagger/", publicMux)
	rootMux.Handle("/", protected)

	// 公開フィード(§5.1): JWT ではなく URL 埋め込みトークンで認証する
	// (C-6)。パターンが "/" より特定的なので管理 API には影響しない。
	feedServer.RegisterPublic(rootMux, feedRateLimiter.Middleware)

	// Return rate limiters for periodic cleanup
	return rootMux, []*middleware.RateLimiter{authRateLimiter, searchRateLimiter, feedRateLimiter}
}

// applyMiddleware wraps the handler with middleware chain.
// Middleware order: CORS → Request ID → Recovery → Logging → Body Limit → CSP
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

	// Build middleware chain (applied in reverse order, innermost to outermost)
	middlewareChain := handler

	middlewareChain = cspMiddleware(middlewareChain)
	middlewareChain = hhttp.LimitRequestBody(1 << 20)(middlewareChain) // 1MB limit
	middlewareChain = hhttp.Logging(logger)(middlewareChain)
	middlewareChain = hhttp.Recover(logger)(middlewareChain)
	middlewareChain = requestid.Middleware(middlewareChain)
	middlewareChain = middleware.CORS(*corsConfig)(middlewareChain)

	return middlewareChain
}

// startRateLimiterCleanup periodically evicts expired entries from the
// endpoint rate limiters to prevent unbounded memory growth.
func startRateLimiterCleanup(ctx context.Context, limiters []*middleware.RateLimiter, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, rl := range limiters {
				rl.CleanupExpired()
			}
		}
	}
}

// runServer starts the HTTP server and handles graceful shutdown.
func runServer(logger *slog.Logger, components *ServerComponents, version string) {
	// Create a context for background goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background cleanup for endpoint rate limiters
	go startRateLimiterCleanup(ctx, components.RateLimiters, 5*time.Minute)

	// Error channel for coordinated shutdown when the public server fails.
	// The private listener never writes here: its failure is degraded to an
	// Error log (§8) so the public side keeps serving.
	serverErrCh := make(chan error, 1)

	// Start HTTP server
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           components.Handler,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		logger.Info("HTTP server starting",
			slog.String("addr", ":8080"),
			slog.String("version", version))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", slog.Any("error", err))
			serverErrCh <- err
		}
	}()

	// 私的フィードリスナー(§3.1): tailnet アドレスにのみバインドする
	// 別リスナー。PRIVATE_FEED_ADDR 未設定なら起動しない。bind や serve の
	// 失敗は Error ログのみで公開サーバーは道連れにしない(§8、C-5:
	// 本人専用なので翌日の systemd 再起動で戻れば足りる)。
	var privateSrv *http.Server
	if components.PrivateFeedAddr != "" {
		privateSrv = startPrivateFeedListener(ctx, logger, components.PrivateFeedAddr, components.PrivateFeedHandler)
	} else {
		logger.Info("private feed listener disabled (PRIVATE_FEED_ADDR not set)")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal or server error
	select {
	case <-quit:
		logger.Info("shutting down server...")
	case err := <-serverErrCh:
		logger.Error("server startup failed, initiating shutdown", slog.Any("error", err))
	}

	// Cancel background goroutines
	cancel()

	// Shutdown HTTP server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown failed", slog.Any("error", err))
	}
	if privateSrv != nil {
		if err := privateSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("private feed listener shutdown failed", slog.Any("error", err))
		}
	}
	logger.Info("HTTP server stopped")
}

// startPrivateFeedListener starts the tailnet-only feed listener (§3.1).
// 縮退許容(§8): bind 失敗(tailscaled 未起動・アドレス未割当等)や
// serve 中の失敗は Error ログに留め、公開サーバーには波及させない。
// bind は同期的に行い、失敗時は nil を返す(呼び出し側は Shutdown 不要)。
// 成功時は返す *http.Server の Addr に実際のリッスンアドレスを設定する。
func startPrivateFeedListener(ctx context.Context, logger *slog.Logger, addr string, handler http.Handler) *http.Server {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("private feed listener disabled: bind failed (public server continues)",
			slog.String("addr", addr), slog.Any("error", err))
		return nil
	}

	srv := &http.Server{
		Addr:              ln.Addr().String(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		logger.Info("private feed listener starting", slog.String("addr", srv.Addr))
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("private feed listener failed (public server continues)",
				slog.String("addr", srv.Addr), slog.Any("error", err))
		}
	}()

	return srv
}
