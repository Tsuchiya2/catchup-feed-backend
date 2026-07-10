package tts_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/tts"
)

func TestParseWavFormat(t *testing.T) {
	t.Run("reads the fmt chunk", func(t *testing.T) {
		wav := buildWAV(t, 24000, 48000)
		format, err := tts.ParseWavFormat(wav)
		require.NoError(t, err)
		assert.Equal(t, tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 16}, format)
	})

	t.Run("not a wav", func(t *testing.T) {
		_, err := tts.ParseWavFormat([]byte("ID3mp3data........"))
		assert.Error(t, err)
	})

	t.Run("truncated fmt chunk", func(t *testing.T) {
		wav := buildWAV(t, 24000, 100)
		_, err := tts.ParseWavFormat(wav[:20]) // header + fmt id/size only
		assert.Error(t, err)
	})
}

// TestSilenceWav pins the §12-5 contract: the silence file must be a valid
// RIFF/WAVE in exactly the source format, with the requested playing time —
// verified through the same parsers the pipeline uses (ParseWavFormat /
// WavDuration round trip).
func TestSilenceWav(t *testing.T) {
	voicevoxLike := tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 16}

	t.Run("3 seconds round-trips through the wav parsers", func(t *testing.T) {
		wav, err := tts.SilenceWav(voicevoxLike, 3*time.Second)
		require.NoError(t, err)

		format, err := tts.ParseWavFormat(wav)
		require.NoError(t, err)
		assert.Equal(t, voicevoxLike, format, "silence must carry the source format verbatim")

		d, err := tts.WavDuration(wav)
		require.NoError(t, err)
		assert.Equal(t, 3*time.Second, d)
	})

	t.Run("stereo 44.1kHz keeps block alignment", func(t *testing.T) {
		format := tts.WavFormat{AudioFormat: 1, Channels: 2, SampleRate: 44100, BitsPerSample: 16}
		wav, err := tts.SilenceWav(format, 500*time.Millisecond)
		require.NoError(t, err)

		d, err := tts.WavDuration(wav)
		require.NoError(t, err)
		assert.Equal(t, 500*time.Millisecond, d)
	})

	t.Run("payload is all zero samples", func(t *testing.T) {
		wav, err := tts.SilenceWav(voicevoxLike, 10*time.Millisecond)
		require.NoError(t, err)
		for _, b := range wav[44:] {
			require.Zero(t, b)
		}
	})

	tests := []struct {
		name   string
		format tts.WavFormat
		d      time.Duration
	}{
		{"non-PCM format refused", tts.WavFormat{AudioFormat: 3, Channels: 1, SampleRate: 24000, BitsPerSample: 32}, time.Second},
		{"zero channels refused", tts.WavFormat{AudioFormat: 1, SampleRate: 24000, BitsPerSample: 16}, time.Second},
		{"zero sample rate refused", tts.WavFormat{AudioFormat: 1, Channels: 1, BitsPerSample: 16}, time.Second},
		{"non-byte-aligned bit depth refused", tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 12}, time.Second},
		{"non-positive duration refused", tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 16}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tts.SilenceWav(tt.format, tt.d)
			assert.Error(t, err)
		})
	}
}
