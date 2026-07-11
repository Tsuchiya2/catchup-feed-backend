package book

import (
	"time"

	bookUC "catchup-feed/internal/usecase/book"
)

// DTO is one dashboard book entry (D-25): the merge of the uploaded PDF on
// disk, the ingested books row and the latest book_ingest job.
// size_bytes/uploaded_at are null for books ingested via the Mac CLI whose
// PDF never lived on the Pi; book_id/chunk_count are null until ingest
// completes.
type DTO struct {
	// Filename is the canonical name: the DELETE key (unique only among
	// deletable entries — a CLI book may share a basename).
	Filename string `json:"filename" example:"golang-book.pdf"`
	Title    string `json:"title" example:"実用 Go 言語"`
	// FilePath is the canonical identity (books.file_path / job payload):
	// unique across all entries. Pi uploads live under BOOKS_DIR; CLI
	// ingests carry the Mac-resolved source path.
	FilePath string `json:"file_path" example:"/data/books/golang-book.pdf"`
	// Deletable reports whether DELETE /books/{filename} can reach this
	// entry: true for Pi uploads, false for CLI-ingested books (those stay
	// managed by the pulse-books CLI).
	Deletable bool `json:"deletable" example:"true"`
	// SizeBytes of the PDF on disk.
	SizeBytes *int64 `json:"size_bytes" example:"1048576"`
	// UploadedAt is the file modification time on disk.
	UploadedAt *time.Time `json:"uploaded_at"`
	// Status is the ingest status derived from jobs:
	// pending | processing | done | failed.
	Status string `json:"status" example:"pending" enums:"pending,processing,done,failed"`
	// BookID is books.id once ingested.
	BookID *int64 `json:"book_id" example:"3"`
	// ChunkCount is the number of book_chunks once ingested.
	ChunkCount *int `json:"chunk_count" example:"412"`
}

func toDTO(e bookUC.Entry) DTO {
	return DTO{
		Filename:   e.Filename,
		Title:      e.Title,
		FilePath:   e.FilePath,
		Deletable:  e.Deletable,
		SizeBytes:  e.SizeBytes,
		UploadedAt: e.UploadedAt,
		Status:     e.Status,
		BookID:     e.BookID,
		ChunkCount: e.ChunkCount,
	}
}
