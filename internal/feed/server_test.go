package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
)

// ---- stubs ----

type stubEpisodeRepo struct {
	episodes []*entity.Episode
	getErr   error
	listErr  error
}

func (s *stubEpisodeRepo) Create(context.Context, *entity.Episode, []*entity.Segment) error {
	return errors.New("not implemented")
}

func (s *stubEpisodeRepo) Get(_ context.Context, id int64) (*entity.Episode, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	for _, ep := range s.episodes {
		if ep.ID == id {
			return ep, nil
		}
	}
	return nil, nil
}

func (s *stubEpisodeRepo) ListByKind(_ context.Context, feedKind string, limit int) ([]*entity.Episode, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []*entity.Episode
	for _, ep := range s.episodes {
		if ep.FeedKind == feedKind {
			out = append(out, ep)
		}
	}
	return sortAndLimit(out, limit), nil
}

func (s *stubEpisodeRepo) ListWithAudioBefore(context.Context, time.Time, int) ([]*entity.Episode, error) {
	return nil, nil
}

func (s *stubEpisodeRepo) ClearAudio(context.Context, int64) error { return nil }

func (s *stubEpisodeRepo) ListAudioPaths(context.Context) ([]string, error) { return nil, nil }

func (s *stubEpisodeRepo) ListRecent(_ context.Context, limit int) ([]*entity.Episode, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return sortAndLimit(append([]*entity.Episode(nil), s.episodes...), limit), nil
}

func (s *stubEpisodeRepo) ListSegments(context.Context, int64) ([]*entity.Segment, error) {
	return nil, nil
}

func (s *stubEpisodeRepo) CountByKindSince(context.Context, string, time.Time) (int, error) {
	return 0, errors.New("not implemented")
}

func sortAndLimit(eps []*entity.Episode, limit int) []*entity.Episode {
	sort.Slice(eps, func(i, j int) bool { return eps[i].PublishedAt.After(eps[j].PublishedAt) })
	if len(eps) > limit {
		eps = eps[:limit]
	}
	return eps
}

// stubTokenRepo reimplements the GetActiveByHash contract over in-memory
// rows so revoked / deactivated / unknown are distinct table cases.
type stubTokenRepo struct {
	byHash           map[string]*entity.FeedToken
	activeSubscriber map[int64]bool
	lookups          int
	err              error
}

func (s *stubTokenRepo) Create(context.Context, *entity.FeedToken) error {
	return errors.New("not implemented")
}

func (s *stubTokenRepo) Get(context.Context, int64) (*entity.FeedToken, error) {
	return nil, errors.New("not implemented")
}

func (s *stubTokenRepo) GetActiveByHash(_ context.Context, tokenHash string) (*entity.FeedToken, error) {
	s.lookups++
	if s.err != nil {
		return nil, s.err
	}
	token, ok := s.byHash[tokenHash]
	if !ok || token.RevokedAt != nil || !s.activeSubscriber[token.SubscriberID] {
		return nil, nil
	}
	return token, nil
}

func (s *stubTokenRepo) ListBySubscriber(context.Context, int64) ([]*entity.FeedToken, error) {
	return nil, nil
}

func (s *stubTokenRepo) Revoke(context.Context, int64, time.Time) error {
	return errors.New("not implemented")
}

type stubAccessLogRepo struct {
	records   []entity.FeedAccessLog
	insertErr error
}

func (s *stubAccessLogRepo) Insert(_ context.Context, log *entity.FeedAccessLog) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	log.ID = int64(len(s.records) + 1)
	log.AccessedAt = time.Now()
	s.records = append(s.records, *log)
	return nil
}

func (s *stubAccessLogRepo) ListRecords(context.Context, *int64, int) ([]*entity.FeedAccessRecord, error) {
	return nil, nil
}

func (s *stubAccessLogRepo) SummarizeBySubscriber(context.Context, time.Time, time.Time) ([]*entity.SubscriberAccessSummary, error) {
	return nil, nil
}

// ---- fixtures ----

type fixture struct {
	server     *Server
	publicMux  *http.ServeMux
	episodes   *stubEpisodeRepo
	tokens     *stubTokenRepo
	accessLogs *stubAccessLogRepo
	plaintext  string // one issued, valid token
	tokenID    int64
}

