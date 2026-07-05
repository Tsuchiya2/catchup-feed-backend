package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Discord message limits and the direct-attachment ceiling (§7: mp3 が
// 10MB 未満なら Discord へ直接添付).
const (
	discordMaxTitle       = 256
	discordMaxDescription = 4096
	discordAttachLimit    = 10 << 20 // 10 MiB

	// discordBlue is the embed accent color (#5865F2), carried over from
	// the old notifier.
	discordBlue = 5793266
)

// Discord posts messages to a Discord webhook (admin channel, D-7).
// The webhook implementation is carried over from the old notifier; the
// per-article rate limiter and internal retry loop are not — one episode
// per day needs neither, and retries belong to the jobs queue (§7).
type Discord struct {
	webhookURL string
	client     *http.Client
	logger     *slog.Logger
}

// NewDiscord builds a Discord destination. timeout bounds one webhook call.
func NewDiscord(webhookURL string, timeout time.Duration, logger *slog.Logger) *Discord {
	if logger == nil {
		logger = slog.Default()
	}
	return &Discord{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: timeout},
		logger:     logger,
	}
}

func (d *Discord) Name() string { return "discord" }

type discordEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	Color       int    `json:"color"`
	Timestamp   string `json:"timestamp"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

// Notify posts one embed; when the message references an mp3 under the
// attachment limit it is uploaded in the same request (multipart).
// Attachment problems degrade to a plain embed instead of failing the
// notification — the audio is already reachable via the feed (§8 縮退).
func (d *Discord) Notify(ctx context.Context, msg Message) error {
	payload := discordPayload{Embeds: []discordEmbed{{
		Title:       truncate(msg.Subject, discordMaxTitle),
		Description: truncate(msg.Body, discordMaxDescription),
		URL:         msg.Link,
		Color:       discordBlue,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}}}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discord: marshal payload: %w", err)
	}

	var req *http.Request
	if msg.AttachmentPath != "" && msg.AttachmentBytes > 0 && msg.AttachmentBytes < discordAttachLimit {
		req, err = d.multipartRequest(ctx, payloadJSON, msg.AttachmentPath)
		if err != nil {
			// Degrade to text-only: a missing / unreadable mp3 must not
			// block the notification itself.
			d.logger.Warn("discord: attachment unavailable, sending text only",
				slog.String("path", msg.AttachmentPath), slog.Any("error", err))
			req = nil
		}
	}
	if req == nil {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(payloadJSON))
		if err != nil {
			return fmt.Errorf("discord: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord: execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("discord: webhook returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// multipartRequest builds the webhook request with payload_json plus the
// mp3 as files[0]. The whole file is buffered — episodes are <10MiB by the
// attach guard, fine on the Pi.
func (d *Discord) multipartRequest(ctx context.Context, payloadJSON []byte, path string) (*http.Request, error) {
	audio, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
		return nil, fmt.Errorf("write payload_json: %w", err)
	}
	part, err := writer.CreateFormFile("files[0]", filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, fmt.Errorf("write attachment: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// truncate cuts s to at most max bytes, appending "..." when it cut. It
// backs up to a rune boundary so Japanese text is never split mid-rune.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const suffix = "..."
	cut := max - len(suffix)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && s[cut]&0xC0 == 0x80 { // continuation byte
		cut--
	}
	return s[:cut] + suffix
}
