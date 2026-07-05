package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/notify"
)

func TestSlack_Notify(t *testing.T) {
	tests := []struct {
		name       string
		msg        notify.Message
		status     int
		wantErr    bool
		wantBlocks int
		wantFirst  string
	}{
		{
			name:       "subject and body become two sections",
			msg:        notify.Message{Subject: "pulse 2026-07-05", Body: "show notes"},
			status:     http.StatusOK,
			wantBlocks: 2,
			wantFirst:  "*pulse 2026-07-05*",
		},
		{
			name:       "link wraps the subject in mrkdwn",
			msg:        notify.Message{Subject: "ep", Body: "notes", Link: "http://pi/private/episodes/2.mp3"},
			status:     http.StatusOK,
			wantBlocks: 2,
			wantFirst:  "*<http://pi/private/episodes/2.mp3|ep>*",
		},
		{
			name:       "empty body sends the subject block only",
			msg:        notify.Message{Subject: "障害"},
			status:     http.StatusOK,
			wantBlocks: 1,
			wantFirst:  "*障害*",
		},
		{
			name:    "non-2xx is an error",
			msg:     notify.Message{Subject: "x"},
			status:  http.StatusInternalServerError,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			destination := notify.NewSlack(server.URL, time.Second)
			assert.Equal(t, "slack", destination.Name())

			err := destination.Notify(context.Background(), tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var payload struct {
				Text   string `json:"text"`
				Blocks []struct {
					Type string `json:"type"`
					Text *struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"text"`
				} `json:"blocks"`
			}
			require.NoError(t, json.Unmarshal(body, &payload))
			assert.Equal(t, tt.msg.Subject, payload.Text)
			require.Len(t, payload.Blocks, tt.wantBlocks)
			assert.Equal(t, "section", payload.Blocks[0].Type)
			assert.Equal(t, tt.wantFirst, payload.Blocks[0].Text.Text)
			if tt.wantBlocks == 2 {
				assert.Equal(t, tt.msg.Body, payload.Blocks[1].Text.Text)
			}
		})
	}
}