func newFixture(t *testing.T, cfg Config) *fixture {
	t.Helper()
	if cfg.PublicBaseURL == "" {
		cfg.PublicBaseURL = "https://radio.catchup-feed.com"
	}
	if cfg.MaxItems == 0 {
		cfg.MaxItems = 30
	}
	if cfg.ChannelTitle == "" {
		cfg.ChannelTitle = "pulse radio"
	}

	plaintext, hash, err := entity.GenerateFeedToken()
	require.NoError(t, err)
	token := &entity.FeedToken{ID: 7, SubscriberID: 3, TokenHash: hash, CreatedAt: time.Now()}

	f := &fixture{
		episodes: &stubEpisodeRepo{},
		tokens: &stubTokenRepo{
			byHash:           map[string]*entity.FeedToken{hash: token},
			activeSubscriber: map[int64]bool{3: true},
		},
		accessLogs: &stubAccessLogRepo{},
		plaintext:  plaintext,
		tokenID:    token.ID,
	}
	f.server = NewServer(cfg, f.episodes, f.tokens, f.accessLogs, testLogger())
	f.publicMux = http.NewServeMux()
	f.server.RegisterPublic(f.publicMux, nil)
	return f
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (f *fixture) get(t *testing.T, h http.Handler, target string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ---- token verification (§5.2) ----

func TestVerifyToken(t *testing.T) {
	makeRevoked := func(f *fixture) string {
		plaintext, hash, err := entity.GenerateFeedToken()
		require.NoError(t, err)
		revokedAt := time.Now().Add(-time.Hour)
		f.tokens.byHash[hash] = &entity.FeedToken{ID: 8, SubscriberID: 3, TokenHash: hash, RevokedAt: &revokedAt}
		return plaintext
	}
	makeInactive := func(f *fixture) string {
		plaintext, hash, err := entity.GenerateFeedToken()
		require.NoError(t, err)
		f.tokens.byHash[hash] = &entity.FeedToken{ID: 9, SubscriberID: 4, TokenHash: hash}
		f.tokens.activeSubscriber[4] = false
		return plaintext
	}
	makeUnknown := func(*fixture) string {
		plaintext, err := entity.NewFeedTokenPlaintext()
		require.NoError(t, err)
		return plaintext
	}

	tests := []struct {
		name        string
		token       func(f *fixture) string
		setup       func(f *fixture)
		wantStatus  int
		wantLookups int
		wantLogs    int
	}{
		{
			name:        "valid token serves the feed and records access",
			token:       func(f *fixture) string { return f.plaintext },
			wantStatus:  http.StatusOK,
			wantLookups: 1,
			wantLogs:    1,
		},
		{
			name:        "revoked token answers 404",
			token:       makeRevoked,
			wantStatus:  http.StatusNotFound,
			wantLookups: 1,
		},
		{
			name:        "deactivated subscriber answers 404",
			token:       makeInactive,
			wantStatus:  http.StatusNotFound,
			wantLookups: 1,
		},
		{
			name:        "unknown but well-formed token answers 404",
			token:       makeUnknown,
			wantStatus:  http.StatusNotFound,
			wantLookups: 1,
		},
		{
			name:        "malformed token: too short, rejected before the DB",
			token:       func(*fixture) string { return "abc" },
			wantStatus:  http.StatusNotFound,
			wantLookups: 0,
		},
		{
			name:        "malformed token: invalid base64url char, rejected before the DB",
			token:       func(*fixture) string { return strings.Repeat("a", 42) + "!" },
			wantStatus:  http.StatusNotFound,
			wantLookups: 0,
		},
		{
			name:        "malformed token: padded base64, rejected before the DB",
			token:       func(*fixture) string { return strings.Repeat("a", 42) + "==" },
			wantStatus:  http.StatusNotFound,
			wantLookups: 0,
		},
		{
			name:        "token lookup error answers 503 without leaking validity",
			token:       func(f *fixture) string { return f.plaintext },
			setup:       func(f *fixture) { f.tokens.err = errors.New("db down") },
			wantStatus:  http.StatusServiceUnavailable,
			wantLookups: 1,
		},
		{
			name:        "access log failure never blocks delivery",
			token:       func(f *fixture) string { return f.plaintext },
			setup:       func(f *fixture) { f.accessLogs.insertErr = errors.New("disk full") },
			wantStatus:  http.StatusOK,
			wantLookups: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{})
			token := tt.token(f)
			if tt.setup != nil {
				tt.setup(f)
			}

			rec := f.get(t, f.publicMux, "/feeds/"+token+"/feed.xml", nil)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantLookups, f.tokens.lookups, "DB lookups")
			assert.Len(t, f.accessLogs.records, tt.wantLogs)
		})
	}
}

func TestVerifyToken_AccessLogFields(t *testing.T) {
	f := newFixture(t, Config{})
	rec := f.get(t, f.publicMux, "/feeds/"+f.plaintext+"/feed.xml",
		map[string]string{"User-Agent": "Overcast/2026.7"})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, f.accessLogs.records, 1)
	logRow := f.accessLogs.records[0]
	assert.Equal(t, f.tokenID, logRow.TokenID)
	assert.Nil(t, logRow.EpisodeID, "feed.xml access carries no episode ID")
	require.NotNil(t, logRow.UserAgent)
	assert.Equal(t, "Overcast/2026.7", *logRow.UserAgent)
}

// ---- public feed content (§5.1, C-9) ----

