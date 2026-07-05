package tts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pkgconfig "catchup-feed/pkg/config"
)

// RunFunc executes an external command. It exists so tests can assert the
// assembled command line without running ffmpeg.
type RunFunc func(ctx context.Context, name string, args ...string) error

// execRun is the real runner: on failure the combined output tail is folded
// into the error so ffmpeg diagnostics reach the slog line.
func execRun(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		const maxTail = 1024
		tail := string(out)
		if len(tail) > maxTail {
			tail = tail[len(tail)-maxTail:]
		}
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(tail))
	}
	return nil
}

// ID3 carries the tags written to the episode mp3 (§6-4: ID3 タグ付与).
type ID3 struct {
	Title  string
	Artist string
	Album  string
	Date   string // YYYY-MM-DD
}

// FFmpeg combines sentence WAVs into the final episode mp3 (§6-4):
// concat demuxer -> loudnorm -> 64 kbps mono 44.1 kHz mp3 with ID3 tags.
type FFmpeg struct {
	// Path is the ffmpeg binary (FFMPEG_PATH, default "ffmpeg" on PATH —
	// Mac 側は brew の ffmpeg 前提).
	Path string
	// Run executes the command; nil means the real exec runner.
	Run RunFunc
}

// NewFFmpeg reads FFMPEG_PATH and returns a combiner using the real runner.
func NewFFmpeg() *FFmpeg {
	return &FFmpeg{Path: pkgconfig.GetEnvString("FFMPEG_PATH", "ffmpeg")}
}

// ConcatToMP3 concatenates the WAV files (all VOICEVOX output, identical
// format) into outPath. The concat list file is written next to outPath,
// which the radio batch keeps inside its temp dir (§6-6: 生成途中の失敗は
// テンポラリディレクトリ内で完結).
func (f *FFmpeg) ConcatToMP3(ctx context.Context, wavPaths []string, outPath string, tags ID3) error {
	if len(wavPaths) == 0 {
		return fmt.Errorf("ffmpeg: no input files")
	}

	listPath := filepath.Join(filepath.Dir(outPath), "concat.txt")
	if err := os.WriteFile(listPath, []byte(concatList(wavPaths)), 0o600); err != nil {
		return fmt.Errorf("ffmpeg: write concat list: %w", err)
	}

	args := []string{
		"-hide_banner", "-nostdin", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-af", "loudnorm",
		"-ac", "1", "-ar", "44100",
		"-c:a", "libmp3lame", "-b:a", "64k",
		"-id3v2_version", "3",
		"-metadata", "title=" + tags.Title,
		"-metadata", "artist=" + tags.Artist,
		"-metadata", "album=" + tags.Album,
		"-metadata", "date=" + tags.Date,
		outPath,
	}

	run := f.Run
	if run == nil {
		run = execRun
	}
	path := f.Path
	if path == "" {
		path = "ffmpeg"
	}
	if err := run(ctx, path, args...); err != nil {
		return fmt.Errorf("ffmpeg: encode %s: %w", filepath.Base(outPath), err)
	}
	return nil
}

// concatList renders the ffmpeg concat demuxer input. Paths are wrapped in
// single quotes with embedded single quotes escaped.
func concatList(paths []string) string {
	var sb strings.Builder
	for _, p := range paths {
		sb.WriteString("file '")
		sb.WriteString(strings.ReplaceAll(p, "'", `'\''`))
		sb.WriteString("'\n")
	}
	return sb.String()
}
