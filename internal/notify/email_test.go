package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSender struct {
	err  error
	to   []string
	subj []string
	body []string
}

func (s *fakeSender) Send(_ context.Context, to, subject, body string) error {
	if s.err != nil {
		return s.err
	}
	s.to = append(s.to, to)
	s.subj = append(s.subj, subject)
	s.body = append(s.body, body)
	return nil
}

func TestEmail_Notify(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		wantBody string
	}{
		{
			name:     "subject and body pass through",
			msg:      Message{Subject: "catchup-feed 障害: radio の実行が失敗しました", Body: "VOICEVOX unreachable"},
			wantBody: "VOICEVOX unreachable",
		},
		{
			name:     "link is appended to the body (plain text has no embeds)",
			msg:      Message{Subject: "pulse 2026-07-26", Body: "show notes", Link: "http://pi.tailnet:8081/private/episodes/7.mp3"},
			wantBody: "show notes\n\nhttp://pi.tailnet:8081/private/episodes/7.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{}
			destination := NewEmail(sender, "admin@example.com")
			assert.Equal(t, "email", destination.Name())

			require.NoError(t, destination.Notify(context.Background(), tt.msg))
			require.Len(t, sender.to, 1)
			assert.Equal(t, "admin@example.com", sender.to[0])
			assert.Equal(t, tt.msg.Subject, sender.subj[0])
			assert.Equal(t, tt.wantBody, sender.body[0])
		})
	}

	t.Run("send failure surfaces to the caller (queue / best-effort decides)", func(t *testing.T) {
		sender := &fakeSender{err: errors.New("smtp down")}
		destination := NewEmail(sender, "admin@example.com")
		err := destination.Notify(context.Background(), Message{Subject: "s", Body: "b"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "smtp down")
	})
}