func TestPublicFeed_Content(t *testing.T) {
	f := newFixture(t, Config{})
	now := time.Now()
	f.episodes.episodes = []*entity.Episode{
		sampleEpisode(1, entity.FeedKindPublic, "public one", now.Add(-48*time.Hour)),
		sampleEpisode(2, entity.FeedKindPrivate, "private secret", now.Add(-24*time.Hour)),
		sampleEpisode(3, entity.FeedKindPublic, "public two", now),
	}

	rec := f.get(t, f.publicMux, "/feeds/"+f.plaintext+"/feed.xml", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/rss+xml; charset=utf-8", rec.Header().Get("Content-Type"))

	body := rec.Body.String()
	// C-9: 音声 URL はフィードと同じトークンパス配下。
	assert.Contains(t, body, "https://radio.catchup-feed.com/feeds/"+f.plaintext+"/episodes/3.mp3")
	assert.Contains(t, body, "https://radio.catchup-feed.com/feeds/"+f.plaintext+"/episodes/1.mp3")
	// feed_kind='private' の行は公開フィードに決して現れない。
	assert.NotContains(t, body, "private secret")
	assert.NotContains(t, body, "/episodes/2.mp3")
}

func TestPublicFeed_ListError(t *testing.T) {
	f := newFixture(t, Config{})
	f.episodes.listErr = errors.New("db down")
	rec := f.get(t, f.publicMux, "/feeds/"+f.plaintext+"/feed.xml", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---- channel artwork ----

// artworkRoute is one URL the artwork is reachable at. Every case below
// runs against all four: the fingerprinted URLs (new, content-addressed,
// immutable) and the legacy fixed ones (kept alive because apps that
// subscribed before cache busting still hold them), on both listeners.
type artworkRoute struct {
	name         string
	handler      func(f *fixture) http.Handler
	path         func(f *fixture) string
	cacheControl string
	wantLookups  int
}

func artworkRoutes() []artworkRoute {
	public := func(f *fixture) http.Handler { return f.publicMux }
	private := func(f *fixture) http.Handler { return f.server.PrivateHandler() }
	return []artworkRoute{
		{
			name:         "public fingerprinted",
			handler:      public,
			path:         func(f *fixture) string { return "/feeds/" + f.plaintext + "/artwork/" + artworkFileName() },
			cacheControl: "public, max-age=31536000, immutable",
			wantLookups:  1,
		},
		{
			name:         "public legacy",
			handler:      public,
			path:         func(f *fixture) string { return "/feeds/" + f.plaintext + "/artwork.jpg" },
			cacheControl: "public, max-age=86400",
			wantLookups:  1,
		},
		{
			name:         "private fingerprinted",
			handler:      private,
			path:         func(*fixture) string { return "/private/artwork/" + artworkFileName() },
			cacheControl: "public, max-age=31536000, immutable",
		},
		{
			name:         "private legacy",
			handler:      private,
			path:         func(*fixture) string { return "/private/artwork.jpg" },
			cacheControl: "public, max-age=86400",
		},
		// D-33(4): 正準でない {fp} も画像は返すが immutable は付かない。
		// URL が内容に紐づいていない以上、1年 immutable の根拠がないため。
		{
			name:         "public fingerprinted with a stale fp",
			handler:      public,
			path:         func(f *fixture) string { return "/feeds/" + f.plaintext + "/artwork/deadbeef.jpg" },
			cacheControl: "public, max-age=86400",
			wantLookups:  1,
		},
		{
			name:         "private fingerprinted with a stale fp",
			handler:      private,
			path:         func(*fixture) string { return "/private/artwork/deadbeef.jpg" },
			cacheControl: "public, max-age=86400",
		},
	}
}

func TestArtwork_Serves(t *testing.T) {
	for _, tt := range artworkRoutes() {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, Config{})

			rec := f.get(t, tt.handler(f), tt.path(f), nil)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
			assert.Equal(t, fmt.Sprint(len(artworkJPEG)), rec.Header().Get("Content-Length"))
			// D-33(4): immutable が付くのは現在の fingerprint と一致する
			// 正準 URL だけ。旧 /artwork.jpg も陳腐化した {fp} も、URL が
			// 内容に紐づいていないので 1 日で切れる。
			assert.Equal(t, tt.cacheControl, rec.Header().Get("Cache-Control"))
			assert.Equal(t, artworkETag, rec.Header().Get("ETag"))
			assert.Equal(t, artworkJPEG, rec.Body.Bytes())
			// 埋め込みアセットが実際に JPEG であることのピン留め(差し替え事故検知)。
			require.GreaterOrEqual(t, rec.Body.Len(), 3)
			assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, rec.Body.Bytes()[:3], "embedded artwork must be a JPEG")
			// アートワーク取得はアプリのキャッシュ通信であり、feed.xml のポーリングと
			// 区別できない nil-episode 行を作らない。
			assert.Empty(t, f.accessLogs.records, "artwork fetches are not access-logged")
			// 私的経路は subscriber 概念の外: トークン照会は起きない。
			assert.Equal(t, tt.wantLookups, f.tokens.lookups)
		})
	}
}

