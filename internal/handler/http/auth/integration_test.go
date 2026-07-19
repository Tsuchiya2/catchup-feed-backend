package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	authservice "catchup-feed/internal/service/auth"
	viewerUC "catchup-feed/internal/usecase/viewer"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// memViewerRepo is an in-memory repository.ViewerRepository covering the
// login / re-validation paths the integration tests exercise.
type memViewerRepo struct {
	mu      sync.Mutex
	viewers map[string]*entity.Viewer // by email
}

var _ repository.ViewerRepository = (*memViewerRepo)(nil)

func newMemViewerRepo() *memViewerRepo {
	return &memViewerRepo{viewers: map[string]*entity.Viewer{}}
}

func (m *memViewerRepo) Create(_ context.Context, v *entity.Viewer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.ID = int64(len(m.viewers) + 1)
	v.CreatedAt, v.UpdatedAt = time.Now(), time.Now()
	m.viewers[v.Email] = v
	return nil
}

func (m *memViewerRepo) Get(_ context.Context, id int64) (*entity.Viewer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.viewers {
		if v.ID == id {
			return v, nil
		}
	}
	return nil, nil
}

func (m *memViewerRepo) List(_ context.Context) ([]*entity.Viewer, error) { return nil, nil }
func (m *memViewerRepo) Update(_ context.Context, _ *entity.Viewer) error { return nil }

func (m *memViewerRepo) Deactivate(_ context.Context, id int64, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.viewers {
		if v.ID == id && v.DeactivatedAt == nil {
			at := t
			v.DeactivatedAt = &at
		}
	}
	return nil
}

func (m *memViewerRepo) Reactivate(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.viewers {
		if v.ID == id {
			v.DeactivatedAt = nil
		}
	}
	return nil
}

func (m *memViewerRepo) Delete(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for email, v := range m.viewers {
		if v.ID == id {
			delete(m.viewers, email)
		}
	}
	return nil
}

func (m *memViewerRepo) GetActiveByEmail(_ context.Context, email string) (*entity.Viewer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.viewers[email]
	if !ok || v.DeactivatedAt != nil {
		return nil, nil
	}
	return v, nil
}

const (
	testViewerEmail    = "friend@example.com"
	testViewerPassword = "viewer-password-1"
)

// newIntegrationServer wires TokenHandler and the role-aware middleware the
// same way cmd/server does: /auth/token and /auth/logout are public, the
// protected mux carries GET /sources, GET /auth/me and an admin catch-all,
// and the whole mux is wrapped in AuthzWithViewer backed by a viewer
// use case over an in-memory repository (D-27). It returns the seeded
// active viewer's usecase service for test-side mutation (deactivation).
func newIntegrationServer(t *testing.T) (http.Handler, *viewerUC.Service) {
	t.Helper()
	setAdminEnv(t, testAdminUser, testHash(t, testPassword))
	t.Setenv("JWT_SECRET", testJWTSecret)

	repo := newMemViewerRepo()
	hash, err := bcrypt.GenerateFromPassword([]byte(testViewerPassword), bcrypt.MinCost)
	require.NoError(t, err)
	require.NoError(t, repo.Create(context.Background(), &entity.Viewer{
		Name: "Friend", Email: testViewerEmail, PasswordHash: string(hash),
	}))
	viewerSvc := &viewerUC.Service{Viewers: repo}

	protected := http.NewServeMux()
	protected.HandleFunc("GET /sources", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sources"))
	})
	protected.Handle("GET /auth/me", MeHandler())
	protected.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("protected"))
	})

	// Mirror cmd/server's two-level routing: a public mux owns /auth/token
	// and /auth/logout, and the root mux delegates those exact paths to it
	// while sending everything else (including /auth/me) to the
	// role-aware-protected mux. This structure matters for method matching —
	// the POST-only logout route must 405 other methods instead of falling
	// through to the catch-all.
	publicMux := http.NewServeMux()
	publicMux.Handle("/auth/token", TokenHandler(authservice.NewAuthService(NewAdminAuthProvider()), viewerSvc))
	// logout is POST-only so a reflected GET (<img src=".../auth/logout">)
	// cannot force-logout a victim.
	publicMux.Handle("POST /auth/logout", LogoutHandler())

	mux := http.NewServeMux()
	mux.Handle("/auth/token", publicMux)
	mux.Handle("/auth/logout", publicMux)
	mux.Handle("/", AuthzWithViewer(viewerSvc)(protected))
	return mux, viewerSvc
}

// login posts credentials and returns the response recorder.
func login(t *testing.T, server http.Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

// loginToken logs in and returns the issued JWT.
func loginToken(t *testing.T, server http.Handler, email, password string) string {
	t.Helper()
	rec := login(t, server, email, password)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)
	return resp.Token
}

// get performs an authenticated GET and returns the recorder.
func get(t *testing.T, server http.Handler, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

// TestIntegration_AdminLoginFlow covers the full path: bcrypt login →
// issued JWT (role=admin) → access to a protected endpoint (非回帰).
func TestIntegration_AdminLoginFlow(t *testing.T) {
	server, _ := newIntegrationServer(t)

	token := loginToken(t, server, testAdminUser, testPassword)

	rec := get(t, server, token, "/subscribers")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "protected", rec.Body.String())

	// /auth/me reports the admin role (D-27 (5)).
	meRec := get(t, server, token, "/auth/me")
	require.Equal(t, http.StatusOK, meRec.Code)
	var me MeResponse
	require.NoError(t, json.Unmarshal(meRec.Body.Bytes(), &me))
	assert.Equal(t, testAdminUser, me.Sub)
	assert.Equal(t, RoleAdmin, me.Role)
}

