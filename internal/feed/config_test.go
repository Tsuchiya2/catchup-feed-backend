package feed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_Defaults(t *testing.T) {
	for _, key := range []string{
		"FEED_PUBLIC_BASE_URL", "FEED_PRIVATE_BASE_URL", "FEED_AUDIO_DIR",
		"FEED_CHANNEL_TITLE", "FEED_CHANNEL_DESCRIPTION", "FEED_MAX_ITEMS",
		"PRIVATE_FEED_ADDR",
	} {
		t.Setenv(key, "")
	}

	cfg := LoadConfig()

	assert.Equal(t, DefaultPublicBaseURL, cfg.PublicBaseURL)
	assert.Empty(t, cfg.PrivateBaseURL)
	assert.Equal(t, DefaultAudioDir, cfg.AudioDir)
	assert.Equal(t, DefaultChannelTitle, cfg.ChannelTitle)
	assert.Equal(t, DefaultMaxItems, cfg.MaxItems)
	assert.Empty(t, cfg.PrivateAddr, "private listener stays disabled unless configured")
}

func TestLoadConfig_Overrides(t *testing.T) {
	t.Setenv("FEED_PUBLIC_BASE_URL", "https://radio.example.com/")
	t.Setenv("FEED_PRIVATE_BASE_URL", "http://pi.tailnet:8081/")
	t.Setenv("FEED_AUDIO_DIR", "/data/episodes")
	t.Setenv("FEED_CHANNEL_TITLE", "my show")
	t.Setenv("FEED_CHANNEL_DESCRIPTION", "desc")
	t.Setenv("FEED_MAX_ITEMS", "5")
	t.Setenv("PRIVATE_FEED_ADDR", "100.64.0.1:8081")

	cfg := LoadConfig()

	assert.Equal(t, "https://radio.example.com", cfg.PublicBaseURL, "trailing slash is trimmed")
	assert.Equal(t, "http://pi.tailnet:8081", cfg.PrivateBaseURL)
	assert.Equal(t, "/data/episodes", cfg.AudioDir)
	assert.Equal(t, "my show", cfg.ChannelTitle)
	assert.Equal(t, "desc", cfg.ChannelDescription)
	assert.Equal(t, 5, cfg.MaxItems)
	assert.Equal(t, "100.64.0.1:8081", cfg.PrivateAddr)
}

func TestLoadConfig_InvalidMaxItemsFallsBack(t *testing.T) {
	t.Setenv("FEED_MAX_ITEMS", "not-a-number")
	assert.Equal(t, DefaultMaxItems, LoadConfig().MaxItems)

	t.Setenv("FEED_MAX_ITEMS", "0")
	assert.Equal(t, DefaultMaxItems, LoadConfig().MaxItems)
}

func TestValidatePrivateAddr(t *testing.T) {
	// C-5 のピン留め: 無認証の私的フィードはワイルドカードバインドで
	// tailnet の外(LAN 全体)に露出してはならない。
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"bare port binds all interfaces", ":8081", true},
		{"IPv4 wildcard", "0.0.0.0:8081", true},
		{"IPv6 wildcard", "[::]:8081", true},
		{"tailnet IPv4 is fine", "100.64.0.1:8081", false},
		{"tailnet MagicDNS name is fine", "pi.tailnet:8081", false},
		{"loopback is fine", "127.0.0.1:8081", false},
		{"missing port is rejected", "100.64.0.1", true},
		{"port only without colon is rejected", "8081", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePrivateAddr(tt.addr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