// ETag による条件付き GET。旧ルート・新ルート双方で 304 が返ること。
func TestArtwork_ConditionalGet(t *testing.T) {
	conditions := []struct {
		name        string
		ifNoneMatch string
		wantStatus  int
	}{
		{"matching ETag answers 304", artworkETag, http.StatusNotModified},
		{"ETag inside a list answers 304", `"deadbeefdeadbeef", ` + artworkETag, http.StatusNotModified},
		{"wildcard answers 304", "*", http.StatusNotModified},
		{"stale ETag re-sends the image", `"deadbeefdeadbeef"`, http.StatusOK},
	}
	for _, route := range artworkRoutes() {
		for _, c := range conditions {
			t.Run(route.name+"/"+c.name, func(t *testing.T) {
				f := newFixture(t, Config{})

				rec := f.get(t, route.handler(f), route.path(f),
					map[string]string{"If-None-Match": c.ifNoneMatch})

				require.Equal(t, c.wantStatus, rec.Code)
				assert.Equal(t, artworkETag, rec.Header().Get("ETag"),
					"304 でも検証子は返す")
				// 再検証パスでもキャッシュ方針の出し分けは維持される
				// (304 は既存のキャッシュエントリの TTL を更新するため、
				// ここで immutable / 86400 を取り違えると 200 経路の
				// 出し分けが無意味になる)。
				assert.Equal(t, route.cacheControl, rec.Header().Get("Cache-Control"))
				if c.wantStatus == http.StatusNotModified {
					assert.Empty(t, rec.Body.Bytes(), "304 must not carry the image")
				} else {
					assert.Equal(t, artworkJPEG, rec.Body.Bytes())
				}
				assert.Empty(t, f.accessLogs.records, "conditional artwork GETs are not access-logged either")
			})
		}
	}
}

// D-33(3) 縮退許容: 古いフィンガープリントを握ったアプリに 404 を返さない。
// URL が古くても現在の画像が返るほうが、チャンネル画像が消えるより良い。
// ただし D-33(4) により、そこに immutable は付かない。
func TestArtwork_StaleFingerprintServesCurrentImage(t *testing.T) {
	files := []string{"deadbeef.jpg", "00000000.jpg", "artwork.jpg", "whatever"}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			f := newFixture(t, Config{})

			pub := f.get(t, f.publicMux, "/feeds/"+f.plaintext+"/artwork/"+file, nil)
			require.Equal(t, http.StatusOK, pub.Code)
			assert.Equal(t, artworkJPEG, pub.Body.Bytes())
			assert.Equal(t, artworkETag, pub.Header().Get("ETag"), "検証子は常に現在の画像のもの")
			assert.Equal(t, "public, max-age=86400", pub.Header().Get("Cache-Control"),
				"正準でない URL に immutable を付けない")

			priv := f.get(t, f.server.PrivateHandler(), "/private/artwork/"+file, nil)
			require.Equal(t, http.StatusOK, priv.Code)
			assert.Equal(t, artworkJPEG, priv.Body.Bytes())
			assert.Equal(t, "public, max-age=86400", priv.Header().Get("Cache-Control"))

			assert.Empty(t, f.accessLogs.records)
		})
	}
}

// http.ServeContent 委譲の副作用として artwork にも Range が効く(D-33 の
// 「害はない」判断のピン留め)。mp3 側の C-10 と同じ形で検証する。
func TestArtwork_RangeRequest(t *testing.T) {
	for _, route := range artworkRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f := newFixture(t, Config{})

			rec := f.get(t, route.handler(f), route.path(f), map[string]string{"Range": "bytes=0-9"})

			require.Equal(t, http.StatusPartialContent, rec.Code)
			assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
			assert.Equal(t, fmt.Sprintf("bytes 0-9/%d", len(artworkJPEG)), rec.Header().Get("Content-Range"))
			assert.Equal(t, artworkJPEG[:10], rec.Body.Bytes())
			assert.Equal(t, artworkETag, rec.Header().Get("ETag"))
			assert.Empty(t, f.accessLogs.records)
		})
	}
}

// ポッドキャストアプリはアートワークに HEAD を投げることがある。GET
// パターンが HEAD も拾うのは Go 1.22+ の ServeMux の仕様であり、将来
// ハンドラ側に r.Method ガードが入ると静かに壊れるため固定する。
func TestArtwork_HeadRequest(t *testing.T) {
	for _, route := range artworkRoutes() {
		t.Run(route.name, func(t *testing.T) {
			f := newFixture(t, Config{})

			req := httptest.NewRequest(http.MethodHead, route.path(f), nil)
			rec := httptest.NewRecorder()
			route.handler(f).ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
			assert.Equal(t, fmt.Sprint(len(artworkJPEG)), rec.Header().Get("Content-Length"),
				"HEAD でも本体サイズを申告する")
			assert.Equal(t, route.cacheControl, rec.Header().Get("Cache-Control"))
			assert.Equal(t, artworkETag, rec.Header().Get("ETag"))
			assert.Empty(t, rec.Body.Bytes(), "HEAD must not carry the image")
			assert.Empty(t, f.accessLogs.records)
		})
	}
}

