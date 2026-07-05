package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/notify"
)

// discordCapture records the last webhook request.
type discordCapture struct {
	contentType string
	body        []byte
}

func newDiscordServer(t *testing.T, status int, capture *discordCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.contentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		capture.body = body
		w.WriteHeader(status)
	}))
}

func TestDiscord_Notify(t *testing.T) {
	attachment := filepath.Join(t.TempDir(), "ep.mp3")
	require.NoError(t, os.WriteFile(attachment, []byte("mp3-bytes"), 0o600))

	tests := []struct {
		name          string
		msg           notify.Message
		status        int
		wantErr       bool
		wantMultipart bool
	}{
		{
			name:   "plain message posts a JSON embed",
			msg:    notify.Message{Subject: "pulse 2026-07-05", Body: "show notes", Link: "http://pi/private/episodes/1.mp3"},
			status: http.StatusNoContent,
		},
		{
			name: "small attachment posts multipart with payload_json and file",
			msg: notify.Message{
				Subject:         "pulse 2026-07-05",
				Body:            "show notes",
				AttachmentPath:  attachment,
				AttachmentBytes: 9,
			},
			status:        http.StatusOK,
			wantMultipart: true,
		},
		{
			name: "attachment at the 10MB limit falls back to JSON (§7: 10MB 未満のみ添付)",
			msg: notify.Message{
				Subject:         "big",
				AttachmentPath:  attachment,
				AttachmentBytes: 10 << 20,
			},
			status: http.StatusNoContent,
		},
		{
			name: "missing attachment file degrades to JSON embed",
			msg: notify.Message{
				Subject:         "gone",
				AttachmentPath:  filepath.Join(t.TempDir(), "missing.mp3"),
				AttachmentBytes: 5,
			},
			status: http.StatusNoContent,
		},
		{
			name:    "non-2xx is an error",
			msg:     notify.Message{Subject: "x"},
			status:  http.StatusBadRequest,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capture discordCapture
			server := newDiscordServer(t, tt.status, &capture)
			defer server.Close()

			destination := notify.NewDiscord(server.URL, time.Second, slog.New(slog.DiscardHandler))
			assert.Equal(t, "discord", destination.Name())

			err := destination.Notify(context.Background(), tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantMultipart {
				assert.True(t, strings.HasPrefix(capture.contentType, "multipart/form-data"),
					"content type: %s", capture.contentType)
				assert.Contains(t, string(capture.body), "payload_json")
				assert.Contains(t, string(capture.body), "mp3-bytes")
				assert.Contains(t, string(capture.body), filepath.Base(tt.msg.AttachmentPath))
				return
			}

			assert.Equal(t, "application/json", capture.contentType)
			var payload struct {
				Embeds []struct {
					Title       string `json:"title"`
					Description string `json:"description"`
					URL         string `json:"url"`
				} `json:"embeds"`
			}
			require.NoError(t, json.Unmarshal(capture.body, &payload))
			require.Len(t, payload.Embeds, 1)
			assert.Equal(t, tt.msg.Subject, payload.Embeds[0].Title)
			assert.Equal(t, tt.msg.Body, payload.Embeds[0].Description)
			assert.Equal(t, tt.msg.Link, payload.Embeds[0].URL)
		})
	}
}

func TestDiscord_Notify_TruncatesLongBody(t *testing.T) {
	var capture discordCapture
	server := newDiscordServer(t, http.StatusOK, &capture)
	defer server.Close()

	destination := notify.NewDiscord(server.URL, time.Second, slog.New(slog.DiscardHandler))
	// Multi-byte body: the cut must land on a rune boundary.
	long := strings.Repeat("あ", 2000) // 6000 bytes > 4096 limit
	err := destination.Notify(context.Background(), notify.Message{Subject: "t", Body: long})
	require.NoError(t, err)

	var payload struct {
		Embeds []struct {
			Description string `json:"description"`
		} `json:"embeds"`
	}
	require.NoError(t, json.Unmarshal(capture.body, &payload))
	require.Len(t, payload.Embeds, 1)
	description := payload.Embeds[0].Description
	assert.LessOrEqual(t, len(description), 4096)
	assert.True(t, strings.HasSuffix(description, "..."))
	assert.True(t, json.Valid(capture.body)) // no broken UTF-8 escaped its way in
}
