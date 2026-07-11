package book_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	hbook "catchup-feed/internal/handler/http/book"
	"catchup-feed/internal/repository"
	bookUC "catchup-feed/internal/usecase/book"
)

/* ───────────────────────────── fakes ───────────────────────────── */

type fakeRepo struct {
	books  []repository.BookRecord
	states map[string]repository.IngestJobState

	pending       map[string]bool
	updatedTitles []string
	cancelReturn  int64
	deleteReturn  bool
	err           error
}

func (f *fakeRepo) ListBooks(context.Context) ([]repository.BookRecord, error) {
	return f.books, f.err
}

func (f *fakeRepo) LatestIngestStates(context.Context) (map[string]repository.IngestJobState, error) {
	return f.states, f.err
}

func (f *fakeRepo) UpdatePendingIngestTitle(_ context.Context, filePath, title string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	if !f.pending[filePath] {
		return 0, nil
	}
	f.updatedTitles = append(f.updatedTitles, title)
	return 1, nil
}

func (f *fakeRepo) CancelPendingIngest(context.Context, string) (int64, error) {
	return f.cancelReturn, f.err
}

func (f *fakeRepo) DeleteBookByFilePath(context.Context, string) (bool, error) {
	return f.deleteReturn, f.err
}

type fakeJobs struct {
	kinds    []string
	payloads []json.RawMessage
}

func (f *fakeJobs) Enqueue(_ context.Context, kind string, payload json.RawMessage, _ time.Time) (int64, error) {
	f.kinds = append(f.kinds, kind)
	f.payloads = append(f.payloads, payload)
	return int64(len(f.kinds)), nil
}

func (f *fakeJobs) ClaimNext(context.Context, ...string) (*entity.Job, error) { panic("not used") }
func (f *fakeJobs) MarkDone(context.Context, int64) error                     { panic("not used") }
func (f *fakeJobs) MarkFailed(context.Context, int64, string, *time.Time) error {
	panic("not used")
}
func (f *fakeJobs) RequeueRunning(context.Context, ...string) (int64, error) { panic("not used") }

func newService(t *testing.T) (*bookUC.Service, *fakeRepo, *fakeJobs) {
	t.Helper()
	repo := &fakeRepo{pending: map[string]bool{}}
	jobs := &fakeJobs{}
	return &bookUC.Service{Repo: repo, Jobs: jobs, Dir: t.TempDir()}, repo, jobs
}

// multipartBody builds a multipart/form-data body from ordered field
// name/value pairs; the field named "file" becomes a file part with the
// given filename.
type field struct {
	name     string
	filename string
	value    string
}

func multipartBody(t *testing.T, fields []field) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range fields {
		if f.filename != "" {
			part, err := w.CreateFormFile(f.name, f.filename)
			require.NoError(t, err)
			_, err = io.WriteString(part, f.value)
			require.NoError(t, err)
		} else {
			require.NoError(t, w.WriteField(f.name, f.value))
		}
	}
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

/* ─────────────────────────── POST /books ─────────────────────────── */

