package notify

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMail(t *testing.T) {
	now := time.Date(2026, 7, 5, 6, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		from    string
		to      string
		subject string
		body    string
		wantErr bool
	}{
		{
			name:    "japanese subject and body survive encoding",
			from:    "pulse@example.com",
			to:      "friend@example.com",
			subject: "pulse 2026-07-05 のエピソード",
			body:    "今朝のショーノート\nhttps://example.com/article",
		},
		{
			name:    "CRLF in recipient is rejected (header injection)",
			from:    "pulse@example.com",
			to:      "evil@example.com\r\nBcc: spam@example.com",
			wantErr: true,
		},
		{
			name:    "CRLF in sender is rejected",
			from:    "pulse@example.com\r\nX-Evil: 1",
			to:      "friend@example.com",
			wantErr: true,
		},
		{
			name:    "empty recipient is rejected",
			from:    "pulse@example.com",
			to:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := buildMail(tt.from, tt.to, tt.subject, tt.body, now)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			text := string(msg)
			header, encoded, found := strings.Cut(text, "\r\n\r\n")
			require.True(t, found, "message must have a header/body separator")

			assert.Contains(t, header, "From: "+tt.from+"\r\n")
			assert.Contains(t, header, "To: "+tt.to+"\r\n")
			assert.Contains(t, header, "Subject: =?UTF-8?")
			assert.Contains(t, header, "Content-Type: text/plain; charset=UTF-8")
			assert.Contains(t, header, "Content-Transfer-Encoding: base64")
			assert.Contains(t, header, "Date: Sun, 05 Jul 2026 06:30:00 +0000")

			decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, "\r\n", ""))
			require.NoError(t, err)
			assert.Equal(t, tt.body, string(decoded))

			// RFC 2045: no line longer than 78 chars in the encoded body.
			for _, line := range strings.Split(strings.TrimRight(encoded, "\r\n"), "\r\n") {
				assert.LessOrEqual(t, len(line), 76)
			}
		})
	}
}

// fakeSMTPServer speaks just enough SMTP (no TLS, no AUTH) to exercise the
// client-side conversation.
type fakeSMTPServer struct {
	addr string
	got  struct {
		from string
		rcpt string
		data string
	}
	done chan struct{}
}

func startFakeSMTP(t *testing.T) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	server := &fakeSMTPServer{addr: listener.Addr().String(), done: make(chan struct{})}
	go func() {
		defer close(server.done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

		write("220 fake ESMTP")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
				write("250-fake")
				write("250 8BITMIME")
			case strings.HasPrefix(line, "MAIL FROM:"):
				server.got.from = line
				write("250 OK")
			case strings.HasPrefix(line, "RCPT TO:"):
				server.got.rcpt = line
				write("250 OK")
			case line == "DATA":
				write("354 go ahead")
				var data strings.Builder
				for {
					dataLine, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimRight(dataLine, "\r\n") == "." {
						break
					}
					data.WriteString(dataLine)
				}
				server.got.data = data.String()
				write("250 queued")
			case line == "QUIT":
				write("221 bye")
				return
			default:
				write("250 OK")
			}
		}
	}()
	return server
}

func TestSMTPMailer_Send(t *testing.T) {
	server := startFakeSMTP(t)
	host, portStr, err := net.SplitHostPort(server.addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	mailer := NewSMTPMailer(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "pulse@example.com",
		Timeout: 2 * time.Second,
	})
	err = mailer.Send(context.Background(), "friend@example.com", "新着エピソード", "ショーノート本文")
	require.NoError(t, err)

	<-server.done
	assert.Equal(t, "MAIL FROM:<pulse@example.com>", strings.TrimSuffix(server.got.from, " BODY=8BITMIME"))
	assert.Equal(t, "RCPT TO:<friend@example.com>", server.got.rcpt)
	assert.Contains(t, server.got.data, "To: friend@example.com")
	assert.Contains(t, server.got.data, "Subject: =?UTF-8?")
}

func TestSMTPMailer_Send_RejectsInjection(t *testing.T) {
	mailer := NewSMTPMailer(SMTPConfig{Host: "smtp.example.com", Port: 587, From: "pulse@example.com"})
	err := mailer.Send(context.Background(), "a@example.com\r\nRCPT TO:<b@example.com>", "s", "b")
	require.Error(t, err) // fails in buildMail before any network I/O
}
