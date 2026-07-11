// Package book implements the dashboard book-PDF management use cases
// (D-25): upload (stage + commit + book_ingest enqueue), the merged
// disk/books/jobs listing, and delete. The PDF lives on the Pi filesystem
// under BOOKS_DIR and the DB holds paths only (C-10 と同型); ingest status
// is derived from the jobs table, never stored (ディスクとジョブが正).
package book

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// DefaultMaxUploadBytes is the D-25 upload ceiling (100MB/冊, aligned with
// the Cloudflare free-plan request limit).
const DefaultMaxUploadBytes = 100 << 20

// Ingest statuses shown on the dashboard, derived from the latest
// kind='book_ingest' job (D-25).
const (
	StatusPending    = "pending"    // job pending, or file on disk with no job yet
	StatusProcessing = "processing" // job running (Mac worker claimed it)
	StatusDone       = "done"       // job done, or books row with no job (CLI ingest)
	StatusFailed     = "failed"     // job failed terminally
)

// maxFilenameBytes caps the stored filename length (filesystem limit).
const maxFilenameBytes = 255

// Service provides the book management use cases. Dir must be the
// canonical absolute BOOKS_DIR: filepath.Join(Dir, filename) is the
// identity key recorded in books.file_path and in the book_ingest payload.
type Service struct {
	Repo repository.BookAdminRepository
	Jobs repository.JobRepository
	// Dir is the absolute books directory (BOOKS_DIR).
	Dir string
	// MaxUploadBytes bounds one PDF; 0 means DefaultMaxUploadBytes.
	MaxUploadBytes int64
}

// Entry is one dashboard book row: the merge of a disk PDF (upload state),
// the books row (ingest result) and the latest book_ingest job (status).
// SizeBytes/UploadedAt are nil for books ingested via the Mac CLI, whose
// PDF never lived on the Pi; BookID/ChunkCount are nil until ingest.
type Entry struct {
	Filename   string
	Title      string
	SizeBytes  *int64
	UploadedAt *time.Time
	Status     string
	BookID     *int64
	ChunkCount *int
}

func (s *Service) maxUploadBytes() int64 {
	if s.MaxUploadBytes > 0 {
		return s.MaxUploadBytes
	}
	return DefaultMaxUploadBytes
}

// ---- filename validation ----

// ValidateFilename accepts exactly the filenames this API stores: a single
// non-hidden path element ending in .pdf. It is used verbatim on the
// DELETE/private-GET path values — no normalization, so an attacker-shaped
// name ("../x.pdf", "a/b.pdf" from an encoded %2F segment, control bytes)
// is rejected instead of resolved.
func ValidateFilename(name string) error {
	if name == "" || len(name) > maxFilenameBytes || !utf8.ValidString(name) {
		return ErrInvalidFilename
	}
	if strings.ContainsAny(name, "/\\") || strings.ContainsRune(name, 0) {
		return ErrInvalidFilename
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return ErrInvalidFilename
		}
	}
	// Hidden names are rejected: they cannot be uploaded, and it keeps the
	// staging temp files (".upload-*") out of reach of DELETE.
	if strings.HasPrefix(name, ".") {
		return ErrInvalidFilename
	}
	stem, ok := strings.CutSuffix(strings.ToLower(name), ".pdf")
	if !ok || stem == "" {
		return ErrNotPDF
	}
	return nil
}

// SanitizeUploadFilename derives the stored filename from the multipart
// filename: strip any client-side directory part (browsers may send
// paths), then validate. The result is final — it becomes the canonical
// identity of the book.
func SanitizeUploadFilename(name string) (string, error) {
	name = strings.TrimSpace(name)
	// Base() both of the path flavours a client might send.
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	name = filepath.Base(name)
	if err := ValidateFilename(name); err != nil {
		return "", err
	}
	return name, nil
}

// ---- upload (stage + commit) ----

