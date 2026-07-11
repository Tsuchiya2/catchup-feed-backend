package book_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hbook "catchup-feed/internal/handler/http/book"
)

// newPrivateMux mounts the handler exactly as cmd/server does, so the
// tests exercise real routing (path values included).
func newPrivateMux(dir string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /private/books/{file}", hbook.PrivateFileHandler{Dir: dir})
	return mux
}

func TestPrivateFileHandler(t *testing.T) {
	dir := t.TempDir()
	const body = "%PDF-1.7 private book content"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "book.pdf"), []byte(body), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".upload-42"), []byte("%PDF staged"), 0o600))
	// A file outside the books dir that a traversal would try to reach.
	outside := filepath.Join(filepath.Dir(dir), "secret.pdf")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantBody   string
	}{
		{"serves the pdf", "/private/books/book.pdf", http.StatusOK, body},
		{"missing file", "/private/books/ghost.pdf", http.StatusNotFound, ""},
		{"traversal with encoded slashes", "/private/books/..%2Fsecret.pdf", http.StatusNotFound, ""},
		{"staging temp file hidden", "/private/books/.upload-42", http.StatusNotFound, ""},
		{"non-pdf name", "/private/books/notes.txt", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			rec := httptest.NewRecorder()
			newPrivateMux(dir).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.Equal(t, tt.wantBody, rec.Body.String())
				assert.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
			} else {
				assert.NotContains(t, rec.Body.String(), "secret")
			}
		})
	}
}

func TestPrivateFileHandler_Range(t *testing.T) {
	dir := t.TempDir()
	const body = "%PDF-1.7 0123456789"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "book.pdf"), []byte(body), 0o600))

	req := httptest.NewRequest(http.MethodGet, "/private/books/book.pdf", nil)
	req.Header.Set("Range", "bytes=9-13")
	rec := httptest.NewRecorder()
	newPrivateMux(dir).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Equal(t, body[9:14], rec.Body.String())
	assert.Equal(t, "bytes 9-13/19", rec.Header().Get("Content-Range"))
}

func TestPrivateFileHandler_MissingDir(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/private/books/book.pdf", nil)
	rec := httptest.NewRecorder()
	newPrivateMux(filepath.Join(t.TempDir(), "nope")).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