func TestArtwork_PublicRoute_InvalidTokens(t *testing.T) {
	paths := []struct {
		name string
		path func(token string) string
	}{
		{"fingerprinted", func(token string) string { return "/feeds/" + token + "/artwork/" + artworkFileName() }},
		{"legacy", func(token string) string { return "/feeds/" + token + "/artwork.jpg" }},
	}
	tests := []struct {
		name        string
		token       func(f *fixture) string
		wantLookups int
	}{
		{
			name: "revoked token answers 404",
			token: func(f *fixture) string {
				plaintext, hash, err := entity.GenerateFeedToken()
				require.NoError(t, err)
				revokedAt := time.Now().Add(-time.Hour)
				f.tokens.byHash[hash] = &entity.FeedToken{ID: 8, SubscriberID: 3, TokenHash: hash, RevokedAt: &revokedAt}
				return plaintext
			},
			wantLookups: 1,
		},
		{
			name: "deactivated subscriber answers 404",
			token: func(f *fixture) string {
				plaintext, hash, err := entity.GenerateFeedToken()
				require.NoError(t, err)
				f.tokens.byHash[hash] = &entity.FeedToken{ID: 9, SubscriberID: 4, TokenHash: hash}
				f.tokens.activeSubscriber[4] = false
				return plaintext
			},
			wantLookups: 1,
		},
		{
			name: "unknown but well-formed token answers 404",
			token: func(*fixture) string {
				plaintext, err := entity.NewFeedTokenPlaintext()
				require.NoError(t, err)
				return plaintext
			},
			wantLookups: 1,
		},
		{
			name:        "malformed token rejected before the DB",
			token:       func(*fixture) string { return "abc" },
			wantLookups: 0,
		},
	}
	for _, p := range paths {
		for _, tt := range tests {
			t.Run(p.name+"/"+tt.name, func(t *testing.T) {
				f := newFixture(t, Config{})
				token := tt.token(f)

				rec := f.get(t, f.publicMux, p.path(token), nil)

				assert.Equal(t, http.StatusNotFound, rec.Code)
				assert.Equal(t, tt.wantLookups, f.tokens.lookups, "DB lookups")
				assert.NotEqual(t, "image/jpeg", rec.Header().Get("Content-Type"),
					"failed verification must not leak the artwork")
				assert.Empty(t, f.accessLogs.records)
			})
		}
	}
}

