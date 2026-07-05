package notify

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestLoadDestinationsFromEnv(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantNames []string
	}{
		{
			name:      "nothing enabled",
			env:       map[string]string{},
			wantNames: nil,
		},
		{
			name: "both channels enabled with valid URLs (D-7)",
			env: map[string]string{
				"DISCORD_ENABLED":     "true",
				"DISCORD_WEBHOOK_URL": "https://discord.com/api/webhooks/1/abc",
				"SLACK_ENABLED":       "true",
				"SLACK_WEBHOOK_URL":   "https://hooks.slack.com/services/T/B/x",
			},
			wantNames: []string{"discord", "slack"},
		},
		{
			name: "enabled flag without URL drops the channel",
			env: map[string]string{
				"DISCORD_ENABLED": "true",
			},
			wantNames: nil,
		},
		{
			name: "wrong host fails closed",
			env: map[string]string{
				"DISCORD_ENABLED":     "true",
				"DISCORD_WEBHOOK_URL": "https://evil.example.com/api/webhooks/1/abc",
				"SLACK_ENABLED":       "true",
				"SLACK_WEBHOOK_URL":   "https://hooks.slack.com/wrong-path/x",
			},
			wantNames: nil,
		},
		{
			name: "http (non-TLS) fails closed",
			env: map[string]string{
				"SLACK_ENABLED":     "true",
				"SLACK_WEBHOOK_URL": "http://hooks.slack.com/services/T/B/x",
			},
			wantNames: nil,
		},
		{
			name: "URL alone without the enabled flag stays off (宣言的有効化)",
			env: map[string]string{
				"DISCORD_WEBHOOK_URL": "https://discord.com/api/webhooks/1/abc",
			},
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			destinations := LoadDestinationsFromEnv(discard())
			var names []string
			for _, destination := range destinations {
				names = append(names, destination.Name())
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

func TestLoadSMTPFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantNil  bool
		wantPort int
		wantFrom string
	}{
		{
			name:    "disabled by default",
			env:     map[string]string{},
			wantNil: true,
		},
		{
			name: "gmail-style config",
			env: map[string]string{
				"SMTP_ENABLED":  "true",
				"SMTP_HOST":     "smtp.gmail.com",
				"SMTP_PORT":     "587",
				"SMTP_USERNAME": "me@gmail.com",
				"SMTP_PASSWORD": "app-password",
			},
			wantPort: 587,
			wantFrom: "me@gmail.com", // From defaults to the username
		},
		{
			name: "explicit from overrides username",
			env: map[string]string{
				"SMTP_ENABLED":  "true",
				"SMTP_HOST":     "smtp.example.com",
				"SMTP_USERNAME": "user",
				"SMTP_FROM":     "pulse@example.com",
			},
			wantPort: 587, // default
			wantFrom: "pulse@example.com",
		},
		{
			name: "missing host disables email",
			env: map[string]string{
				"SMTP_ENABLED": "true",
				"SMTP_FROM":    "pulse@example.com",
			},
			wantNil: true,
		},
		{
			name: "invalid port disables email",
			env: map[string]string{
				"SMTP_ENABLED": "true",
				"SMTP_HOST":    "smtp.example.com",
				"SMTP_PORT":    "not-a-port",
				"SMTP_FROM":    "pulse@example.com",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			mailer := LoadSMTPFromEnv(discard())
			if tt.wantNil {
				assert.Nil(t, mailer)
				return
			}
			require.NotNil(t, mailer)
			assert.Equal(t, tt.wantPort, mailer.cfg.Port)
			assert.Equal(t, tt.wantFrom, mailer.cfg.From)
		})
	}
}