// StagedUpload is a validated PDF written to a temp file inside Dir,
// waiting for Commit (rename to the canonical name + job enqueue) or
// Discard. The two-phase shape lets the HTTP handler stream the multipart
// file part to disk before it has necessarily seen the title field.
type StagedUpload struct {
	Filename string
	Size     int64
	tmpPath  string
}

// Discard removes the staged temp file. Safe to call after Commit (no-op).
func (up *StagedUpload) Discard() {
	if up == nil || up.tmpPath == "" {
		return
	}
	_ = os.Remove(up.tmpPath)
	up.tmpPath = ""
}

// Stage validates the upload (filename shape, %PDF magic, size ceiling)
// and streams it to a temp file in Dir. The caller must Commit or Discard
// the result.
func (s *Service) Stage(filename string, r io.Reader) (*StagedUpload, error) {
	name, err := SanitizeUploadFilename(filename)
	if err != nil {
		return nil, err
	}

	// Magic-byte check before touching the disk: a PDF starts with "%PDF".
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrNotPDF
		}
		return nil, fmt.Errorf("book: read upload: %w", err)
	}
	if string(magic) != "%PDF" {
		return nil, ErrNotPDF
	}

	f, err := os.CreateTemp(s.Dir, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("book: create temp file: %w", err)
	}
	tmpPath := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := f.Write(magic); err != nil {
		cleanup()
		return nil, fmt.Errorf("book: write upload: %w", err)
	}
	limit := s.maxUploadBytes()
	// LimitReader(limit-3) because 4 magic bytes are already written: total
	// may reach exactly limit; one extra readable byte proves overflow.
	written, err := io.Copy(f, io.LimitReader(r, limit-int64(len(magic))+1))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("book: write upload: %w", err)
	}
	total := written + int64(len(magic))
	if total > limit {
		cleanup()
		return nil, ErrTooLarge
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("book: close upload: %w", err)
	}

	return &StagedUpload{Filename: name, Size: total, tmpPath: tmpPath}, nil
}

// Commit finalizes a staged upload: rename onto the canonical path
// (atomic overwrite — 同名ファイルの再アップは置き換え, D-25 の冪等意味論)
// and enqueue kind='book_ingest' unless a pending job for the same
// canonical path already exists. title falls back to the filename stem.
func (s *Service) Commit(ctx context.Context, up *StagedUpload, title string) (Entry, error) {
	if up == nil || up.tmpPath == "" {
		return Entry{}, errors.New("book: commit of empty staged upload")
	}
	canonical := filepath.Join(s.Dir, up.Filename)
	if err := os.Rename(up.tmpPath, canonical); err != nil {
		up.Discard()
		return Entry{}, fmt.Errorf("book: finalize upload: %w", err)
	}
	up.tmpPath = ""

	title = strings.TrimSpace(title)
	if title == "" {
		title = titleFromFilename(up.Filename)
	}

	pending, err := s.Repo.HasPendingIngest(ctx, canonical)
	if err != nil {
		return Entry{}, err
	}
	if !pending {
		payload, err := json.Marshal(entity.BookIngestPayload{FilePath: canonical, Title: title})
		if err != nil {
			return Entry{}, fmt.Errorf("book: marshal payload: %w", err)
		}
		if _, err := s.Jobs.Enqueue(ctx, entity.JobKindBookIngest, payload, time.Time{}); err != nil {
			// The PDF is saved but no job exists; re-uploading the same file
			// retries the enqueue (idempotent by canonical path).
			return Entry{}, err
		}
	}

	now := time.Now()
	size := up.Size
	return Entry{
		Filename:   up.Filename,
		Title:      title,
		SizeBytes:  &size,
		UploadedAt: &now,
		Status:     StatusPending,
	}, nil
}

// titleFromFilename strips the .pdf extension (any case).
func titleFromFilename(name string) string {
	if stem := name[:len(name)-len(".pdf")]; stem != "" {
		return stem
	}
	return name
}

// ---- list ----

