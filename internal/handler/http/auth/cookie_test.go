package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findAuthCookie returns the auth cookie from a response, or nil.
func findAuthCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookieName {
			return c
		}
	}
	return nil
}

// TestTokenHandler_SetsAuthCookie verifies a successful login emits a
// correctly-attributed HttpOnly cookie carrying the JWT (D-22).
func TestTokenHandler_SetsAuthCookie(t *testing.T) {
	handler := TokenHandler(newTestAuthService(t), nil)

	body := `{"email":"` + testAdminUser + `","password":"` + testPassword + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	c := findAuthCookie(t, rec)
	require.NotNil(t, c, "auth cookie must be set on successful login")

	assert.NotEmpty(t, c.Value, "cookie must carry the JWT")
	assert.True(t, c.HttpOnly, "cookie must be HttpOnly")
	assert.True(t, c.Secure, "cookie must be Secure by default")
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite, "cookie must be SameSite=Strict")
	assert.Equal(t, "/", c.Path)
	assert.Equal(t, int(tokenTTL.Seconds()), c.MaxAge, "Max-Age must equal the JWT TTL")

	// The cookie value must be a validly-signed JWT that Authz accepts.
	mw := Authz(okHandler())
	protected := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	protected.AddCookie(&http.Cookie{Name: authCookieName, Value: c.Value})
	protectedRec := httptest.NewRecorder()
	mw.ServeHTTP(protectedRec, protected)
	assert.Equal(t, http.StatusOK, protectedRec.Code)
}

// TestTokenHandler_CookieDomain covers the AUTH_COOKIE_DOMAIN env handling:
// unset -> no Domain attribute (host-scoped, localhost dev); set -> emitted.
func TestTokenHandler_CookieDomain(t *testing.T) {
	tests := []struct {
		name       string
		domainEnv  string
		setDomain  bool
		wantDomain string
	}{
		{name: "unset omits Domain attribute", setDomain: false, wantDomain: ""},
		{name: "empty omits Domain attribute", setDomain: true, domainEnv: "", wantDomain: ""},
		// net/http normalizes a leading-dot Domain to the bare host on the
		// wire (RFC 6265 §4.1.2.3 strips the dot; both forms match all
		// subdomains identically), so the emitted attribute is "Domain=<host>".
		{name: "production apex domain (dot normalized away)", setDomain: true, domainEnv: ".catchup-feed.com", wantDomain: "catchup-feed.com"},
		{name: "production apex domain (bare)", setDomain: true, domainEnv: "catchup-feed.com", wantDomain: "catchup-feed.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := TokenHandler(newTestAuthService(t), nil)
			if tt.setDomain {
				t.Setenv(EnvCookieDomain, tt.domainEnv)
			}

			body := `{"email":"` + testAdminUser + `","password":"` + testPassword + `"}`
			req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)

			c := findAuthCookie(t, rec)
			require.NotNil(t, c)

			// Assert on the raw Set-Cookie header: net/http's cookie parser
			// (used by rec.Result().Cookies()) strips a leading dot from
			// Domain, so it cannot distinguish ".catchup-feed.com" from
			// "catchup-feed.com". The wire header is what the browser sees.
			setCookie := rec.Header().Get("Set-Cookie")
			if tt.wantDomain == "" {
				assert.NotContains(t, setCookie, "Domain=",
					"no Domain attribute must be emitted when AUTH_COOKIE_DOMAIN is empty")
			} else {
				assert.Contains(t, setCookie, "Domain="+tt.wantDomain,
					"Domain attribute must appear on the wire (net/http-normalized form)")
			}
		})
	}
}

// TestTokenHandler_CookieAlwaysSecure verifies the Secure attribute is pinned
// on: it cannot be disabled, and a leftover AUTH_COOKIE_SECURE=false env has no
// effect (the toggle was removed so gosec can prove Secure statically, and to
// eliminate any path that ships a non-Secure auth cookie in production).
func TestTokenHandler_CookieAlwaysSecure(t *testing.T) {
	handler := TokenHandler(newTestAuthService(t), nil)
	// A stale env from an old deployment must not weaken the cookie.
	t.Setenv("AUTH_COOKIE_SECURE", "false")

	body := `{"email":"` + testAdminUser + `","password":"` + testPassword + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	c := findAuthCookie(t, rec)
	require.NotNil(t, c)
	assert.True(t, c.Secure, "auth cookie must always carry the Secure attribute")
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "Secure")
}

// TestTokenHandler_NoCookieOnFailure verifies no cookie leaks on bad creds.
func TestTokenHandler_NoCookieOnFailure(t *testing.T) {
	handler := TokenHandler(newTestAuthService(t), nil)

	body := `{"email":"` + testAdminUser + `","password":"wrong-password-123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	assert.Nil(t, findAuthCookie(t, rec), "no auth cookie may be set on failed login")
}

// TestLogoutHandler verifies logout emits an expiring cookie and is idempotent.
func TestLogoutHandler(t *testing.T) {
	handler := LogoutHandler()

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)

	c := findAuthCookie(t, rec)
	require.NotNil(t, c, "logout must emit a Set-Cookie for the auth cookie")
	assert.Empty(t, c.Value, "logout cookie value must be empty")
	assert.True(t, c.MaxAge < 0, "logout cookie must have Max-Age<=0 to delete")
	assert.Equal(t, "/", c.Path)
	assert.True(t, c.HttpOnly)

	// The raw header must say Max-Age=0 (net/http encodes MaxAge<0 as 0).
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "Max-Age=0")
}

// TestLogoutHandler_DomainMirrors verifies the expiring cookie carries the same
// Domain as the issued cookie so browsers match it for deletion.
func TestLogoutHandler_DomainMirrors(t *testing.T) {
	t.Setenv(EnvCookieDomain, ".catchup-feed.com")
	handler := LogoutHandler()

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	c := findAuthCookie(t, rec)
	require.NotNil(t, c)
	// net/http normalizes the leading dot away (see TestTokenHandler_CookieDomain).
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "Domain=catchup-feed.com")
}
