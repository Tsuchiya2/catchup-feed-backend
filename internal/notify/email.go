package notify

import (
	"context"
	"fmt"
)

// Sender is the slice of SMTPMailer the Email destination needs — one
// plain-text mail to one recipient. Narrow on purpose so tests can fake it.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Email delivers admin notifications as plain-text mail (D-29: 障害通知は
// メールに一本化 — the 7/22 outage went unnoticed for four days on webhook
// channels, the mailbox is the one place actually checked daily). It wraps
// the same SMTPMailer the friend channel uses (C-11), so there is exactly
// one SMTP configuration; only the recipient differs (NOTIFY_ERROR_EMAIL_TO).
type Email struct {
	sender Sender
	to     string
}

// NewEmail builds the admin email destination.
func NewEmail(sender Sender, to string) *Email {
	return &Email{sender: sender, to: to}
}

// Name implements Destination.
func (e *Email) Name() string { return "email" }

// Notify sends one mail. The optional link travels in the body — plain
// text has no embed concept, and HTML mail is out of scope (右サイズ).
func (e *Email) Notify(ctx context.Context, msg Message) error {
	body := msg.Body
	if msg.Link != "" {
		body += "\n\n" + msg.Link
	}
	if err := e.sender.Send(ctx, e.to, msg.Subject, body); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	return nil
}
