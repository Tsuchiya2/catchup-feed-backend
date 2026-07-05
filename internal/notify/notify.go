// Package notify implements the §7 notification layer: a destination-type
// interface with Discord / Slack webhook implementations for the admin
// (D-7: 本人=Discord+Slack、config で宣言的に有効/無効) and a plain SMTP
// mailer for friends (C-11: 友人=メール). Destinations are deliberately
// single-shot — no circuit breaker, no internal retry loops (設計原則1:
// 右サイズ). Delivery failures are handled by the jobs queue retry
// (§7: attempts 上限 3), and the notify_error path is best-effort.
package notify

import "context"

// Message is one notification. §7's payload is タイトル+ショーノート+
// エピソードURL; the same shape also carries error notices (§8), which is
// why the interface takes a Message rather than an Episode — the episode
// formatting happens in the jobs handler.
type Message struct {
	// Subject is the short line: episode title or error headline.
	Subject string
	// Body is the long text: show notes or error detail.
	Body string
	// Link is an optional URL shown with the message (episode URL).
	Link string
	// AttachmentPath / AttachmentBytes optionally reference a local mp3.
	// Only Discord uses them (§7: mp3 が 10MB 未満なら Discord へ直接添付、
	// 公開エピソードのみ — the caller enforces the public-only rule, the
	// destination enforces the size limit).
	AttachmentPath  string
	AttachmentBytes int64
}

// Destination is one admin notification channel (§7 / D-7). Implementations
// are swappable and enabled declaratively via config; adding or removing a
// channel must never require touching the dispatch code.
type Destination interface {
	// Name identifies the channel in logs ("discord", "slack").
	Name() string
	// Notify delivers the message. One shot: retries are the job queue's
	// concern, not the destination's.
	Notify(ctx context.Context, msg Message) error
}
