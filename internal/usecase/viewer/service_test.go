package viewer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

/* ───────── モック実装 ───────── */

type stubViewerRepo struct {
	viewers map[int64]*entity.Viewer

	createErr error
	updateErr error
	getErr    error

	deactivatedAt map[int64]time.Time
	reactivated   []int64
	deleted       []int64
}

func newStubViewerRepo(viewers ...*entity.Viewer) *stubViewerRepo {
	s := &stubViewerRepo{
		viewers:       map[int64]*entity.Viewer{},
		deactivatedAt: map[int64]time.Time{},
	}
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
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.viewers[id], nil
}

func (s *stubViewerRepo) List(_ context.Context) ([]*entity.Viewer, error) {
	out := make([]*entity.Viewer, 0, len(s.viewers))
	for _, v := range s.viewers {
		out = append(out, v)
	}
	return out, nil
}

func (s *stubViewerRepo) Update(_ context.Context, v *entity.Viewer) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.viewers[v.ID] = v
	return nil
}

func (s *stubViewerRepo) Deactivate(_ context.Context, id int64, t time.Time) error {
	s.deactivatedAt[id] = t
	if v, ok := s.viewers[id]; ok && v.DeactivatedAt == nil {
		at := t
		v.DeactivatedAt = &at
	}
	return nil
}

func (s *stubViewerRepo) Reactivate(_ context.Context, id int64) error {
	s.reactivated = append(s.reactivated, id)
	if v, ok := s.viewers[id]; ok {
		v.DeactivatedAt = nil
	}
	return nil
}

func (s *stubViewerRepo) Delete(_ context.Context, id int64) error {
	s.deleted = append(s.deleted, id)
	delete(s.viewers, id)
	return nil
}

func (s *stubViewerRepo) GetActiveByEmail(_ context.Context, email string) (*entity.Viewer, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	for _, v := range s.viewers {
		if v.Email == email && v.DeactivatedAt == nil {
			return v, nil
		}
	}
	return nil, nil
}

func hash(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

func ptr[T any](v T) *T { return &v }

/* ───────── テストケース ───────── */

// TestDummyBcryptHash_CostMatchesDefaultCost pins the timing-equalizer
// property: the dummy hash compared for unknown emails must cost exactly
// what real viewer hashes cost (hashPassword uses bcrypt.DefaultCost). A
// mismatch re-enables account enumeration via response timing — if
// bcrypt.DefaultCost is ever bumped, regenerate dummyBcryptHash.
func TestDummyBcryptHash_CostMatchesDefaultCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyBcryptHash))
	require.NoError(t, err)
	assert.Equal(t, bcrypt.DefaultCost, cost,
		"dummyBcryptHash cost must equal bcrypt.DefaultCost (timing equalization)")
}

func TestService_Create(t *testing.T) {
	tests := []struct {
		name    string
		in      CreateInput
		repoErr error
		wantErr error
	}{
		{
			name: "valid input hashes the password",
			in:   CreateInput{Name: "Alice", Email: "alice@example.com", Password: "password-123"},
		},
		{
			name:    "name required",
			in:      CreateInput{Name: "  ", Email: "alice@example.com", Password: "password-123"},
			wantErr: ErrNameRequired,
		},
		{
			name:    "invalid email",
			in:      CreateInput{Name: "Alice", Email: "not-an-email", Password: "password-123"},
			wantErr: ErrInvalidEmail,
		},
		{
			name:    "email without dotted domain",
			in:      CreateInput{Name: "Alice", Email: "alice@localhost", Password: "password-123"},
			wantErr: ErrInvalidEmail,
		},
		{
			name:    "password too short",
			in:      CreateInput{Name: "Alice", Email: "alice@example.com", Password: "short"},
			wantErr: ErrPasswordTooShort,
		},
		{
			name: "password over bcrypt 72-byte limit rejected at validation",
			in: CreateInput{Name: "Alice", Email: "alice@example.com",
				Password: strings.Repeat("x", MaxPasswordLength+1)},
			wantErr: ErrPasswordTooLong,
		},
		{
			name:    "duplicate email maps to ErrEmailTaken",
			in:      CreateInput{Name: "Alice", Email: "alice@example.com", Password: "password-123"},
			repoErr: repository.ErrDuplicateViewerEmail,
			wantErr: ErrEmailTaken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newStubViewerRepo()
			repo.createErr = tt.repoErr
			svc := &Service{Viewers: repo}

			created, err := svc.Create(context.Background(), tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotZero(t, created.ID)
			assert.Equal(t, tt.in.Name, created.Name)
			assert.Equal(t, tt.in.Email, created.Email)
			// 平文は保存されず、bcrypt ハッシュが照合可能であること。
			assert.NotEqual(t, tt.in.Password, created.PasswordHash)
			assert.NoError(t, bcrypt.CompareHashAndPassword(
				[]byte(created.PasswordHash), []byte(tt.in.Password)))
		})
	}
}

