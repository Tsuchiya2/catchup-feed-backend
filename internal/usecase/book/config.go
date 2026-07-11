package book

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultBooksDir mirrors the feed package's episodes default: a relative
// directory next to the binary for development; production sets BOOKS_DIR
// (compose mounts it at /data/books, D-25).
const DefaultBooksDir = "books"

// Config holds the book management settings.
type Config struct {
	// Dir is the absolute books directory. Absolute because
	// filepath.Join(Dir, filename) is the canonical identity recorded in
	// books.file_path and the book_ingest payload — a cwd-relative value
	// would make the identity depend on where the server was started.
	Dir string
}

// LoadConfig reads BOOKS_DIR (default "books") and resolves it absolute.
func LoadConfig() (Config, error) {
	dir := os.Getenv("BOOKS_DIR")
	if dir == "" {
		dir = DefaultBooksDir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Config{}, fmt.Errorf("book: resolve BOOKS_DIR: %w", err)
	}
	return Config{Dir: abs}, nil
}
