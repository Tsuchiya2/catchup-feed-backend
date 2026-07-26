package notify

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestLoadDestinationsFromEnv(t *testing.T) {
	mailer := NewSMTPMailer(SMTPConfig{Host: "smtp.example.com", Port: 587, From: "pulse@example.com"})

	tests := []struct {
		name      string
		to        string
		mailer    *SMTPMailer
		wantNames []string
	}{
		{
			name:      "unset address disables admin notifications",
			to:        "",
			mailer:    mailer,
			wantNames: nil,
		},
		{
			name:      "valid address with SMTP enabled (D-29)",
			to:        "admin@example.com",
			mailer:    mailer,
			wantNames: []string{"email"},
		},
		{
			name:      "address set but SMTP disabled drops the channel (縮退)",
			to:        "admin@example.com",
			mailer:    nil,
			wantNames: nil,
		},
		{
			name:      "address without @ fails closed",
			to:        "not-an-address",
			mailer:    mailer,
			wantNames: nil,
		},
		{
			name:      "address with whitespace fails closed",
			to:        "admin@example.com evil@example.com",
			mailer:    mailer,
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NOTIFY_ERROR_EMAIL_TO", tt.to)
			destinations := LoadDestinationsFromEnv(discard(), tt.mailer)
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