func TestUploadHandler(t *testing.T) {
	const pdf = "%PDF-1.7 content"

	tests := []struct {
		name       string
		fields     []field
		wantStatus int
		wantTitle  string // checked when 201
	}{
		{
			name: "file then title",
			fields: []field{
				{name: "file", filename: "book.pdf", value: pdf},
				{name: "title", value: "実用 Go 言語"},
			},
			wantStatus: http.StatusCreated,
			wantTitle:  "実用 Go 言語",
		},
		{
			name: "title then file",
			fields: []field{
				{name: "title", value: "先行タイトル"},
				{name: "file", filename: "book.pdf", value: pdf},
			},
			wantStatus: http.StatusCreated,
			wantTitle:  "先行タイトル",
		},
		{
			name: "title omitted falls back to filename",
			fields: []field{
				{name: "file", filename: "go-book.pdf", value: pdf},
			},
			wantStatus: http.StatusCreated,
			wantTitle:  "go-book",
		},
		{
			name: "unknown fields ignored",
			fields: []field{
				{name: "junk", value: "x"},
				{name: "file", filename: "book.pdf", value: pdf},
			},
			wantStatus: http.StatusCreated,
			wantTitle:  "book",
		},
		{
			name:       "missing file field",
			fields:     []field{{name: "title", value: "t"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate file field",
			fields: []field{
				{name: "file", filename: "a.pdf", value: pdf},
				{name: "file", filename: "b.pdf", value: pdf},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "not a pdf by magic",
			fields: []field{
				{name: "file", filename: "book.pdf", value: "MZ executable"},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "not a pdf by extension",
			fields: []field{
				{name: "file", filename: "book.epub", value: pdf},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "title too long",
			fields: []field{
				{name: "title", value: strings.Repeat("あ", 2048)},
				{name: "file", filename: "book.pdf", value: pdf},
			},
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, jobs := newService(t)
			body, contentType := multipartBody(t, tt.fields)

			req := httptest.NewRequest(http.MethodPost, "/books", body)
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()
			hbook.UploadHandler{Svc: svc}.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantStatus != http.StatusCreated {
				assert.Empty(t, jobs.kinds, "rejected upload must not enqueue")
				// A rejected upload must leave no files behind.
				entries, err := os.ReadDir(svc.Dir)
				require.NoError(t, err)
				assert.Empty(t, entries)
				return
			}

			var dto hbook.DTO
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
			assert.Equal(t, tt.wantTitle, dto.Title)
			assert.Equal(t, bookUC.StatusPending, dto.Status)
			assert.Equal(t, filepath.Join(svc.Dir, dto.Filename), dto.FilePath)
			assert.True(t, dto.Deletable)
			require.Len(t, jobs.kinds, 1)
			assert.Equal(t, entity.JobKindBookIngest, jobs.kinds[0])

			var payload entity.BookIngestPayload
			require.NoError(t, json.Unmarshal(jobs.payloads[0], &payload))
			assert.Equal(t, filepath.Join(svc.Dir, dto.Filename), payload.FilePath)
			assert.Equal(t, tt.wantTitle, payload.Title)
		})
	}
}

func TestUploadHandler_NotMultipart(t *testing.T) {
	svc, _, _ := newService(t)
	req := httptest.NewRequest(http.MethodPost, "/books", strings.NewReader(`{"file":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	hbook.UploadHandler{Svc: svc}.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadHandler_TooLarge(t *testing.T) {
	svc, _, jobs := newService(t)
	svc.MaxUploadBytes = 64 // tiny ceiling for the test

	body, contentType := multipartBody(t, []field{
		{name: "file", filename: "book.pdf", value: "%PDF" + strings.Repeat("x", 128)},
	})
	req := httptest.NewRequest(http.MethodPost, "/books", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	hbook.UploadHandler{Svc: svc}.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Empty(t, jobs.kinds)
}

func TestUploadHandler_PendingJobNotDuplicated(t *testing.T) {
	svc, repo, jobs := newService(t)
	repo.pending[filepath.Join(svc.Dir, "book.pdf")] = true

	body, contentType := multipartBody(t, []field{
		{name: "title", value: "改訂タイトル"},
		{name: "file", filename: "book.pdf", value: "%PDF body"},
	})
	req := httptest.NewRequest(http.MethodPost, "/books", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	hbook.UploadHandler{Svc: svc}.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Empty(t, jobs.kinds, "existing pending job suppresses re-enqueue")
	// The deduped pending job gets the fresh title instead of a stale one.
	assert.Equal(t, []string{"改訂タイトル"}, repo.updatedTitles)
	// The file itself is still replaced.
	_, err := os.Stat(filepath.Join(svc.Dir, "book.pdf"))
	assert.NoError(t, err)
}

/* ─────────────────────────── GET /books ─────────────────────────── */

func TestListHandler(t *testing.T) {
	svc, repo, _ := newService(t)
	pdfPath := filepath.Join(svc.Dir, "on-disk.pdf")
	require.NoError(t, os.WriteFile(pdfPath, []byte("%PDF body"), 0o600))
	repo.states = map[string]repository.IngestJobState{
		pdfPath: {Status: entity.JobStatusRunning, Title: "処理中の本"},
	}
	repo.books = []repository.BookRecord{
		{ID: 3, Title: "CLI の本", FilePath: "/mac/cli.pdf", ImportedAt: time.Now(), ChunkCount: 12},
	}

	req := httptest.NewRequest(http.MethodGet, "/books", nil)
	rec := httptest.NewRecorder()
	hbook.ListHandler{Svc: svc}.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var dtos []hbook.DTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	require.Len(t, dtos, 2)

	byName := map[string]hbook.DTO{}
	for _, d := range dtos {
		byName[d.Filename] = d
	}
	onDisk := byName["on-disk.pdf"]
	assert.Equal(t, bookUC.StatusProcessing, onDisk.Status)
	assert.Equal(t, "処理中の本", onDisk.Title)
	require.NotNil(t, onDisk.SizeBytes)
	assert.Equal(t, int64(len("%PDF body")), *onDisk.SizeBytes)
	assert.Equal(t, pdfPath, onDisk.FilePath)
	assert.True(t, onDisk.Deletable, "Pi uploads are deletable")

	cli := byName["cli.pdf"]
	assert.Equal(t, bookUC.StatusDone, cli.Status)
	require.NotNil(t, cli.BookID)
	assert.Equal(t, int64(3), *cli.BookID)
	require.NotNil(t, cli.ChunkCount)
	assert.Equal(t, 12, *cli.ChunkCount)
	assert.Nil(t, cli.SizeBytes)
	assert.Equal(t, "/mac/cli.pdf", cli.FilePath)
	assert.False(t, cli.Deletable, "CLI books (Mac path) are not deletable via the API")
}

func TestListHandler_RepoError(t *testing.T) {
	svc, repo, _ := newService(t)
	repo.err = errors.New("db down")

	rec := httptest.NewRecorder()
	hbook.ListHandler{Svc: svc}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/books", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "db down", "internal detail must not leak")
}

/* ──────────────────────── DELETE /books/{filename} ──────────────────────── */

func TestDeleteHandler(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		fileOnDisk   bool
		deleteReturn bool
		cancelReturn int64
		wantStatus   int
	}{
		{"deletes existing file", "book.pdf", true, false, 0, http.StatusNoContent},
		{"deletes book row without file", "book.pdf", false, true, 0, http.StatusNoContent},
		{"cancels pending job without file or row", "book.pdf", false, false, 1, http.StatusNoContent},
		{"nothing to delete", "ghost.pdf", false, false, 0, http.StatusNotFound},
		{"traversal filename (encoded slash)", "../book.pdf", false, false, 0, http.StatusBadRequest},
		{"hidden staging file", ".upload-42", false, false, 0, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newService(t)
			repo.deleteReturn = tt.deleteReturn
			repo.cancelReturn = tt.cancelReturn
			if tt.fileOnDisk {
				require.NoError(t, os.WriteFile(filepath.Join(svc.Dir, tt.filename), []byte("%PDF"), 0o600))
			}

			req := httptest.NewRequest(http.MethodDelete, "/books/x", nil)
			req.SetPathValue("filename", tt.filename)
			rec := httptest.NewRecorder()
			hbook.DeleteHandler{Svc: svc}.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.fileOnDisk && tt.wantStatus == http.StatusNoContent {
				_, err := os.Stat(filepath.Join(svc.Dir, tt.filename))
				assert.True(t, os.IsNotExist(err))
			}
		})
	}
}
