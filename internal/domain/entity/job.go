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
)

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