// TestIntegration_ViewerLoginFlow covers the D-27 happy path: viewer login
// → issued JWT (role=viewer) → allowlisted reads OK, everything else 403.
func TestIntegration_ViewerLoginFlow(t *testing.T) {
	server, _ := newIntegrationServer(t)

	token := loginToken(t, server, testViewerEmail, testViewerPassword)

	// Allowlisted: GET /sources and GET /auth/me.
	assert.Equal(t, http.StatusOK, get(t, server, token, "/sources").Code)

	meRec := get(t, server, token, "/auth/me")
	require.Equal(t, http.StatusOK, meRec.Code)
	var me MeResponse
	require.NoError(t, json.Unmarshal(meRec.Body.Bytes(), &me))
	assert.Equal(t, testViewerEmail, me.Sub)
	assert.Equal(t, RoleViewer, me.Role)

	// Everything else is admin-only (D-27 (3)).
	for _, path := range []string{"/subscribers", "/articles", "/viewers", "/sources/search", "/access-logs"} {
		rec := get(t, server, token, path)
		assert.Equal(t, http.StatusForbidden, rec.Code, "GET %s must be 403 for viewers", path)
	}

	// Writes are admin-only too.
	req := httptest.NewRequest(http.MethodPost, "/sources", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestIntegration_DeactivatedViewer covers immediate cutoff (D-27 (4)):
// deactivation blocks both new logins and requests carrying an existing,
// still-unexpired JWT.
func TestIntegration_DeactivatedViewer(t *testing.T) {
	server, viewerSvc := newIntegrationServer(t)

	token := loginToken(t, server, testViewerEmail, testViewerPassword)
	require.Equal(t, http.StatusOK, get(t, server, token, "/sources").Code)

	_, err := viewerSvc.SetActive(context.Background(), 1, false)
	require.NoError(t, err)

	// The existing JWT is cut off on the next request, before expiry.
	assert.Equal(t, http.StatusForbidden, get(t, server, token, "/sources").Code)
	// New logins are rejected too.
	assert.Equal(t, http.StatusUnauthorized, login(t, server, testViewerEmail, testViewerPassword).Code)

	// Reactivation restores both.
	_, err = viewerSvc.SetActive(context.Background(), 1, true)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, get(t, server, token, "/sources").Code)
}

func TestIntegration_LoginRejectsBadCredentials(t *testing.T) {
	server, _ := newIntegrationServer(t)

	tests := []struct {
		name     string
		email    string
		password string
	}{
		{"wrong admin password", testAdminUser, "totally-wrong-password"},
		{"unknown user", "demo@example.com", testPassword},
		{"viewer wrong password", testViewerEmail, "totally-wrong-password"},
		{"empty credentials", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := login(t, server, tt.email, tt.password)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.NotContains(t, rec.Body.String(), "token\":")
		})
	}
}

// TestIntegration_LegacyTokenRejected replays the pre-D-27 scenario end to
// end: a role-less token signed with the current secret must never reach
// the API (403, C-20 rule re-read), a viewer-role token for a viewer that
// does not exist in the DB is rejected (403), and an unsigned tamper
// attempt must fail signature validation (401).
func TestIntegration_LegacyTokenRejected(t *testing.T) {
	server, _ := newIntegrationServer(t)

	roleLessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": testAdminUser,
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}).SignedString([]byte(testJWTSecret))
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, get(t, server, roleLessToken, "/subscribers").Code)
	assert.Equal(t, http.StatusForbidden, get(t, server, roleLessToken, "/sources").Code)

	ghostViewerToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "ghost@example.com",
		"role": RoleViewer,
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}).SignedString([]byte(testJWTSecret))
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, get(t, server, ghostViewerToken, "/sources").Code)

	tampered := tamperSub(t, ghostViewerToken, testAdminUser)
	assert.Equal(t, http.StatusUnauthorized, get(t, server, tampered, "/subscribers").Code)
}

func TestIntegration_ProtectedWithoutToken(t *testing.T) {
	server, _ := newIntegrationServer(t)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestIntegration_LogoutMethodRestriction verifies /auth/logout is POST-only,
// wired the same way cmd/server registers it. A reflected GET
// (<img src=".../auth/logout">) must be rejected with 405 and must NOT expire
// the auth cookie, closing the GET-CSRF force-logout vector. POST still
// clears the cookie with 204.
func TestIntegration_LogoutMethodRestriction(t *testing.T) {
	server, _ := newIntegrationServer(t)

	tests := []struct {
		name          string
		method        string
		wantCode      int
		wantSetCookie bool
	}{
		{name: "GET is rejected without clearing cookie", method: http.MethodGet, wantCode: http.StatusMethodNotAllowed, wantSetCookie: false},
		{name: "HEAD is rejected", method: http.MethodHead, wantCode: http.StatusMethodNotAllowed, wantSetCookie: false},
		{name: "POST clears the cookie", method: http.MethodPost, wantCode: http.StatusNoContent, wantSetCookie: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/auth/logout", nil)
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)

			c := findAuthCookie(t, rec)
			if tt.wantSetCookie {
				require.NotNil(t, c, "POST logout must emit an expiring Set-Cookie")
				assert.Empty(t, c.Value)
				assert.True(t, c.MaxAge < 0, "logout cookie must delete (Max-Age<=0)")
			} else {
				assert.Nil(t, c, "rejected method must not emit a Set-Cookie (no forced logout)")
			}

			// stdlib ServeMux advertises the allowed methods on 405 for the
			// reflected-GET vector (the CSRF path). HEAD is handled specially
			// by ServeMux and may omit Allow, so only assert it for GET.
			if tt.method == http.MethodGet {
				assert.Contains(t, rec.Header().Get("Allow"), http.MethodPost)
			}
		})
	}
}
