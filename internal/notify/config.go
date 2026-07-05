package notify

import (
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// webhookTimeout bounds one webhook call. Generous because Discord episode
// notifications may upload an mp3 of several MB from the Pi.
const webhookTimeout = 60 * time.Second

func getenv(key string) string { return os.Getenv(key) }

// LoadDestinationsFromEnv assembles the admin destinations (D-7:
// 本人=Discord+Slack、宣言的に有効/無効). A channel is active only when its
// *_ENABLED flag is "true" and its webhook URL validates; anything else
// logs a warning and drops the channel — notifications degrade, the worker
// keeps running (§8).
//
// Environment variables:
//   - DISCORD_ENABLED / DISCORD_WEBHOOK_URL
//   - SLACK_ENABLED   / SLACK_WEBHOOK_URL
func LoadDestinationsFromEnv(logger *slog.Logger) []Destination {
	if logger == nil {
		logger = slog.Default()
	}
	var destinations []Destination
	if u, ok := loadWebhook(logger, "discord", "DISCORD_ENABLED", "DISCORD_WEBHOOK_URL", "discord.com", "/api/webhooks/"); ok {
		destinations = append(destinations, NewDiscord(u, webhookTimeout, logger))
	}
	if u, ok := loadWebhook(logger, "slack", "SLACK_ENABLED", "SLACK_WEBHOOK_URL", "hooks.slack.com", "/services/"); ok {
		destinations = append(destinations, NewSlack(u, webhookTimeout))
	}
	return destinations
}

// loadWebhook reads and validates one webhook channel configuration. The
// host/path pinning is carried over from the old worker: a mistyped URL
// must fail closed instead of posting the feed's content elsewhere.
func loadWebhook(logger *slog.Logger, name, enabledKey, urlKey, wantHost, wantPathPrefix string) (string, bool) {
	if getenv(enabledKey) != "true" {
		logger.Info("notify: channel disabled", slog.String("channel", name))
		return "", false
	}
	raw := getenv(urlKey)
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		logger.Warn("notify: invalid webhook URL, disabling channel",
			slog.String("channel", name), slog.Any("error", err))
		return "", false
	}
	if u.Scheme != "https" || u.Host != wantHost || !strings.HasPrefix(u.Path, wantPathPrefix) {
		logger.Warn("notify: webhook URL does not match the expected service, disabling channel",
			slog.String("channel", name), slog.String("host", u.Host), slog.String("path", u.Path))
		return "", false
	}
	logger.Info("notify: channel enabled", slog.String("channel", name))
	return raw, true
}

// LoadSMTPFromEnv builds the friend mailer (C-11). Returns nil when
// SMTP_ENABLED is not "true" or the configuration is incomplete — email
// silently off, podcast delivery unaffected.
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
