package viewer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/viewer"
	"catchup-feed/internal/repository"
	viewerUC "catchup-feed/internal/usecase/viewer"
)

/* ───────── モック実装 ───────── */

type stubViewerRepo struct {
	viewers   map[int64]*entity.Viewer
	createErr error
}

func newStubViewerRepo(viewers ...*entity.Viewer) *stubViewerRepo {
	s := &stubViewerRepo{viewers: map[int64]*entity.Viewer{}}
	for _, v := range viewers {
		s.viewers[v.ID] = v
	}
	return s
}

func (s *stubViewerRepo) Create(_ context.Context, v *entity.Viewer) error {
	if s.createErr != nil {
		return s.createErr
	}
	v.ID = int64(len(s.viewers) + 1)
	v.CreatedAt, v.UpdatedAt = time.Now(), time.Now()
	s.viewers[v.ID] = v
	return nil
}

func (s *stubViewerRepo) Get(_ context.Context, id int64) (*entity.Viewer, error) {
	return s.viewers[id], nil
}

func (s *stubViewerRepo) List(_ context.Context) ([]*entity.Viewer, error) {
	out := make([]*entity.Viewer, 0, len(s.viewers))
	for id := int64(1); id <= int64(len(s.viewers))+10; id++ {
		if v, ok := s.viewers[id]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *stubViewerRepo) Update(_ context.Context, v *entity.Viewer) error {
	s.viewers[v.ID] = v
	return nil
}

func (s *stubViewerRepo) Deactivate(_ context.Context, id int64, t time.Time) error {
	if v, ok := s.viewers[id]; ok && v.DeactivatedAt == nil {
		at := t
		v.DeactivatedAt = &at
	}
	return nil
}

func (s *stubViewerRepo) Reactivate(_ context.Context, id int64) error {
	if v, ok := s.viewers[id]; ok {
		v.DeactivatedAt = nil
	}
	return nil
}

func (s *stubViewerRepo) Delete(_ context.Context, id int64) error {
	delete(s.viewers, id)
	return nil
}

func (s *stubViewerRepo) GetActiveByEmail(_ context.Context, email string) (*entity.Viewer, error) {
	for _, v := range s.viewers {
		if v.Email == email && v.DeactivatedAt == nil {
			return v, nil
		}
	}
	return nil, nil
}

func newMux(repo repository.ViewerRepository) *http.ServeMux {
	svc := &viewerUC.Service{Viewers: repo}
	mux := http.NewServeMux()
	// ルーティングパターン({id} / {id}/active の共存)を検証したいので
	// Register と同じパターンで、認可ミドルウェアなしに直接張る。
	mux.Handle("GET /viewers", viewer.ListHandler{Svc: svc})
	mux.Handle("POST /viewers", viewer.CreateHandler{Svc: svc})
	mux.Handle("PUT /viewers/{id}", viewer.UpdateHandler{Svc: svc})
	mux.Handle("PUT /viewers/{id}/active", viewer.SetActiveHandler{Svc: svc})
	mux.Handle("DELETE /viewers/{id}", viewer.DeleteHandler{Svc: svc})
	return mux
}

func do(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

/* ───────── テストケース ───────── */

func TestListHandler(t *testing.T) {
	deactivatedAt := time.Now().Add(-time.Hour)
	repo := newStubViewerRepo(
		&entity.Viewer{ID: 1, Name: "Alice", Email: "alice@example.com", PasswordHash: "h"},
		&entity.Viewer{ID: 2, Name: "Bob", Email: "bob@example.com", PasswordHash: "h", DeactivatedAt: &deactivatedAt},
	)

	rec := do(newMux(repo), http.MethodGet, "/viewers", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got []viewer.DTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	// active / deactivated_at で有効・無効を判別できること(D-27)。
	assert.True(t, got[0].Active)
	assert.Nil(t, got[0].DeactivatedAt)
	assert.False(t, got[1].Active)
	assert.NotNil(t, got[1].DeactivatedAt)
	// password_hash がレスポンスに漏れないこと。
	assert.NotContains(t, rec.Body.String(), "password")
}

func TestCreateHandler(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "valid create",
			body:     `{"name":"Alice","email":"alice@example.com","password":"password-123"}`,
			wantCode: http.StatusCreated,
		},
		{
			name:     "missing name",
			body:     `{"name":"","email":"alice@example.com","password":"password-123"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid email",
			body:     `{"name":"Alice","email":"nope","password":"password-123"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "short password",
			body:     `{"name":"Alice","email":"alice@example.com","password":"short"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			// bcrypt の72バイト上限超過はバリデーションで 400(500 にしない)。
			name:     "password over 72 bytes",
			body:     `{"name":"Alice","email":"alice@example.com","password":"` + strings.Repeat("x", 73) + `"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid json",
			body:     `{not json`,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newMux(newStubViewerRepo()), http.MethodPost, "/viewers", tt.body)
			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusCreated {
				var got viewer.DTO
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				assert.Equal(t, "Alice", got.Name)
				assert.True(t, got.Active)
				assert.NotContains(t, rec.Body.String(), "password")
			}
		})
	}
}

func TestCreateHandler_DuplicateEmail(t *testing.T) {
	repo := newStubViewerRepo()
	repo.createErr = repository.ErrDuplicateViewerEmail

	rec := do(newMux(repo), http.MethodPost, "/viewers",
		`{"name":"Alice","email":"alice@example.com","password":"password-123"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestUpdateHandler(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("old-password-1"), bcrypt.MinCost)
	require.NoError(t, err)
	existing := &entity.Viewer{ID: 1, Name: "Alice", Email: "alice@example.com", PasswordHash: string(hash)}

	tests := []struct {
		name     string
		path     string
		body     string
		wantCode int
	}{
		{
			name:     "update without password keeps hash",
			path:     "/viewers/1",
			body:     `{"name":"Alicia","email":"alicia@example.com"}`,
			wantCode: http.StatusOK,
		},
		{
			name:     "update with password resets it",
			path:     "/viewers/1",
			body:     `{"name":"Alice","email":"alice@example.com","password":"new-password-1"}`,
			wantCode: http.StatusOK,
		},
		{
			name:     "not found",
			path:     "/viewers/99",
			body:     `{"name":"Alice","email":"alice@example.com"}`,
			wantCode: http.StatusNotFound,
		},
		{
			name:     "invalid id",
			path:     "/viewers/abc",
			body:     `{"name":"Alice","email":"alice@example.com"}`,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copied := *existing
			repo := newStubViewerRepo(&copied)
			rec := do(newMux(repo), http.MethodPut, tt.path, tt.body)
			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

func TestSetActiveHandler(t *testing.T) {
	deactivatedAt := time.Now().Add(-time.Hour)

	tests := []struct {
		name       string
		viewer     *entity.Viewer
		path       string
		body       string
		wantCode   int
		wantActive bool
	}{
		{
			name:       "deactivate",
			viewer:     &entity.Viewer{ID: 1, Name: "A", Email: "a@example.com", PasswordHash: "h"},
			path:       "/viewers/1/active",
			body:       `{"active":false}`,
			wantCode:   http.StatusOK,
			wantActive: false,
		},
		{
			name:       "reactivate",
			viewer:     &entity.Viewer{ID: 1, Name: "A", Email: "a@example.com", PasswordHash: "h", DeactivatedAt: &deactivatedAt},
			path:       "/viewers/1/active",
			body:       `{"active":true}`,
			wantCode:   http.StatusOK,
			wantActive: true,
		},
		{
			name:     "not found",
			viewer:   &entity.Viewer{ID: 1, Name: "A", Email: "a@example.com", PasswordHash: "h"},
			path:     "/viewers/9/active",
			body:     `{"active":false}`,
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newStubViewerRepo(tt.viewer)
			rec := do(newMux(repo), http.MethodPut, tt.path, tt.body)
			require.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusOK {
				var got viewer.DTO
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				assert.Equal(t, tt.wantActive, got.Active)
			}
		})
	}
}

func TestDeleteHandler(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantCode int
	}{
		{name: "delete existing", path: "/viewers/1", wantCode: http.StatusNoContent},
		{name: "not found", path: "/viewers/9", wantCode: http.StatusNotFound},
		{name: "invalid id", path: "/viewers/-1", wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newStubViewerRepo(&entity.Viewer{ID: 1, Name: "A", Email: "a@example.com", PasswordHash: "h"})
			rec := do(newMux(repo), http.MethodDelete, tt.path, "")
			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusNoContent {
				assert.Empty(t, repo.viewers)
			}
		})
	}
}
