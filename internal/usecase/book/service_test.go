package book_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	bookUC "catchup-feed/internal/usecase/book"
)

/* ───────────────────────────── fakes ───────────────────────────── */

type titleUpdate struct {
	filePath string
	title    string
}

type fakeRepo struct {
	books  []repository.BookRecord
	states map[string]repository.IngestJobState

	pending      map[string]bool
	titleUpdates []titleUpdate

	cancelledPaths []string
	cancelReturn   int64

	deletedPaths []string
	deleteReturn bool

	err error
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
	f.titleUpdates = append(f.titleUpdates, titleUpdate{filePath: filePath, title: title})
	return 1, nil
}

func (f *fakeRepo) CancelPendingIngest(_ context.Context, filePath string) (int64, error) {
	f.cancelledPaths = append(f.cancelledPaths, filePath)
	return f.cancelReturn, f.err
}

func (f *fakeRepo) DeleteBookByFilePath(_ context.Context, filePath string) (bool, error) {
	f.deletedPaths = append(f.deletedPaths, filePath)
	return f.deleteReturn, f.err
}

type enqueued struct {
	kind    string
	payload json.RawMessage
}

type fakeJobs struct {
	enqueued []enqueued
	err      error
}

func (f *fakeJobs) Enqueue(_ context.Context, kind string, payload json.RawMessage, _ time.Time) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.enqueued = append(f.enqueued, enqueued{kind: kind, payload: payload})
	return int64(len(f.enqueued)), nil
}

func (f *fakeJobs) ClaimNext(context.Context, ...string) (*entity.Job, error) {
	panic("not used")
}
func (f *fakeJobs) MarkDone(context.Context, int64) error { panic("not used") }
func (f *fakeJobs) MarkFailed(context.Context, int64, string, *time.Time) error {
	panic("not used")
}
func (f *fakeJobs) RequeueRunning(context.Context, ...string) (int64, error) { panic("not used") }

func newService(t *testing.T, repo *fakeRepo, jobs *fakeJobs) *bookUC.Service {
	t.Helper()
	return &bookUC.Service{Repo: repo, Jobs: jobs, Dir: t.TempDir()}
}

/* ─────────────────────── filename validation ─────────────────────── */

func TestValidateFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantErr  error
	}{
		{"plain pdf", "book.pdf", nil},
		{"japanese name", "実用Go言語.pdf", nil},
		{"uppercase extension", "BOOK.PDF", nil},
		{"empty", "", bookUC.ErrInvalidFilename},
		{"dot", ".", bookUC.ErrInvalidFilename},
		{"dotdot", "..", bookUC.ErrInvalidFilename},
		{"hidden file", ".hidden.pdf", bookUC.ErrInvalidFilename},
		{"staging temp file", ".upload-12345", bookUC.ErrInvalidFilename},
		{"slash traversal", "../etc/passwd.pdf", bookUC.ErrInvalidFilename},
		{"embedded slash (encoded %2F)", "a/b.pdf", bookUC.ErrInvalidFilename},
		{"backslash", `a\b.pdf`, bookUC.ErrInvalidFilename},
		{"nul byte", "a\x00b.pdf", bookUC.ErrInvalidFilename},
		{"control char", "a\nb.pdf", bookUC.ErrInvalidFilename},
		{"invalid utf8", "\xff\xfe.pdf", bookUC.ErrInvalidFilename},
		{"too long", strings.Repeat("a", 252) + ".pdf", bookUC.ErrInvalidFilename},
		{"not a pdf", "book.epub", bookUC.ErrNotPDF},
		{"no extension", "book", bookUC.ErrNotPDF},
		{"extension only", ".pdf", bookUC.ErrInvalidFilename},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bookUC.ValidateFilename(tt.filename)
			if tt.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeUploadFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
		wantErr  error
	}{
		{"plain", "book.pdf", "book.pdf", nil},
		{"client unix path stripped", "/home/user/book.pdf", "book.pdf", nil},
		{"client windows path stripped", `C:\Users\user\book.pdf`, "book.pdf", nil},
		{"traversal collapses to base", "../../book.pdf", "book.pdf", nil},
		{"whitespace trimmed", "  book.pdf  ", "book.pdf", nil},
		{"empty", "", "", bookUC.ErrInvalidFilename},
		{"directory only", "a/b/", "", bookUC.ErrInvalidFilename},
		{"not pdf", "notes.txt", "", bookUC.ErrNotPDF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bookUC.SanitizeUploadFilename(tt.filename)
			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

/* ─────────────────────────── Stage ─────────────────────────── */

func TestService_Stage(t *testing.T) {
	pdf := "%PDF-1.7 tiny body"

	tests := []struct {
		name     string
		filename string
		body     string
		maxBytes int64
		wantErr  error
	}{
		{"valid pdf", "book.pdf", pdf, 0, nil},
		{"exactly at limit", "book.pdf", "%PDF" + strings.Repeat("x", 6), 10, nil},
		{"over the limit", "book.pdf", "%PDF" + strings.Repeat("x", 7), 10, bookUC.ErrTooLarge},
		{"bad magic", "book.pdf", "MZPE not a pdf", 0, bookUC.ErrNotPDF},
		{"empty body", "book.pdf", "", 0, bookUC.ErrNotPDF},
		{"truncated magic", "book.pdf", "%P", 0, bookUC.ErrNotPDF},
		{"wrong extension", "book.txt", pdf, 0, bookUC.ErrNotPDF},
		{"invalid name", "../book.pdf", pdf, 0, nil}, // Base() strips the traversal
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newService(t, &fakeRepo{}, &fakeJobs{})
			svc.MaxUploadBytes = tt.maxBytes

			staged, err := svc.Stage(tt.filename, strings.NewReader(tt.body))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				// No stray files may survive a rejected stage.
				assertDirEmpty(t, svc.Dir)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int64(len(tt.body)), staged.Size)

			staged.Discard()
			assertDirEmpty(t, svc.Dir)
		})
	}
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