// 公開フィードのチャンネル画像 URL は enclosure と同じトークンパス配下(C-9)
// で、かつ画像の内容が変わったら URL も変わる(アプリ側キャッシュの破棄)。
func TestPublicFeed_ChannelArtworkTags(t *testing.T) {
	f := newFixture(t, Config{})
	f.episodes.episodes = []*entity.Episode{sampleEpisode(1, entity.FeedKindPublic, "e", time.Now())}

	rec := f.get(t, f.publicMux, "/feeds/"+f.plaintext+"/feed.xml", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	artworkURL := "https://radio.catchup-feed.com/feeds/" + f.plaintext + "/artwork/" + artworkFingerprint + ".jpg"
	assert.Contains(t, body, `<itunes:image href="`+artworkURL+`"`)
	assert.Contains(t, body, "<url>"+artworkURL+"</url>")
	// 固定 URL に戻ると Apple / AntennaPod が旧ロゴを持ち続ける(PR #104 の再発)。
	assert.NotContains(t, body, "/artwork.jpg")
	// フィードが指す URL は実際に配信されているルートでなければならない。
	art := f.get(t, f.publicMux, "/feeds/"+f.plaintext+"/artwork/"+artworkFingerprint+".jpg", nil)
	assert.Equal(t, http.StatusOK, art.Code)
}

// 私的フィードのチャンネル画像 URL は /private 配下で、公開ホストを広告しない。
func TestPrivateFeed_ChannelArtworkTags(t *testing.T) {
	f := newFixture(t, Config{})
	f.episodes.episodes = []*entity.Episode{sampleEpisode(9, entity.FeedKindPrivate, "j", time.Now())}

	rec := f.get(t, f.server.PrivateHandler(), "http://pi.tailnet:8081/private/feed.xml", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	artworkURL := "http://pi.tailnet:8081/private/artwork/" + artworkFingerprint + ".jpg"
	assert.Contains(t, body, `<itunes:image href="`+artworkURL+`"`)
	assert.Contains(t, body, "<url>"+artworkURL+"</url>")
	assert.NotContains(t, body, "radio.catchup-feed.com")
	assert.NotContains(t, body, "/artwork.jpg")
}

// ---- mp3 delivery (C-10) ----

// newAudioFixture writes a 16-byte fake mp3 into a temp audio dir and
// registers episode 1 (public) pointing at it with an absolute path.
func newAudioFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	dir := t.TempDir()
	content := "0123456789abcdef"
	audioPath := filepath.Join(dir, "2026-07-04.mp3")
	require.NoError(t, os.WriteFile(audioPath, []byte(content), 0o644))

	f := newFixture(t, Config{AudioDir: dir})
	ep := sampleEpisode(1, entity.FeedKindPublic, "pulse", time.Now())
	ep.AudioPath = audioPath
	ep.AudioBytes = int64(len(content))
	f.episodes.episodes = []*entity.Episode{ep}
	return f, content
}

func (f *fixture) episodeURL(id string) string {
	return "/feeds/" + f.plaintext + "/episodes/" + id
}

func TestPublicEpisode_FullDownload(t *testing.T) {
	f, content := newAudioFixture(t)

	rec := f.get(t, f.publicMux, f.episodeURL("1.mp3"), nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "audio/mpeg", rec.Header().Get("Content-Type"))
	assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
	assert.Equal(t, content, rec.Body.String())
}

func TestPublicEpisode_RangeRequests(t *testing.T) {
	tests := []struct {
		name             string
		rangeHeader      string
		wantStatus       int
		wantBody         string
		wantContentRange string
	}{
		{
			name:             "leading range",
			rangeHeader:      "bytes=0-4",
			wantStatus:       http.StatusPartialContent,
			wantBody:         "01234",
			wantContentRange: "bytes 0-4/16",
		},
		{
			name:             "open-ended tail range",
			rangeHeader:      "bytes=10-",
			wantStatus:       http.StatusPartialContent,
			wantBody:         "abcdef",
			wantContentRange: "bytes 10-15/16",
		},
		{
			name:             "suffix range",
			rangeHeader:      "bytes=-3",
			wantStatus:       http.StatusPartialContent,
			wantBody:         "def",
			wantContentRange: "bytes 13-15/16",
		},
		{
			name:             "exact last byte",
			rangeHeader:      "bytes=15-15",
			wantStatus:       http.StatusPartialContent,
			wantBody:         "f",
			wantContentRange: "bytes 15-15/16",
		},
		{
			name:        "range start beyond EOF is unsatisfiable",
			rangeHeader: "bytes=16-",
			wantStatus:  http.StatusRequestedRangeNotSatisfiable,
		},
		{
			// http.ServeContent answers 416 for an unparsable Range header.
			name:        "garbage range is unsatisfiable",
			rangeHeader: "bytes=oops",
			wantStatus:  http.StatusRequestedRangeNotSatisfiable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newAudioFixture(t)
			rec := f.get(t, f.publicMux, f.episodeURL("1.mp3"), map[string]string{"Range": tt.rangeHeader})

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.Equal(t, tt.wantBody, rec.Body.String())
			}
			if tt.wantContentRange != "" {
				assert.Equal(t, tt.wantContentRange, rec.Header().Get("Content-Range"))
			}
		})
	}
}

func TestPublicEpisode_NotFoundCases(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		setup func(f *fixture, audioDir string)
	}{
		{name: "unknown episode id", file: "999.mp3"},
		{name: "non-numeric file name", file: "abc.mp3"},
		{name: "missing mp3 suffix", file: "1"},
		{name: "signed id is rejected", file: "+1.mp3"},
		{
			name: "private episode is invisible on the public route",
			file: "1.mp3",
			setup: func(f *fixture, _ string) {
				f.episodes.episodes[0].FeedKind = entity.FeedKindPrivate
			},
		},
		{
			name: "file listed in DB but missing on disk",
			file: "1.mp3",
			setup: func(f *fixture, _ string) {
				require.NoError(t, os.Remove(f.episodes.episodes[0].AudioPath))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newAudioFixture(t)
			if tt.setup != nil {
				tt.setup(f, f.server.cfg.AudioDir)
			}
			rec := f.get(t, f.publicMux, f.episodeURL(tt.file), nil)
			assert.Equal(t, http.StatusNotFound, rec.Code)
		})
	}
}

func TestPublicEpisode_PathTraversal(t *testing.T) {
	// A secret outside the audio dir must not be reachable no matter what
	// audio_path the DB row carries.
	writeSecret := func(t *testing.T, dir string) string {
		t.Helper()
		outside := filepath.Join(filepath.Dir(dir), "secret-"+filepath.Base(dir)+".mp3")
		require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o644))
		t.Cleanup(func() { _ = os.Remove(outside) })
		return outside
	}

	tests := []struct {
		name      string
		audioPath func(t *testing.T, audioDir string) string
	}{
		{
			name: "absolute path outside the audio dir",
			audioPath: func(t *testing.T, dir string) string {
				return writeSecret(t, dir)
			},
		},
		{
			name: "absolute path escaping via ..",
			audioPath: func(t *testing.T, dir string) string {
				outside := writeSecret(t, dir)
				return filepath.Join(dir, "..", filepath.Base(outside))
			},
		},
		{
			name: "relative path escaping via ..",
			audioPath: func(t *testing.T, dir string) string {
				outside := writeSecret(t, dir)
				return "../" + filepath.Base(outside)
			},
		},
		{
			name: "symlink inside the dir pointing outside",
			audioPath: func(t *testing.T, dir string) string {
				if runtime.GOOS == "windows" {
					t.Skip("symlinks not exercised on windows")
				}
				outside := writeSecret(t, dir)
				link := filepath.Join(dir, "link.mp3")
				require.NoError(t, os.Symlink(outside, link))
				return link
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newAudioFixture(t)
			f.episodes.episodes[0].AudioPath = tt.audioPath(t, f.server.cfg.AudioDir)

			rec := f.get(t, f.publicMux, f.episodeURL("1.mp3"), nil)

			assert.Equal(t, http.StatusNotFound, rec.Code)
			assert.NotContains(t, rec.Body.String(), "secret")
		})
	}
}

