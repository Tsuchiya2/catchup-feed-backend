package feed

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// Server holds the dependencies of the feed delivery handlers (§5).
type Server struct {
	cfg        Config
	episodes   repository.EpisodeRepository
	tokens     repository.FeedTokenRepository
	accessLogs repository.FeedAccessLogRepository
	logger     *slog.Logger
}

// NewServer builds a feed Server.
func NewServer(
	cfg Config,
	episodes repository.EpisodeRepository,
	tokens repository.FeedTokenRepository,
	accessLogs repository.FeedAccessLogRepository,
	logger *slog.Logger,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:        cfg,
		episodes:   episodes,
		tokens:     tokens,
		accessLogs: accessLogs,
		logger:     logger,
	}
}

// RegisterPublic registers the token-protected public routes (§5.1) on
// mux. wrap, when non-nil, is applied outside token verification — the
// per-IP rate limiter guarding against invalid-token hammering (§5.2).
//
//	GET /feeds/{token}/feed.xml
//	GET /feeds/{token}/episodes/{id}.mp3
func (s *Server) RegisterPublic(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	if wrap == nil {
		wrap = func(h http.Handler) http.Handler { return h }
	}
	mux.Handle("GET /feeds/{token}/feed.xml", wrap(s.verifyToken(http.HandlerFunc(s.handlePublicFeed))))
	mux.Handle("GET /feeds/{token}/episodes/{file}", wrap(s.verifyToken(http.HandlerFunc(s.handlePublicEpisode))))
	// Catch-all for everything else under /feeds/: unmatched variants
	// (trailing slashes, wrong methods, stray segments) answer 404 here
	// instead of falling through to the JWT-protected "/" handler, so a
	// token-bearing request never touches the admin auth stack.
	mux.Handle("/feeds/", wrap(http.NotFoundHandler()))
}

// PrivateHandler returns the mux for the tailnet-only listener (§3.1,
// C-5): no authentication, no subscriber concept, every feed kind. It must
// only ever be bound to a tailnet address — the physical boundary is the
// authentication.
//
//	GET /private/feed.xml
//	GET /private/episodes/{id}.mp3
func (s *Server) PrivateHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /private/feed.xml", s.handlePrivateFeed)
	mux.HandleFunc("GET /private/episodes/{file}", s.handlePrivateEpisode)
	return mux
}

// ---- token verification middleware (§5.2) ----

// tokenCtxKey carries the verified *entity.FeedToken from verifyToken to
// the public handlers, which record the access once they know whether the
// requested episode actually exists (avoids FK violations on garbage IDs).
type tokenCtxKey struct{}

func withToken(ctx context.Context, token *entity.FeedToken) context.Context {
	return context.WithValue(ctx, tokenCtxKey{}, token)
}

func tokenFromContext(ctx context.Context) *entity.FeedToken {
	token, _ := ctx.Value(tokenCtxKey{}).(*entity.FeedToken)
	return token
}

// verifyToken authenticates the {token} path segment: SHA-256 the
// plaintext, look the digest up via GetActiveByHash (not revoked AND
// subscriber active). Every failure mode — malformed, unknown, revoked,
// deactivated subscriber — answers 404 so the response never reveals
// whether a token exists. The verified token is stored in the request
// context; access logging happens in the handlers.
func (s *Server) verifyToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plaintext := r.PathValue("token")
		if !validTokenFormat(plaintext) {
			http.NotFound(w, r)
			return
		}
		token, err := s.tokens.GetActiveByHash(r.Context(), entity.HashFeedToken(plaintext))
		if err != nil {
			s.logger.Error("feed: token lookup failed", slog.Any("error", err))
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		if token == nil {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(withToken(r.Context(), token)))
	})
}

// validTokenFormat rejects anything that cannot be a token issued by
// entity.NewFeedTokenPlaintext (base64url of exactly 32 random bytes)
// before spending a DB roundtrip on it.
func validTokenFormat(plaintext string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(plaintext)
	return err == nil && len(raw) == 32
}

// recordAccess appends one feed_access_logs row (episode ID for a
// verified-to-exist mp3, nil for feed.xml) plus a structured log line.
// Handlers call it only after confirming the episode exists, keeping FK
// violations out of the logs. The client IP is not part of the §4 schema,
// so it lives in the slog output only.
func (s *Server) recordAccess(r *http.Request, token *entity.FeedToken, episodeID *int64) {
	if token == nil {
		return
	}
	var userAgent *string
	if ua := r.UserAgent(); ua != "" {
		userAgent = &ua
	}

	row := &entity.FeedAccessLog{TokenID: token.ID, EpisodeID: episodeID, UserAgent: userAgent}
	if err := s.accessLogs.Insert(r.Context(), row); err != nil {
		// Best effort: losing an analytics row must never break a friend's
		// podcast app.
		s.logger.Warn("feed: access log insert failed, continuing delivery",
			slog.Int64("token_id", token.ID), slog.Any("error", err))
	}
	s.logger.Info("feed: access",
		slog.Int64("token_id", token.ID),
		slog.Int64("subscriber_id", token.SubscriberID),
		slog.String("remote_addr", r.RemoteAddr),
		slog.String("user_agent", r.UserAgent()))
}

// ---- public handlers ----

