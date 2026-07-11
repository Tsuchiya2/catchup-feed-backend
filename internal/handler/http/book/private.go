package book

import (
	"log/slog"
	"net/http"
	"os"

	bookUC "catchup-feed/internal/usecase/book"
)

// PrivateFileHandler serves GET /private/books/{file} on the tailnet-only
// listener (D-25 (3)): the Mac worker downloads the PDF here when it
// claims a book_ingest job — the jobs payload carries the Pi path, the
// worker builds this URL from its own env + the path's basename. No
// authentication, same C-5 contract as the private feed: the tailnet bind
// is the boundary, so this handler must never be mounted on the public
// listener. Range requests are honoured via http.ServeContent (useful for
// resuming large PDFs over the tailnet).
type PrivateFileHandler struct {
	// Dir is the books directory (same value the admin API writes to).
	Dir    string
	Logger *slog.Logger
}

func (h PrivateFileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	// Strict validation, no normalization: a traversal-shaped name (an
	// encoded %2F puts a "/" in the path value) is a 404, and the
	// ".upload-*" staging temp files stay unreachable.
	if err := bookUC.ValidateFilename(name); err != nil {
		http.NotFound(w, r)
		return
	}
	// os.Root confines the open to Dir even against symlinks.
	root, err := os.OpenRoot(h.Dir)
	if err != nil {
		h.logger().Warn("books: books dir unavailable", slog.String("dir", h.Dir), slog.Any("error", err))
		http.NotFound(w, r)
		return
	}
	defer func() { _ = root.Close() }()

	f, err := root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (h PrivateFileHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}
