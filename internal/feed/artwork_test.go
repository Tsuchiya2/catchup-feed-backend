package feed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArtworkAsset_MeetsPodcastRequirements pins the embedded asset
// against the podcast-app cover-art rules: a square JPEG of at least
// 1400x1400 (Apple Podcasts' minimum). Oversized/undersized art is simply
// ignored by the apps rather than reported as an error, so the check has
// to live in CI.
//
// What it deliberately does NOT check is composition. A square canvas
// whose artwork is letterboxed inside black bars satisfies every
// automated rule while wasting a third of the thumbnail — exactly the
// 2026-08-12 defect this asset was re-cropped to fix. Judging that stays
// a human review step; only dimensions and format are machine-checkable.
func TestArtworkAsset_MeetsPodcastRequirements(t *testing.T) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(artworkJPEG))
	require.NoError(t, err, "the embedded artwork must be a decodable image")

	assert.Equal(t, "jpeg", format, "the RSS enclosure type and Content-Type both claim JPEG")
	assert.Equal(t, cfg.Height, cfg.Width, "cover art must be square")
	assert.GreaterOrEqual(t, cfg.Width, 1400, "Apple Podcasts requires at least 1400x1400")
	assert.Equal(t, 1536, cfg.Width, "§5.1 で規定した 1536x1536(縮小事故の検知)")
}

// TestArtworkFingerprint_DerivedFromEmbeddedBytes is the regression guard
// for the cache-busting scheme: the fingerprint — and therefore the
// artwork URL in the RSS — must be a function of the embedded bytes. If
// it ever becomes a hardcoded constant, swapping the image stops changing
// the URL and podcast apps keep serving the old logo forever (PR #104).
func TestArtworkFingerprint_DerivedFromEmbeddedBytes(t *testing.T) {
	sum := sha256.Sum256(artworkJPEG)
	assert.Equal(t, hex.EncodeToString(sum[:])[:8], artworkFingerprint)
	assert.Len(t, artworkFingerprint, 8)
	assert.Equal(t, artworkFingerprint+".jpg", artworkFileName())
	assert.Equal(t, `"`+artworkFingerprint+`"`, artworkETag, "ETag は同じダイジェスト由来")

	// 別バイト列は別フィンガープリント = 別 URL。
	assert.NotEqual(t, fingerprint([]byte("old logo")), fingerprint([]byte("new logo")))
}
