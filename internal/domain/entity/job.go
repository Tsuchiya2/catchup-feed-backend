package entity

import (
	"encoding/json"
	"time"
)

// Job statuses (§4: jobs.status).
const (
	JobStatusPending = "pending"
	JobStatusRunning = "running"
	JobStatusDone    = "done"
	JobStatusFailed  = "failed"
)

// Well-known job kinds (§4: jobs.kind). The list is open-ended; these
// constants only name the kinds known so far.
const (
	JobKindRegenerateFeed  = "regenerate_feed"
	JobKindNotifyEpisode   = "notify_episode"
	JobKindNotifyError     = "notify_error"      // §8: radio バッチ失敗の本人通知(best-effort)
	JobKindCleanupOldMedia = "cleanup_old_media" // D-4: 45日より古い mp3 の掃除
	// JobKindTranscribe is enqueued by the Pi worker for youtube/podcast
	// sources (Phase 2 §5) and claimed ONLY by the Mac transcribe worker
	// (catchup-feed-ai). The Pi consumer must never register a handler for
	// it: unregistered kinds stay pending instead of being claimed.
	JobKindTranscribe = "transcribe"
	// JobKindBookIngest is enqueued by the book upload API (D-25) and
	// claimed ONLY by the Mac worker (catchup-feed-ai, pulse-books): the
	// ingest pipeline (PDF extraction, chunking, Ollama embeddings) is
	// local-LLM-only (C-12) and Ollama lives on the Mac. Like transcribe,
	// the Pi consumer must never register a handler for it.
	JobKindBookIngest = "book_ingest"
)

// TranscribePayload is the jobs.payload contract for kind='transcribe'
// (Phase 2 §4/§5). The Python transcribe worker (Mac) reads exactly these
// keys; treat renames as a cross-repo breaking change.
type TranscribePayload struct {
	ArticleID  int64  `json:"article_id"`
	MediaURL   string `json:"media_url"`   // youtube: 動画 URL / podcast: enclosure 音声 URL
	SourceKind string `json:"source_kind"` // 'youtube' | 'podcast'
}

// BookIngestPayload is the jobs.payload contract for kind='book_ingest'
// (D-25). The Python book worker (Mac, pulse-books) reads exactly these
// keys; treat renames as a cross-repo breaking change.
//
// FilePath is the canonical absolute path of the PDF **as seen by the Pi
// server** (BOOKS_DIR + sanitized filename). It is the identity key of the
// book (same semantics as the CLI ingest: re-upload of the same name is a
// replace, books.file_path = this value). The Mac worker does NOT read
// this path from its own filesystem — it downloads the file from the
// tailnet-only endpoint GET /private/books/{filename}, building the URL
// from its own env plus filepath.Base(FilePath). The fetch base URL is
// deliberately not part of the payload.
type BookIngestPayload struct {
	FilePath string `json:"file_path"`
	Title    string `json:"title"`
}

// Job is one row of the jobs table (§4), the sole inter-process channel
// between worker (Pi) and radio (Mac): C-4 — no internal HTTP/RPC. A DB
// queue survives restarts and fits the nightly-batch cadence.
type Job struct {
	ID        int64
	Kind      string
	Payload   json.RawMessage
	Status    string
	Attempts  int
	LastError *string
	RunAfter  time.Time
	CreatedAt time.Time
}
