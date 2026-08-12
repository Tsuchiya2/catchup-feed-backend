package feed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArtworkAsset_MeetsPodcastRequirements pins the embedded asset
// against the cover-art rules recorded in D-34 (親リポジトリの
// docs/podcast-artwork.md — this repository does not carry that file):
// a square, non-CMYK JPEG between 1400x1400 and 3000x3000. Art that
// violates them is simply ignored by the apps rather than reported as an
// error, so the check has to live in CI.
//
// The bounds are deliberately a range, not the current 1536x1536: moving
// to another size inside Apple's window is a legitimate change that this
// test must not veto.
//
// What it deliberately does NOT check is composition. A square canvas
// whose artwork is letterboxed inside black bars satisfies every
// automated rule while wasting a third of the thumbnail — exactly the
// 2026-08-12 defect this asset was re-cropped to fix (D-34). Judging that
// stays a human review step; only dimensions, format and colour space are
// machine-checkable.
func TestArtworkAsset_MeetsPodcastRequirements(t *testing.T) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(artworkJPEG))
	require.NoError(t, err, "the embedded artwork must be a decodable image")

	assert.Equal(t, "jpeg", format, "the RSS image tags and Content-Type both claim JPEG")
	assert.Equal(t, cfg.Width, cfg.Height, "cover art must be square")
	assert.GreaterOrEqual(t, cfg.Width, 1400, "Apple Podcasts requires at least 1400x1400")
	assert.LessOrEqual(t, cfg.Width, 3000, "Apple Podcasts caps cover art at 3000x3000")

	// 色空間の要件: CMYK の JPEG は Apple に弾かれる。グレースケールも
	// ロゴとしては事故なので、許可リスト方式で両方を除外する。
	//
	// image/jpeg の DecodeConfig が返す ColorModel の実測(2026-08-12):
	//
	//	baseline / progressive / 4:4:4 / 4:2:0  → color.YCbCrModel
	//	Pillow keep_rgb=True(真の RGB JPEG)   → color.RGBAModel
	//	グレースケール                          → color.GrayModel
	//	CMYK                                    → color.CMYKModel
	//
	// RGBA が現れるのは、Adobe APP14 transform=0 か成分 ID が 'R','G','B'
	// のとき Go が isRGB() 経路に入るため。これも Apple が受け入れる正当な
	// RGB JPEG なので許可する — 等値で YCbCr に固定すると、docs の Pillow
	// レシピに keep_rgb=True を足しただけで誤って落ちる。
	assert.Contains(t, []color.Model{color.YCbCrModel, color.RGBAModel}, cfg.ColorModel,
		"cover art must be RGB — CMYK is rejected by Apple Podcasts, greyscale is a mistake")
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
	assert.Equal(t, artworkFingerprint+".jpg", artworkFileName)
	assert.Equal(t, `"`+artworkFingerprint+`"`, artworkETag, "ETag は同じダイジェスト由来")

	// 別バイト列は別フィンガープリント = 別 URL。
	assert.NotEqual(t, fingerprint([]byte("old logo")), fingerprint([]byte("new logo")))
}
