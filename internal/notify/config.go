package notify

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

func getenv(key string) string { return os.Getenv(key) }

// LoadDestinationsFromEnv assembles the admin destinations (D-29: 本人向け
// 通知はメールに一本化). mailer is the shared SMTP mailer from
// LoadSMTPFromEnv; the channel is active only when the mailer exists and
// NOTIFY_ERROR_EMAIL_TO holds a plausible address — anything else logs a
// warning and drops the channel: notifications degrade, the worker keeps
// running (§8).
//
// Environment variables:
//   - NOTIFY_ERROR_EMAIL_TO: admin recipient for notify_error and the admin
//     copy of notify_episode (SMTP_* is read by LoadSMTPFromEnv)
func LoadDestinationsFromEnv(logger *slog.Logger, mailer *SMTPMailer) []Destination {
	if logger == nil {
		logger = slog.Default()
	}
	to := getenv("NOTIFY_ERROR_EMAIL_TO")
	if to == "" {
		logger.Info("notify: admin email disabled (NOTIFY_ERROR_EMAIL_TO empty)")
		return nil
	}
	if mailer == nil {
		logger.Warn("notify: NOTIFY_ERROR_EMAIL_TO is set but SMTP is disabled, dropping admin notifications")
		return nil
	}
	// A malformed address must fail closed here rather than on every send.
	// buildMail rejects CR/LF again at send time (header injection guard).
	if strings.ContainsAny(to, "\r\n ") || !strings.Contains(to, "@") {
		logger.Warn("notify: invalid NOTIFY_ERROR_EMAIL_TO, dropping admin notifications")
		return nil
	}
	logger.Info("notify: channel enabled", slog.String("channel", "email"))
	return []Destination{NewEmail(mailer, to)}
}

// LoadSMTPFromEnv builds the shared SMTP mailer — friend mail (C-11) and,
// combined with NOTIFY_ERROR_EMAIL_TO, admin notifications (D-29). Returns
// nil when SMTP_ENABLED is not "true" or the configuration is incomplete —
// email silently off, podcast delivery unaffected.
//
// Environment variables:
//   - SMTP_ENABLED: "true" to enable
//   - SMTP_HOST / SMTP_PORT (default 587)
//   - SMTP_USERNAME / SMTP_PASSWORD (e.g. Gmail address + app password)
//   - SMTP_FROM (default SMTP_USERNAME)
func LoadSMTPFromEnv(logger *slog.Logger) *SMTPMailer {
	if logger == nil {
		logger = slog.Default()
	}
	if getenv("SMTP_ENABLED") != "true" {
		logger.Info("notify: channel disabled", slog.String("channel", "email"))
		return nil
	}
	cfg := SMTPConfig{
		Host:     getenv("SMTP_HOST"),
		Port:     587,
		Username: getenv("SMTP_USERNAME"),
		Password: getenv("SMTP_PASSWORD"),
		From:     getenv("SMTP_FROM"),
		Timeout:  30 * time.Second,
	}
	if v := getenv("SMTP_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port <= 0 || port > 65535 {
			logger.Warn("notify: invalid SMTP_PORT, disabling email", slog.String("value", v))
			return nil
		}
		cfg.Port = port
	}
	if cfg.From == "" {
		cfg.From = cfg.Username
	}
	if cfg.Host == "" || cfg.From == "" {
		logger.Warn("notify: SMTP_HOST / SMTP_FROM missing, disabling email")
		return nil
	}
	logger.Info("notify: channel enabled", slog.String("channel", "email"),
		slog.String("host", cfg.Host), slog.Int("port", cfg.Port))
	return NewSMTPMailer(cfg)
}