/* ─────────────────────────── Commit ─────────────────────────── */

func TestService_Commit(t *testing.T) {
	const body = "%PDF-1.7 body"

	tests := []struct {
		name        string
		title       string
		pending     bool
		wantTitle   string
		wantEnqueue bool
	}{
		{"enqueues with explicit title", "実用 Go 言語", false, "実用 Go 言語", true},
		{"title falls back to filename stem", "", false, "book", true},
		{"pending job suppresses re-enqueue", "新しいタイトル", true, "新しいタイトル", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{pending: map[string]bool{}}
			jobs := &fakeJobs{}
			svc := newService(t, repo, jobs)
			canonical := filepath.Join(svc.Dir, "book.pdf")
			repo.pending[canonical] = tt.pending

			staged, err := svc.Stage("book.pdf", strings.NewReader(body))
			require.NoError(t, err)

			entry, err := svc.Commit(context.Background(), staged, tt.title)
			require.NoError(t, err)
			assert.Equal(t, "book.pdf", entry.Filename)
			assert.Equal(t, tt.wantTitle, entry.Title)
			assert.Equal(t, bookUC.StatusPending, entry.Status)
			assert.Equal(t, canonical, entry.FilePath)
			assert.True(t, entry.Deletable, "Pi uploads are always deletable")

			// The PDF sits at the canonical path with the full content.
			got, err := os.ReadFile(canonical)
			require.NoError(t, err)
			assert.Equal(t, body, string(got))

			if !tt.wantEnqueue {
				assert.Empty(t, jobs.enqueued)
				// The deduped pending job must carry the fresh title, not
				// the one from the original enqueue.
				assert.Equal(t, []titleUpdate{{filePath: canonical, title: tt.wantTitle}}, repo.titleUpdates)
				return
			}
			require.Len(t, jobs.enqueued, 1)
			assert.Equal(t, entity.JobKindBookIngest, jobs.enqueued[0].kind)
			var payload entity.BookIngestPayload
			require.NoError(t, json.Unmarshal(jobs.enqueued[0].payload, &payload))
			assert.Equal(t, canonical, payload.FilePath, "payload must carry the canonical absolute path")
			assert.Equal(t, tt.wantTitle, payload.Title)

			// Discard after a successful commit must not remove the PDF.
			staged.Discard()
			_, err = os.Stat(canonical)
			assert.NoError(t, err)
		})
	}
}

func TestService_Commit_ReuploadOverwrites(t *testing.T) {
	repo := &fakeRepo{pending: map[string]bool{}}
	jobs := &fakeJobs{}
	svc := newService(t, repo, jobs)

	for i, body := range []string{"%PDF first", "%PDF second (replaced)"} {
		staged, err := svc.Stage("book.pdf", strings.NewReader(body))
		require.NoError(t, err)
		_, err = svc.Commit(context.Background(), staged, "")
		require.NoError(t, err, "commit %d", i)
	}

	got, err := os.ReadFile(filepath.Join(svc.Dir, "book.pdf"))
	require.NoError(t, err)
	assert.Equal(t, "%PDF second (replaced)", string(got))
	// Both commits enqueued (no pending job existed at either point:
	// the fake reports none, mirroring a done/failed previous ingest).
	assert.Len(t, jobs.enqueued, 2)
	assertDirHasOnly(t, svc.Dir, "book.pdf")
}

func assertDirHasOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	assert.ElementsMatch(t, names, got)
}

func TestService_Commit_EnqueueFailureKeepsFile(t *testing.T) {
	repo := &fakeRepo{pending: map[string]bool{}}
	jobs := &fakeJobs{err: errors.New("db down")}
	svc := newService(t, repo, jobs)

	staged, err := svc.Stage("book.pdf", strings.NewReader("%PDF body"))
	require.NoError(t, err)
	_, err = svc.Commit(context.Background(), staged, "")
	require.Error(t, err)

	// The PDF stays: re-uploading the same file retries the enqueue.
	_, statErr := os.Stat(filepath.Join(svc.Dir, "book.pdf"))
	assert.NoError(t, statErr)
}

/* ─────────────────────────── List ─────────────────────────── */

func TestService_List(t *testing.T) {
	ctx := context.Background()

	t.Run("merges disk, books and jobs", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := newService(t, repo, &fakeJobs{})

		write := func(name, body string) string {
			t.Helper()
			path := filepath.Join(svc.Dir, name)
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
			return path
		}
		pendingPath := write("a-pending.pdf", "%PDF a")
		runningPath := write("b-running.pdf", "%PDF b")
		donePath := write("c-done.pdf", "%PDF c")
		failedPath := write("d-failed.pdf", "%PDF d")
		write(".upload-123", "%PDF staging leftover") // must be hidden
		require.NoError(t, os.Mkdir(filepath.Join(svc.Dir, "subdir"), 0o750))

		repo.states = map[string]repository.IngestJobState{
			pendingPath: {Status: entity.JobStatusPending, Title: "A(job)"},
			runningPath: {Status: entity.JobStatusRunning, Title: "B"},
			donePath:    {Status: entity.JobStatusDone, Title: "C"},
			failedPath:  {Status: entity.JobStatusFailed, Title: "D"},
		}
		repo.books = []repository.BookRecord{
			{ID: 7, Title: "C 完全版", FilePath: donePath, ImportedAt: time.Now(), ChunkCount: 42},
			{ID: 8, Title: "CLI 取り込みの本", FilePath: "/Users/mac/books/cli.pdf", ChunkCount: 9},
		}

		entries, err := svc.List(ctx)
		require.NoError(t, err)

		byName := map[string]bookUC.Entry{}
		for _, e := range entries {
			byName[e.Filename] = e
		}
		require.Len(t, byName, 5, "4 disk PDFs + 1 CLI book; temp file and subdir skipped")

		assert.Equal(t, bookUC.StatusPending, byName["a-pending.pdf"].Status)
		assert.Equal(t, "A(job)", byName["a-pending.pdf"].Title, "job payload title wins before ingest")
		assert.Nil(t, byName["a-pending.pdf"].BookID)
		assert.NotNil(t, byName["a-pending.pdf"].SizeBytes)
		assert.NotNil(t, byName["a-pending.pdf"].UploadedAt)
		assert.Equal(t, pendingPath, byName["a-pending.pdf"].FilePath)
		assert.True(t, byName["a-pending.pdf"].Deletable, "Pi uploads are deletable")

		assert.Equal(t, bookUC.StatusProcessing, byName["b-running.pdf"].Status)
		assert.Equal(t, bookUC.StatusFailed, byName["d-failed.pdf"].Status)

		done := byName["c-done.pdf"]
		assert.Equal(t, bookUC.StatusDone, done.Status)
		assert.Equal(t, "C 完全版", done.Title, "books row title wins over job payload")
		require.NotNil(t, done.BookID)
		assert.Equal(t, int64(7), *done.BookID)
		require.NotNil(t, done.ChunkCount)
		assert.Equal(t, 42, *done.ChunkCount)

		cli := byName["cli.pdf"]
		assert.Equal(t, bookUC.StatusDone, cli.Status, "books row with no job (CLI ingest) reads done")
		assert.Nil(t, cli.SizeBytes, "no PDF on the Pi for a CLI ingest")
		assert.Nil(t, cli.UploadedAt)
		require.NotNil(t, cli.BookID)
		assert.Equal(t, int64(8), *cli.BookID)
		assert.Equal(t, "/Users/mac/books/cli.pdf", cli.FilePath)
		assert.False(t, cli.Deletable, "CLI books (Mac path) are not deletable via the API")
	})

	t.Run("disk file with no job and no book reads pending", func(t *testing.T) {
		svc := newService(t, &fakeRepo{}, &fakeJobs{})
		require.NoError(t, os.WriteFile(filepath.Join(svc.Dir, "manual.pdf"), []byte("%PDF"), 0o600))

		entries, err := svc.List(ctx)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, bookUC.StatusPending, entries[0].Status)
		assert.Equal(t, "manual", entries[0].Title, "title derived from the filename stem")
	})

	t.Run("missing books dir means empty list", func(t *testing.T) {
		svc := newService(t, &fakeRepo{}, &fakeJobs{})
		svc.Dir = filepath.Join(svc.Dir, "does-not-exist")

		entries, err := svc.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("sorted by filename", func(t *testing.T) {
		svc := newService(t, &fakeRepo{}, &fakeJobs{})
		for _, name := range []string{"zz.pdf", "aa.pdf", "mm.pdf"} {
			require.NoError(t, os.WriteFile(filepath.Join(svc.Dir, name), []byte("%PDF"), 0o600))
		}
		entries, err := svc.List(ctx)
		require.NoError(t, err)
		var names []string
		for _, e := range entries {
			names = append(names, e.Filename)
		}
		assert.Equal(t, []string{"aa.pdf", "mm.pdf", "zz.pdf"}, names)
	})

	t.Run("repo error propagates", func(t *testing.T) {
		svc := newService(t, &fakeRepo{err: errors.New("db down")}, &fakeJobs{})
		_, err := svc.List(ctx)
		assert.Error(t, err)
	})
}

/* ─────────────────────────── Delete ─────────────────────────── */

func TestService_Delete(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		filename     string
		fileOnDisk   bool
		deleteReturn bool  // books row existed
		cancelReturn int64 // pending jobs cancelled
		wantErr      error
	}{
		{"full delete: file + book row", "book.pdf", true, true, 0, nil},
		{"pre-ingest: file + pending job only", "book.pdf", true, false, 1, nil},
		{"file only", "book.pdf", true, false, 0, nil},
		{"book row only (CLI-era leftovers)", "book.pdf", false, true, 0, nil},
		{"nothing anywhere", "ghost.pdf", false, false, 0, bookUC.ErrNotFound},
		{"traversal filename rejected", "../escape.pdf", false, false, 0, bookUC.ErrInvalidFilename},
		{"hidden filename rejected", ".upload-1", false, false, 0, bookUC.ErrInvalidFilename},
		{"non-pdf rejected", "book.txt", false, false, 0, bookUC.ErrNotPDF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{deleteReturn: tt.deleteReturn, cancelReturn: tt.cancelReturn}
			svc := newService(t, repo, &fakeJobs{})
			if tt.fileOnDisk {
				require.NoError(t, os.WriteFile(filepath.Join(svc.Dir, tt.filename), []byte("%PDF"), 0o600))
			}

			err := svc.Delete(ctx, tt.filename)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				if errors.Is(tt.wantErr, bookUC.ErrInvalidFilename) || errors.Is(tt.wantErr, bookUC.ErrNotPDF) {
					assert.Empty(t, repo.deletedPaths, "invalid names must not reach the repository")
					assert.Empty(t, repo.cancelledPaths)
				}
				return
			}
			require.NoError(t, err)

			canonical := filepath.Join(svc.Dir, tt.filename)
			assert.Equal(t, []string{canonical}, repo.cancelledPaths)
			assert.Equal(t, []string{canonical}, repo.deletedPaths)
			_, statErr := os.Stat(canonical)
			assert.True(t, os.IsNotExist(statErr), "the PDF must be gone")
		})
	}
}

/* ─────────────────────── SweepStagingFiles ─────────────────────── */

func TestSweepStagingFiles(t *testing.T) {
	t.Run("removes only staging temp files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".upload-123"), []byte("%PDF partial"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".upload-456"), []byte("%PDF partial"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "book.pdf"), []byte("%PDF kept"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".other-hidden"), []byte("kept"), 0o600))

		removed, err := bookUC.SweepStagingFiles(dir)
		require.NoError(t, err)
		assert.Equal(t, 2, removed)
		assertDirHasOnly(t, dir, "book.pdf", ".other-hidden")
	})

	t.Run("empty dir is a no-op", func(t *testing.T) {
		removed, err := bookUC.SweepStagingFiles(t.TempDir())
		require.NoError(t, err)
		assert.Zero(t, removed)
	})

	t.Run("missing dir is not an error", func(t *testing.T) {
		removed, err := bookUC.SweepStagingFiles(filepath.Join(t.TempDir(), "nope"))
		require.NoError(t, err)
		assert.Zero(t, removed)
	})
}
