package viewer

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// maxEmailLength is the RFC 5321 ceiling for a complete address.
const maxEmailLength = 254

// MinPasswordLength is the minimum viewer password length. The admin sets
// the password, so this is a floor against typos rather than a strength
// policy (単一ユーザー右サイズ: 複雑なパスワードポリシーは持ち込まない).
const MinPasswordLength = 8

// MaxPasswordLength is bcrypt's hard input limit (72 bytes). Longer
// passwords make bcrypt.GenerateFromPassword fail with ErrPasswordTooLong,
// so they are rejected at validation time (400) instead of surfacing as a
// 500 from the hashing step.
const MaxPasswordLength = 72

// dummyBcryptHash is compared when the email does not belong to an active
// viewer, so login timing does not reveal whether the account exists. Same
// pattern as the admin provider (internal/handler/http/auth/provider.go).
//
// Its cost MUST equal bcrypt.DefaultCost — the cost real viewer hashes are
// generated with (hashPassword) — or the timing of "unknown email" vs
// "known email, wrong password" diverges and re-enables account
// enumeration. TestDummyBcryptHash_CostMatchesDefaultCost pins this; if
// bcrypt.DefaultCost ever changes, regenerate this constant.
const dummyBcryptHash = "$2a$10$2liJaVtwjkEHDTCuT02M2.Fk2DMXjYqQhpWzlKwPwD.B5SfFQ0fpm"

// CreateInput carries the fields for POST /viewers. All fields required.
type CreateInput struct {
	Name     string
	Email    string
	Password string
}

// UpdateInput carries the fields for PUT /viewers/{id}. Name / Email are
// full replacements; Password is optional (nil keeps the current hash).
type UpdateInput struct {
	Name     string
	Email    string
	Password *string
}

// Service provides the viewer account use cases (D-27): admin-managed CRUD
// plus login (Authenticate) and the per-request activity re-check
// (IsActiveViewer) the auth middleware performs so deactivation takes
// effect immediately, without waiting for JWT expiry.
type Service struct {
	Viewers repository.ViewerRepository
	// Now returns the current time; nil means time.Now. Injected for
	// deterministic tests of deactivation timestamps.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// validateEmail rejects anything that is not a single bare address
// (name@domain, no display name, no groups) with a dotted domain. Same
// rules as subscriber emails (usecase/subscriber): the address is the login
// identifier here.
func validateEmail(email string) error {
	if email == "" || len(email) > maxEmailLength || strings.TrimSpace(email) != email {
		return ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return ErrInvalidEmail
	}
	at := strings.LastIndex(addr.Address, "@")
	if at < 0 || !strings.Contains(addr.Address[at+1:], ".") {
		return ErrInvalidEmail
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}

// normalizeEmail lowercases the address so that lookups, the UNIQUE
// constraint and login all treat Alice@example.com and alice@example.com as
// the same account. Normalization happens at every use case boundary
// (create / update / login / per-request re-validation), so the DB only
// ever stores lowercase.
func normalizeEmail(email string) string {
	return strings.ToLower(email)
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// List returns all viewers, active and deactivated, oldest first.
func (s *Service) List(ctx context.Context) ([]*entity.Viewer, error) {
	viewers, err := s.Viewers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list viewers: %w", err)
	}
	return viewers, nil
}

// Get returns the viewer or ErrViewerNotFound.
func (s *Service) Get(ctx context.Context, id int64) (*entity.Viewer, error) {
	viewer, err := s.Viewers.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get viewer: %w", err)
	}
	if viewer == nil {
		return nil, ErrViewerNotFound
	}
	return viewer, nil
}

// Create registers a new viewer account. The password is bcrypt-hashed
// before it reaches the repository; the plaintext is never stored.
func (s *Service) Create(ctx context.Context, in CreateInput) (*entity.Viewer, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrNameRequired
	}
	in.Email = normalizeEmail(in.Email)
	if err := validateEmail(in.Email); err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	viewer := &entity.Viewer{Name: in.Name, Email: in.Email, PasswordHash: hash}
	if err := s.Viewers.Create(ctx, viewer); err != nil {
		if errors.Is(err, repository.ErrDuplicateViewerEmail) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create viewer: %w", err)
	}
	return viewer, nil
}

// Update rewrites name / email and, when Password is non-nil, re-hashes and
// replaces the password. Returns the updated viewer.
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (*entity.Viewer, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrNameRequired
	}
	in.Email = normalizeEmail(in.Email)
	if err := validateEmail(in.Email); err != nil {
		return nil, err
	}
	if in.Password != nil {
		if err := validatePassword(*in.Password); err != nil {
			return nil, err
		}
	}
	viewer, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	viewer.Name = in.Name
	viewer.Email = in.Email
	if in.Password != nil {
		hash, err := hashPassword(*in.Password)
		if err != nil {
			return nil, err
		}
		viewer.PasswordHash = hash
	}
	if err := s.Viewers.Update(ctx, viewer); err != nil {
		if errors.Is(err, repository.ErrDuplicateViewerEmail) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("update viewer: %w", err)
	}
	return viewer, nil
}

// SetActive toggles the viewer's logical activation (PUT
// /viewers/{id}/active). Deactivation blocks login immediately and — via
// the middleware's per-request IsActiveViewer check — cuts off existing
// JWTs on their next request (D-27 (4)). Idempotent in both directions.
// Returns the resulting viewer.
func (s *Service) SetActive(ctx context.Context, id int64, active bool) (*entity.Viewer, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	if active {
		if err := s.Viewers.Reactivate(ctx, id); err != nil {
			return nil, fmt.Errorf("reactivate viewer: %w", err)
		}
	} else {
		if err := s.Viewers.Deactivate(ctx, id, s.now()); err != nil {
			return nil, fmt.Errorf("deactivate viewer: %w", err)
		}
	}
	return s.Get(ctx, id)
}

// Delete removes the viewer physically (D-27 (4): viewers は物理削除あり —
// subscribers と違い、集計のために行を残す理由がない).
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.Viewers.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete viewer: %w", err)
	}
	return nil
}

// Authenticate validates a viewer login (POST /auth/token fallback after
// the admin check). Deactivated or unknown viewers fail with the same
// generic ErrInvalidCredentials; a bcrypt comparison runs in every path so
// timing does not reveal whether the account exists.
func (s *Service) Authenticate(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return ErrInvalidCredentials
	}
	viewer, err := s.Viewers.GetActiveByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("authenticate viewer: %w", err)
	}
	hash := dummyBcryptHash
	if viewer != nil {
		hash = viewer.PasswordHash
	}
	passErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if viewer == nil || passErr != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// IsActiveViewer reports whether email belongs to an existing, active
// viewer. The auth middleware calls this on every viewer request so
// deactivation takes effect immediately (D-27 (4)).
func (s *Service) IsActiveViewer(ctx context.Context, email string) (bool, error) {
	viewer, err := s.Viewers.GetActiveByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return false, fmt.Errorf("check viewer activity: %w", err)
	}
	return viewer != nil, nil
}
