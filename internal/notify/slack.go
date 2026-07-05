package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Slack Block Kit limits (carried over from the old notifier).
const (
	slackMaxSectionText = 3000
	slackMaxFallback    = 150
)

// Slack posts messages to a Slack incoming webhook (admin channel, D-7).
// Attachments are ignored — the mp3 rides Discord only (§7).
type Slack struct {
	webhookURL string
	client     *http.Client
}

// NewSlack builds a Slack destination. timeout bounds one webhook call.
func NewSlack(webhookURL string, timeout time.Duration) *Slack {
	return &Slack{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: timeout},
	}
}

func (s *Slack) Name() string { return "slack" }

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackBlock struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
}

type slackPayload struct {
	Text   string       `json:"text"` // fallback for OS notifications
	Blocks []slackBlock `json:"blocks"`
}

// Notify posts one Block Kit message: a bold subject (linked when the
// message carries a URL) followed by the body.
func (s *Slack) Notify(ctx context.Context, msg Message) error {
	subject := "*" + msg.Subject + "*"
	if msg.Link != "" {
		subject = fmt.Sprintf("*<%s|%s>*", msg.Link, msg.Subject)
	}
	payload := slackPayload{
		Text: truncate(msg.Subject, slackMaxFallback),
		Blocks: []slackBlock{
			{Type: "section", Text: &slackText{Type: "mrkdwn", Text: truncate(subject, slackMaxSectionText)}},
		},
	}
	if msg.Body != "" {
		payload.Blocks = append(payload.Blocks,
			slackBlock{Type: "section", Text: &slackText{Type: "mrkdwn", Text: truncate(msg.Body, slackMaxSectionText)}})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("slack: webhook returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
