package tts_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/tts"
)

// buildWAV assembles a minimal PCM WAV: sampleRate Hz, 16-bit mono, with
// dataBytes bytes of audio payload.
func buildWAV(t *testing.T, sampleRate uint32, dataBytes int) []byte {
	t.Helper()
	byteRate := sampleRate * 2 // mono, 16-bit

	var b []byte
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(36+dataBytes))
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1) // PCM
	b = binary.LittleEndian.AppendUint16(b, 1) // mono
	b = binary.LittleEndian.AppendUint32(b, sampleRate)
	b = binary.LittleEndian.AppendUint32(b, byteRate)
	b = binary.LittleEndian.AppendUint16(b, 2)  // block align
	b = binary.LittleEndian.AppendUint16(b, 16) // bits per sample
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(dataBytes))
	b = append(b, make([]byte, dataBytes)...)
	return b
}

func TestWavDuration(t *testing.T) {
	t.Run("one second of 24kHz mono 16-bit", func(t *testing.T) {
		wav := buildWAV(t, 24000, 48000)
		d, err := tts.WavDuration(wav)
		require.NoError(t, err)
		assert.Equal(t, time.Second, d)
	})

	t.Run("half second", func(t *testing.T) {
		wav := buildWAV(t, 24000, 24000)
		d, err := tts.WavDuration(wav)
		require.NoError(t, err)
		assert.Equal(t, 500*time.Millisecond, d)
	})

	t.Run("not a wav", func(t *testing.T) {
		_, err := tts.WavDuration([]byte("ID3mp3data........"))
		assert.Error(t, err)
	})

	t.Run("truncated header", func(t *testing.T) {
		_, err := tts.WavDuration([]byte("RIFF"))
		assert.Error(t, err)
	})

	t.Run("missing data chunk", func(t *testing.T) {
		wav := buildWAV(t, 24000, 100)
		_, err := tts.WavDuration(wav[:36]) // ends after fmt, before the data chunk header
		assert.Error(t, err)
	})

	t.Run("zero byte rate", func(t *testing.T) {
		wav := buildWAV(t, 0, 100)
		_, err := tts.WavDuration(wav)
		assert.Error(t, err)
	})
}