func TestPublicEpisode_AccessLogCarriesEpisodeID(t *testing.T) {
	f, _ := newAudioFixture(t)
	rec := f.get(t, f.publicMux, f.episodeURL("1.mp3"), nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, f.accessLogs.records, 1)
	require.NotNil(t, f.accessLogs.records[0].EpisodeID)
	assert.Equal(t, int64(1), *f.accessLogs.records[0].EpisodeID)
}

func TestAudioRelPath(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name      string
		audioDir  string
		audioPath string
		wantRel   string
		wantOK    bool
	}{
		{"absolute inside", "/data/episodes", "/data/episodes/a.mp3", "a.mp3", true},
		{"absolute nested inside", "/data/episodes", "/data/episodes/2026/a.mp3", "2026" + sep + "a.mp3", true},
		{"absolute outside", "/data/episodes", "/etc/passwd", "", false},
		{"absolute sibling prefix", "/data/episodes", "/data/episodes-evil/a.mp3", "", false},
		{"absolute parent escape", "/data/episodes", "/data/episodes/../a.mp3", "", false},
		{"relative plain", "/data/episodes", "a.mp3", "a.mp3", true},
		{"relative dot-cleaned", "/data/episodes", "./a.mp3", "a.mp3", true},
		{"relative escape", "/data/episodes", "../a.mp3", "", false},
		{"relative nested escape", "/data/episodes", "x/../../a.mp3", "", false},
		{"empty path", "/data/episodes", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel, ok := audioRelPath(tt.audioDir, tt.audioPath)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantRel, rel)
			}
		})
	}
}

// ---- private listener (§3.1, C-5) ----

func TestPrivateFeed_NoAuthAllKinds(t *testing.T) {
	f := newFixture(t, Config{})
	now := time.Now()
	f.episodes.episodes = []*entity.Episode{
		sampleEpisode(1, entity.FeedKindPublic, "public one", now.Add(-24*time.Hour)),
		sampleEpisode(2, entity.FeedKindPrivate, "private journal", now),
	}
	handler := f.server.PrivateHandler()

	rec := f.get(t, handler, "http://pi.tailnet:8081/private/feed.xml", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "private journal")
	assert.Contains(t, body, "public one")
	// enclosure は /private 配下、ベース URL はリクエストの Host から導出。
	assert.Contains(t, body, `url="http://pi.tailnet:8081/private/episodes/2.mp3"`)
	assert.Contains(t, body, `url="http://pi.tailnet:8081/private/episodes/1.mp3"`)
	// 私的経路は subscriber 概念の外: アクセスログもトークン照会も発生しない。
	assert.Empty(t, f.accessLogs.records)
	assert.Zero(t, f.tokens.lookups)
}

// TestPrivateFeed_CollapsesDailyPair pins the Phase 3 §7.1 fold: the radio
// batch inserts a public and a private episode with the SAME published_at
// (one selection timestamp per run), and the private feed must show only
// the private one — the public twin is a strict subset of it. Unpaired
// public episodes (私的版の生成が縮退した日, pre-Phase 3 の既存行) keep
// appearing; the public feed path is untouched (§12-1).
func TestPrivateFeed_CollapsesDailyPair(t *testing.T) {
	now := time.Now()
	f := newFixture(t, Config{})
	f.episodes.episodes = []*entity.Episode{
		sampleEpisode(1, entity.FeedKindPublic, "twin public edition", now),
		sampleEpisode(2, entity.FeedKindPrivate, "twin private edition", now),
		sampleEpisode(3, entity.FeedKindPublic, "solo public edition", now.Add(-24*time.Hour)),
	}

	rec := f.get(t, f.server.PrivateHandler(), "http://pi.tailnet:8081/private/feed.xml", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "twin private edition")
	assert.NotContains(t, body, "twin public edition",
		"同一 published_at の公開版は私的フィードでは畳まれる(毎朝の二重掲載防止)")
	assert.Contains(t, body, "solo public edition",
		"私的版が欠けた日は公開版がフォールバックとして残る (§9)")
}