// TestService_EmailNormalization pins the case-insensitivity contract:
// create / update store lowercase (so the UNIQUE constraint catches
// Alice@x.com vs alice@x.com), and login / re-validation match regardless
// of the case the friend types.
func TestService_EmailNormalization(t *testing.T) {
	repo := newStubViewerRepo()
	svc := &Service{Viewers: repo}

	created, err := svc.Create(context.Background(),
		CreateInput{Name: "Alice", Email: "Alice@Example.COM", Password: "password-123"})
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", created.Email, "create must store lowercase")

	// ログインは入力の大文字小文字によらず成功する。
	assert.NoError(t, svc.Authenticate(context.Background(), "ALICE@example.com", "password-123"))
	assert.NoError(t, svc.Authenticate(context.Background(), "alice@example.com", "password-123"))

	// リクエスト毎の再検証(JWT の sub が大文字混じりでも一致)。
	active, err := svc.IsActiveViewer(context.Background(), "Alice@Example.COM")
	require.NoError(t, err)
	assert.True(t, active)

	// 更新でも小文字化される。
	updated, err := svc.Update(context.Background(), created.ID,
		UpdateInput{Name: "Alice", Email: "Alice@NEW.example.com"})
	require.NoError(t, err)
	assert.Equal(t, "alice@new.example.com", updated.Email, "update must store lowercase")
}

func TestService_Update(t *testing.T) {
	existing := func() *entity.Viewer {
		return &entity.Viewer{
			ID: 1, Name: "Alice", Email: "alice@example.com",
			PasswordHash: "$2a$04$existinghashexistinghashexistingha",
		}
	}

	tests := []struct {
		name    string
		id      int64
		in      UpdateInput
		wantErr error
	}{
		{
			name: "update name and email keeps the password when nil",
			id:   1,
			in:   UpdateInput{Name: "Alicia", Email: "alicia@example.com"},
		},
		{
			name: "update with password re-hashes",
			id:   1,
			in:   UpdateInput{Name: "Alice", Email: "alice@example.com", Password: ptr("new-password-123")},
		},
		{
			name:    "short password rejected",
			id:      1,
			in:      UpdateInput{Name: "Alice", Email: "alice@example.com", Password: ptr("short")},
			wantErr: ErrPasswordTooShort,
		},
		{
			name:    "not found",
			id:      99,
			in:      UpdateInput{Name: "Alice", Email: "alice@example.com"},
			wantErr: ErrViewerNotFound,
		},
		{
			name:    "name required",
			id:      1,
			in:      UpdateInput{Name: "", Email: "alice@example.com"},
			wantErr: ErrNameRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := existing()
			repo := newStubViewerRepo(before)
			svc := &Service{Viewers: repo}

			updated, err := svc.Update(context.Background(), tt.id, tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.in.Name, updated.Name)
			assert.Equal(t, tt.in.Email, updated.Email)
			if tt.in.Password == nil {
				assert.Equal(t, existing().PasswordHash, updated.PasswordHash,
					"nil password must keep the current hash")
			} else {
				assert.NoError(t, bcrypt.CompareHashAndPassword(
					[]byte(updated.PasswordHash), []byte(*tt.in.Password)))
			}
		})
	}
}

