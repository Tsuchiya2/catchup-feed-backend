// Package feed serves the podcast feeds (§5): RSS 2.0 generation, the
// URL-embedded token verification middleware (C-6, D-5) and Range-capable
// mp3 delivery via http.ServeContent (C-10).
//
// Two flavours exist (C-5):
//   - public: /feeds/{token}/... behind Cloudflare Tunnel, token verified
//     against feed_tokens on every request;
//   - private: /private/... on a separate tailnet-bound listener, no
//     authentication, serving episodes of every feed kind.
package feed

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Defaults for the feed configuration. The public base URL is the D-6
// decision (radio.catchup-feed.com); everything else is a right-sized
// single-user default overridable via environment variables.
const (
	DefaultPublicBaseURL      = "https://radio.catchup-feed.com"
	DefaultAudioDir           = "episodes"
	DefaultChannelTitle       = "catchup-feed radio"
	DefaultChannelDescription = "毎朝の技術ニュースラジオ"
	DefaultMaxItems           = 30
)

// Config holds the feed delivery settings, loaded from environment
// variables (ffmpeg/rsync paths live in the radio batch config, not here).
type Config struct {
	// PublicBaseURL is the origin used to build public feed and enclosure
	// URLs, e.g. https://radio.catchup-feed.com (D-6). Enclosure URLs are
	// generated under the same token path (C-9).
	PublicBaseURL string
	// PrivateBaseURL is the origin used to build private enclosure URLs.
	// Empty means "derive from the request Host header", which is the
	// right default on a tailnet listener.
	PrivateBaseURL string
	// AudioDir is the base directory holding episode mp3 files. Audio
	// paths from the DB are only served when they resolve inside this
	// directory (path traversal guard).
	AudioDir string
	// ChannelTitle / ChannelDescription describe the RSS channel.
	ChannelTitle       string
	ChannelDescription string
	// MaxItems caps the number of items in a generated feed.
	MaxItems int
	// PrivateAddr is the tailnet listen address for the private feed
	// (§3.1), e.g. "100.64.0.1:8081". Empty disables the private listener.
	PrivateAddr string
}

// LoadConfig reads the feed configuration from the environment, applying
// defaults for anything unset.
func LoadConfig() Config {
	cfg := Config{
		PublicBaseURL:      envOr("FEED_PUBLIC_BASE_URL", DefaultPublicBaseURL),
		PrivateBaseURL:     os.Getenv("FEED_PRIVATE_BASE_URL"),
		AudioDir:           envOr("FEED_AUDIO_DIR", DefaultAudioDir),
		ChannelTitle:       envOr("FEED_CHANNEL_TITLE", DefaultChannelTitle),
		ChannelDescription: envOr("FEED_CHANNEL_DESCRIPTION", DefaultChannelDescription),
		MaxItems:           DefaultMaxItems,
		PrivateAddr:        os.Getenv("PRIVATE_FEED_ADDR"),
	}
	if v := os.Getenv("FEED_MAX_ITEMS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxItems = n
		}
	}
	cfg.PublicBaseURL = strings.TrimSuffix(cfg.PublicBaseURL, "/")
	cfg.PrivateBaseURL = strings.TrimSuffix(cfg.PrivateBaseURL, "/")
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ValidatePrivateAddr rejects listen addresses that would expose the
// unauthenticated private feed beyond the tailnet. C-5 relies on the
// physical boundary as the only authentication, so a wildcard bind
// (":8081", "0.0.0.0:8081", "[::]:8081") would collapse it: every host on
// the LAN could read private episodes. The address must name a concrete
// host — in production the machine's Tailscale IP or MagicDNS name.
func ValidatePrivateAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("PRIVATE_FEED_ADDR %q is not a host:port address: %w", addr, err)
	}
	if host == "" {
		return errors.New(`PRIVATE_FEED_ADDR must name a concrete host (e.g. the tailnet IP); a bare ":port" binds all interfaces and would expose the unauthenticated private feed beyond the tailnet (C-5)`)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return fmt.Errorf("PRIVATE_FEED_ADDR host %q is a wildcard address; bind the tailnet IP explicitly so the private feed never leaves the tailnet (C-5)", host)
	}
	return nil
}
