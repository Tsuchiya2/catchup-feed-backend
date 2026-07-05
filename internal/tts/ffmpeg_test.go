package tts_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/tts"
)

// capturedRun records the command line instead of executing ffmpeg.
type capturedRun struct {
	name string
	args []string
	err  error
}

func (c *capturedRun) run(_ context.Context, name string, args ...string) error {
	c.name = name
	c.args = args
	return c.err
}

func argValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("flag %s not found in %v", flag, args)
	return ""
}

func TestFFmpeg_ConcatToMP3_CommandAssembly(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "2026-07-05.mp3")
	wavs := []string{
		filepath.Join(dir, "seg_001.wav"),
		filepath.Join(dir, "seg_002.wav"),
	}

	captured := &capturedRun{}
	f := &tts.FFmpeg{Path: "/opt/homebrew/bin/ffmpeg", Run: captured.run}

	tags := tts.ID3{Title: "pulse 2026-07-05", Artist: "pulse", Album: "pulse", Date: "2026-07-05"}
	require.NoError(t, f.ConcatToMP3(context.Background(), wavs, out, tags))

	assert.Equal(t, "/opt/homebrew/bin/ffmpeg", captured.name)
	args := captured.args

	// concat demuxer input
	assert.Contains(t, args, "-f")
	listPath := argValue(t, args, "-i")
	assert.Equal(t, filepath.Join(dir, "concat.txt"), listPath,
		"concat list lives next to the output, inside the temp dir (§6-6)")

	// §6-4: loudnorm -> 64kbps mono 44.1kHz mp3
	assert.Equal(t, "loudnorm", argValue(t, args, "-af"))
	assert.Equal(t, "1", argValue(t, args, "-ac"))
	assert.Equal(t, "44100", argValue(t, args, "-ar"))
	assert.Equal(t, "64k", argValue(t, args, "-b:a"))
	assert.Equal(t, "libmp3lame", argValue(t, args, "-c:a"))

	// ID3 tags
	assert.Contains(t, args, "-id3v2_version")
	assert.Contains(t, args, "title=pulse 2026-07-05")
	assert.Contains(t, args, "artist=pulse")
	assert.Contains(t, args, "date=2026-07-05")

	// output is the final argument
	assert.Equal(t, out, args[len(args)-1])

	// the list file references every wav in order
	content, err := os.ReadFile(listPath)
	require.NoError(t, err)
	assert.Equal(t,
		"file '"+wavs[0]+"'\n"+"file '"+wavs[1]+"'\n",
		string(content))
}

func TestFFmpeg_ConcatToMP3_EscapesQuotes(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.mp3")

	captured := &capturedRun{}
	f := &tts.FFmpeg{Path: "ffmpeg", Run: captured.run}

	require.NoError(t, f.ConcatToMP3(context.Background(),
		[]string{dir + "/it's.wav"}, out, tts.ID3{}))

	content, err := os.ReadFile(filepath.Join(dir, "concat.txt"))
	require.NoError(t, err)
	assert.Equal(t, "file '"+dir+`/it'\''s.wav'`+"\n", string(content))
}

func TestFFmpeg_ConcatToMP3_Errors(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.mp3")

	t.Run("no inputs", func(t *testing.T) {
		f := &tts.FFmpeg{Path: "ffmpeg", Run: (&capturedRun{}).run}
		assert.Error(t, f.ConcatToMP3(context.Background(), nil, out, tts.ID3{}))
	})

	t.Run("runner failure propagates", func(t *testing.T) {
		captured := &capturedRun{err: errors.New("exit status 1: unknown encoder")}
		f := &tts.FFmpeg{Path: "ffmpeg", Run: captured.run}
		err := f.ConcatToMP3(context.Background(), []string{dir + "/a.wav"}, out, tts.ID3{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown encoder")
	})
}