func TestPrivateFeed_ConfiguredBaseURL(t *testing.T) {
	f := newFixture(t, Config{PrivateBaseURL: "http://100.64.0.1:8081"})
	f.episodes.episodes = []*entity.Episode{sampleEpisode(9, entity.FeedKindPrivate, "j", time.Now())}

	rec := f.get(t, f.server.PrivateHandler(), "http://whatever/private/feed.xml", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `url="http://100.64.0.1:8081/private/episodes/9.mp3"`)
}

func TestPrivateEpisode_ServesPrivateKindWithRange(t *testing.T) {
	f, _ := newAudioFixture(t)
	f.episodes.episodes[0].FeedKind = entity.FeedKindPrivate
	handler := f.server.PrivateHandler()

	rec := f.get(t, handler, "/private/episodes/1.mp3", map[string]string{"Range": "bytes=0-3"})

	require.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, "0123", rec.Body.String())
	assert.Equal(t, "bytes 0-3/16", rec.Header().Get("Content-Range"))
	assert.Empty(t, f.accessLogs.records, "private deliveries are not access-logged")
}

func TestPrivateEpisode_UnknownID(t *testing.T) {
	f, _ := newAudioFixture(t)
	rec := f.get(t, f.server.PrivateHandler(), "/private/episodes/42.mp3", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestEpisodeIDFromFile(t *testing.T) {
	tests := []struct {
		file   string
		wantID int64
		wantOK bool
	}{
		{"1.mp3", 1, true},
		{"123456.mp3", 123456, true},
		{"0.mp3", 0, false},
		{"-1.mp3", 0, false},
		{"+1.mp3", 0, false},
		{"1.mp3.mp3", 0, false},
		{"1.wav", 0, false},
		{".mp3", 0, false},
		{"", 0, false},
		{"1e3.mp3", 0, false},
		{fmt.Sprintf("%d.mp3", int64(1)<<62), int64(1) << 62, true},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			id, ok := episodeIDFromFile(tt.file)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

// ---- review follow-ups ----

// FK 安全性のピン留め: 実在しないエピソード ID への正規トークンアクセスは
// feed_access_logs に行を作らない(episodes への FK 違反を発生させない)。
func TestPublicEpisode_NoAccessLogForMissingEpisode(t *testing.T) {
	f, _ := newAudioFixture(t)

	rec := f.get(t, f.publicMux, f.episodeURL("999.mp3"), nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, f.accessLogs.records, "missing episodes must not be access-logged")
}

func TestPublicEpisode_NoAccessLogForPrivateEpisode(t *testing.T) {
	f, _ := newAudioFixture(t)
	f.episodes.episodes[0].FeedKind = entity.FeedKindPrivate

	rec := f.get(t, f.publicMux, f.episodeURL("1.mp3"), nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, f.accessLogs.records)
}

// /feeds/ 配下のパターン不一致は JWT 保護の "/" ハンドラへ落ちず、
// フィード側のキャッチオールが 404 を返す(トークン付きリクエストが
// 認証スタックに触れない)。
func TestFeedsCatchAllShieldsAuthStack(t *testing.T) {
	f := newFixture(t, Config{})
	// cmd/server の "/" (JWT Authz) を模したフォールバック。
	f.publicMux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	tests := []struct {
		name   string
		method string
		target string
	}{
		{"trailing slash after mp3", http.MethodGet, "/feeds/" + f.plaintext + "/episodes/1.mp3/"},
		{"stray segment", http.MethodGet, "/feeds/" + f.plaintext + "/episodes/1.mp3/extra"},
		{"token without resource", http.MethodGet, "/feeds/" + f.plaintext + "/"},
		{"bare token segment", http.MethodGet, "/feeds/" + f.plaintext},
		{"unsupported method", http.MethodPost, "/feeds/" + f.plaintext + "/feed.xml"},
		{"artwork without a file segment", http.MethodGet, "/feeds/" + f.plaintext + "/artwork/"},
		{"artwork with a stray segment", http.MethodGet, "/feeds/" + f.plaintext + "/artwork/" + artworkFileName() + "/extra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			rec := httptest.NewRecorder()
			f.publicMux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusNotFound, rec.Code,
				"unmatched /feeds/ requests must 404, not reach the auth stack")
			assert.Zero(t, f.tokens.lookups, "the catch-all must not verify tokens")
		})
	}
}

// 私的フィードの channel <link> は私的ベース URL を指す(公開ドメインを
// 私的フィードに広告しない)。
func TestPrivateFeed_ChannelLinkUsesPrivateBase(t *testing.T) {
	f := newFixture(t, Config{PrivateBaseURL: "http://100.64.0.1:8081"})
	f.episodes.episodes = []*entity.Episode{sampleEpisode(9, entity.FeedKindPrivate, "j", time.Now())}

	rec := f.get(t, f.server.PrivateHandler(), "http://whatever/private/feed.xml", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "<link>http://100.64.0.1:8081</link>")
	assert.NotContains(t, body, "radio.catchup-feed.com")
}