func (s *Server) handlePublicFeed(w http.ResponseWriter, r *http.Request) {
	s.recordAccess(r, tokenFromContext(r.Context()), nil)

	episodes, err := s.episodes.ListByKind(r.Context(), entity.FeedKindPublic, s.cfg.MaxItems)
	if err != nil {
		s.logger.Error("feed: list public episodes failed", slog.Any("error", err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	plaintext := r.PathValue("token")
	s.writeFeed(w, s.cfg.PublicBaseURL, episodes, func(ep *entity.Episode) string {
		return publicEnclosureURL(s.cfg.PublicBaseURL, plaintext, ep.ID)
	})
}

func (s *Server) handlePublicEpisode(w http.ResponseWriter, r *http.Request) {
	episode, ok := s.lookupEpisode(w, r)
	if !ok {
		return
	}
	// The public route serves public episodes only; a 'private' row is
	// indistinguishable from a missing one (§4 設計メモ).
	if episode.FeedKind != entity.FeedKindPublic {
		http.NotFound(w, r)
		return
	}
	// Logged only for episodes that verifiably exist — a garbage ID must
	// not turn into a foreign-key violation on feed_access_logs.
	s.recordAccess(r, tokenFromContext(r.Context()), &episode.ID)
	s.serveAudio(w, r, episode)
}

// ---- private handlers (tailnet listener) ----

func (s *Server) handlePrivateFeed(w http.ResponseWriter, r *http.Request) {
	episodes, err := s.episodes.ListRecent(r.Context(), s.cfg.MaxItems)
	if err != nil {
		s.logger.Error("feed: list episodes failed", slog.Any("error", err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	base := s.cfg.PrivateBaseURL
	if base == "" {
		// Tailnet-only plain HTTP; the Host header is the tailnet name.
		base = "http://" + r.Host
	}
	s.writeFeed(w, base, episodes, func(ep *entity.Episode) string {
		return privateEnclosureURL(base, ep.ID)
	})
}

func (s *Server) handlePrivateEpisode(w http.ResponseWriter, r *http.Request) {
	episode, ok := s.lookupEpisode(w, r)
	if !ok {
		return
	}
	s.serveAudio(w, r, episode)
}

// ---- shared pieces ----

// writeFeed renders and writes the RSS document. link becomes the channel
// <link>: the public base URL for the public feed, the private base for
// the tailnet feed (the private feed must not advertise the public host).
func (s *Server) writeFeed(w http.ResponseWriter, link string, episodes []*entity.Episode, enclosureURL func(*entity.Episode) string) {
	meta := channelMeta{
		Title:       s.cfg.ChannelTitle,
		Link:        link,
		Description: s.cfg.ChannelDescription,
		Language:    "ja",
	}
	body, err := renderRSS(meta, episodes, enclosureURL)
	if err != nil {
		s.logger.Error("feed: rss rendering failed", slog.Any("error", err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write(body)
}

// lookupEpisode parses the {file} path segment ("{id}.mp3") and loads the
// episode. It writes the error response itself and returns ok=false when
// the request cannot be served.
func (s *Server) lookupEpisode(w http.ResponseWriter, r *http.Request) (*entity.Episode, bool) {
	id, ok := episodeIDFromFile(r.PathValue("file"))
	if !ok {
		http.NotFound(w, r)
		return nil, false
	}
	episode, err := s.episodes.Get(r.Context(), id)
	if err != nil {
		s.logger.Error("feed: episode lookup failed", slog.Int64("episode_id", id), slog.Any("error", err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, false
	}
	if episode == nil {
		http.NotFound(w, r)
		return nil, false
	}
	return episode, true
}

// serveAudio streams the episode mp3 with Range support via
// http.ServeContent (C-10). The file is opened through os.Root so that a
// DB audio_path can never escape the configured audio directory, not even
// via symlinks.
func (s *Server) serveAudio(w http.ResponseWriter, r *http.Request, episode *entity.Episode) {
	rel, ok := audioRelPath(s.cfg.AudioDir, episode.AudioPath)
	if !ok {
		s.logger.Warn("feed: audio path outside audio dir, refusing",
			slog.Int64("episode_id", episode.ID), slog.String("audio_path", episode.AudioPath))
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(s.cfg.AudioDir)
	if err != nil {
		s.logger.Error("feed: audio dir unavailable", slog.String("audio_dir", s.cfg.AudioDir), slog.Any("error", err))
		http.NotFound(w, r)
		return
	}
	defer func() { _ = root.Close() }()

	f, err := root.Open(rel)
	if err != nil {
		// Degraded state (e.g. mp3 pruned by the retention job, D-4):
		// answer 404 and let the client retry from the feed.
		s.logger.Warn("feed: audio file missing",
			slog.Int64("episode_id", episode.ID), slog.String("audio_path", episode.AudioPath), slog.Any("error", err))
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	// The empty name keeps ServeContent from re-deriving the content type;
	// it still handles Range, If-Modified-Since and friends.
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// audioRelPath resolves an episodes.audio_path (absolute Pi path or
// relative) to a path relative to audioDir, refusing anything that points
// outside it. This is the lexical half of the traversal guard; os.Root
// enforces the same boundary at the filesystem level.
func audioRelPath(audioDir, audioPath string) (string, bool) {
	if audioPath == "" {
		return "", false
	}
	rel := audioPath
	if filepath.IsAbs(audioPath) {
		base, err := filepath.Abs(audioDir)
		if err != nil {
			return "", false
		}
		rel, err = filepath.Rel(base, audioPath)
		if err != nil {
			return "", false
		}
	} else {
		rel = filepath.Clean(rel)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// episodeIDFromFile parses the "{id}.mp3" path segment. Only plain
// positive decimal IDs are accepted.
func episodeIDFromFile(file string) (int64, bool) {
	numeric, found := strings.CutSuffix(file, ".mp3")
	if !found || numeric == "" {
		return 0, false
	}
	for _, c := range numeric {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(numeric, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