// List merges the three sources of truth (D-25): PDFs on disk (uploaded),
// books rows (ingested — dashboard uploads and CLI ingests alike) and the
// latest book_ingest job per canonical path (status). Keyed by canonical
// file path; sorted by filename for a stable dashboard order.
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	byPath := make(map[string]*Entry)
	var paths []string
	upsert := func(path string) *Entry {
		if e, ok := byPath[path]; ok {
			return e
		}
		e := &Entry{Filename: filepath.Base(path)}
		byPath[path] = e
		paths = append(paths, path)
		return e
	}

	dirEntries, err := os.ReadDir(s.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("book: read books dir: %w", err)
	}
	for _, de := range dirEntries {
		name := de.Name()
		// Skip staging temp files, subdirectories and anything the API
		// could never have stored.
		if !de.Type().IsRegular() || ValidateFilename(name) != nil {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue // raced with a delete
		}
		e := upsert(filepath.Join(s.Dir, name))
		size := info.Size()
		mtime := info.ModTime()
		e.SizeBytes = &size
		e.UploadedAt = &mtime
	}

	records, err := s.Repo.ListBooks(ctx)
	if err != nil {
		return nil, err
	}
	for _, rec := range records {
		e := upsert(rec.FilePath)
		id, chunks := rec.ID, rec.ChunkCount
		e.BookID = &id
		e.ChunkCount = &chunks
		e.Title = rec.Title
	}

	states, err := s.Repo.LatestIngestStates(ctx)
	if err != nil {
		return nil, err
	}
	for path, e := range byPath {
		state, hasJob := states[path]
		switch {
		case hasJob:
			e.Status = statusFromJob(state.Status)
			if e.Title == "" {
				e.Title = state.Title
			}
		case e.BookID != nil:
			e.Status = StatusDone // ingested with no surviving job (e.g. CLI)
		default:
			e.Status = StatusPending // file on disk, nothing else knows it
		}
		if e.Title == "" {
			e.Title = titleFromFilename(e.Filename)
		}
	}

	out := make([]Entry, 0, len(paths))
	for _, path := range paths {
		out = append(out, *byPath[path])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Filename != out[j].Filename {
			return out[i].Filename < out[j].Filename
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

// statusFromJob maps a jobs.status to the dashboard ingest status.
func statusFromJob(jobStatus string) string {
	switch jobStatus {
	case entity.JobStatusRunning:
		return StatusProcessing
	case entity.JobStatusDone:
		return StatusDone
	case entity.JobStatusFailed:
		return StatusFailed
	default: // pending (and anything unknown degrades to "not done yet")
		return StatusPending
	}
}

// ---- delete ----

// Delete removes a book by canonical filename (D-25): pending book_ingest
// jobs are cancelled, DB rows (books + dependents) removed, the PDF
// deleted. Succeeds when at least one of the three existed — a book
// deleted before ingest (no books row yet) still cleans file + job.
// Returns ErrNotFound when nothing was there at all.
func (s *Service) Delete(ctx context.Context, filename string) error {
	if err := ValidateFilename(filename); err != nil {
		return err
	}
	canonical := filepath.Join(s.Dir, filename)

	cancelled, err := s.Repo.CancelPendingIngest(ctx, canonical)
	if err != nil {
		return err
	}
	deletedRows, err := s.Repo.DeleteBookByFilePath(ctx, canonical)
	if err != nil {
		return err
	}
	fileExisted, err := s.removeFile(filename)
	if err != nil {
		return err
	}

	if cancelled == 0 && !deletedRows && !fileExisted {
		return ErrNotFound
	}
	return nil
}

// removeFile deletes the PDF through os.Root so even a hostile filename
// that survived validation could not escape Dir.
func (s *Service) removeFile(filename string) (bool, error) {
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("book: open books dir: %w", err)
	}
	defer func() { _ = root.Close() }()

	if err := root.Remove(filename); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("book: remove pdf: %w", err)
	}
	return true, nil
}