func TestService_SetActive(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	deactivated := now.Add(-24 * time.Hour)

	tests := []struct {
		name       string
		viewer     *entity.Viewer
		active     bool
		wantActive bool
	}{
		{
			name:       "deactivate active viewer",
			viewer:     &entity.Viewer{ID: 1, Name: "A", Email: "a@example.com"},
			active:     false,
			wantActive: false,
		},
		{
			name:       "reactivate deactivated viewer",
			viewer:     &entity.Viewer{ID: 1, Name: "A", Email: "a@example.com", DeactivatedAt: &deactivated},
			active:     true,
			wantActive: true,
		},
		{
			name:       "deactivate is idempotent",
			viewer:     &entity.Viewer{ID: 1, Name: "A", Email: "a@example.com", DeactivatedAt: &deactivated},
			active:     false,
			wantActive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newStubViewerRepo(tt.viewer)
			svc := &Service{Viewers: repo, Now: func() time.Time { return now }}

			got, err := svc.SetActive(context.Background(), 1, tt.active)
			require.NoError(t, err)
			assert.Equal(t, tt.wantActive, got.IsActive())
		})
	}

	t.Run("not found", func(t *testing.T) {
		svc := &Service{Viewers: newStubViewerRepo()}
		_, err := svc.SetActive(context.Background(), 42, false)
		assert.ErrorIs(t, err, ErrViewerNotFound)
	})
}

func TestService_Delete(t *testing.T) {
	t.Run("deletes existing viewer physically", func(t *testing.T) {
		repo := newStubViewerRepo(&entity.Viewer{ID: 1, Name: "A", Email: "a@example.com"})
		svc := &Service{Viewers: repo}
		require.NoError(t, svc.Delete(context.Background(), 1))
		assert.Equal(t, []int64{1}, repo.deleted)
	})
	t.Run("not found", func(t *testing.T) {
		svc := &Service{Viewers: newStubViewerRepo()}
		assert.ErrorIs(t, svc.Delete(context.Background(), 1), ErrViewerNotFound)
	})
}

func TestService_Authenticate(t *testing.T) {
	const password = "viewer-password-1"
	deactivatedAt := time.Now().Add(-time.Hour)

	active := &entity.Viewer{ID: 1, Name: "A", Email: "a@example.com"}
	inactive := &entity.Viewer{ID: 2, Name: "B", Email: "b@example.com", DeactivatedAt: &deactivatedAt}

	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{name: "valid credentials", email: "a@example.com", password: password},
		{name: "wrong password", email: "a@example.com", password: "wrong-password", wantErr: ErrInvalidCredentials},
		{name: "unknown email", email: "ghost@example.com", password: password, wantErr: ErrInvalidCredentials},
		{name: "deactivated viewer rejected", email: "b@example.com", password: password, wantErr: ErrInvalidCredentials},
		{name: "empty credentials", email: "", password: "", wantErr: ErrInvalidCredentials},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := hash(t, password)
			active.PasswordHash, inactive.PasswordHash = h, h
			svc := &Service{Viewers: newStubViewerRepo(active, inactive)}

			err := svc.Authenticate(context.Background(), tt.email, tt.password)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("repo error is propagated, not mistaken for bad credentials", func(t *testing.T) {
		repo := newStubViewerRepo()
		repo.getErr = errors.New("db down")
		svc := &Service{Viewers: repo}
		err := svc.Authenticate(context.Background(), "a@example.com", password)
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrInvalidCredentials)
	})
}

func TestService_IsActiveViewer(t *testing.T) {
	deactivatedAt := time.Now().Add(-time.Hour)
	repo := newStubViewerRepo(
		&entity.Viewer{ID: 1, Email: "a@example.com"},
		&entity.Viewer{ID: 2, Email: "b@example.com", DeactivatedAt: &deactivatedAt},
	)
	svc := &Service{Viewers: repo}

	tests := []struct {
		name  string
		email string
		want  bool
	}{
		{"active viewer", "a@example.com", true},
		{"deactivated viewer", "b@example.com", false},
		{"deleted / unknown viewer", "ghost@example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.IsActiveViewer(context.Background(), tt.email)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
