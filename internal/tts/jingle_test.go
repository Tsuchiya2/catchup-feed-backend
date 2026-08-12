package tts_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/tts"
)

// fakeFFmpeg stands in for the ffmpeg binary: it records each command line
// and writes a wav back at the output path, so DecodeJingles' format check
// and duration measurement read a real file instead of being short-circuited.
type fakeFFmpeg struct {
	// format of the wav written back. Set it different from the requested
	// format to exercise the §12-5 mismatch rejection.
	format tts.WavFormat
	// durations written per call, in order; the last one repeats.
	durations []time.Duration
	// err fails every invocation (ffmpeg 不在・mp3 破損の代表).
	err error
	// skipWrite runs "successfully" without producing the output file.
	skipWrite bool

	calls [][]string
}

func (f *fakeFFmpeg) run(_ context.Context, _ string, args ...string) error {
	i := len(f.calls)
	f.calls = append(f.calls, args)
	if f.err != nil {
		return f.err
	}
	if f.skipWrite {
		return nil
	}
	d := f.durations[len(f.durations)-1]
	if i < len(f.durations) {
		d = f.durations[i]
	}
	wav, err := tts.SilenceWav(f.format, d)
	if err != nil {
		return err
	}
	return os.WriteFile(args[len(args)-1], wav, 0o600)
}

// voicevoxFormat is what the engine emits today (mono 24 kHz 16-bit).
func voicevoxFormat() tts.WavFormat {
	return tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 16}
}

// TestFFmpeg_DecodeJingles_CommandAssembly pins that the decode targets
// exactly the format handed in — the whole point of §12-5: a jingle that
// does not match the engine's own output breaks the concat demuxer.
func TestFFmpeg_DecodeJingles_CommandAssembly(t *testing.T) {
	tests := []struct {
		name      string
		format    tts.WavFormat
		wantCodec string
	}{
		{"VOICEVOX mono 24kHz 16-bit", voicevoxFormat(), "pcm_s16le"},
		{"stereo 44.1kHz 16-bit", tts.WavFormat{AudioFormat: 1, Channels: 2, SampleRate: 44100, BitsPerSample: 16}, "pcm_s16le"},
		{"8-bit is unsigned", tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 8000, BitsPerSample: 8}, "pcm_u8"},
		{"24-bit", tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 48000, BitsPerSample: 24}, "pcm_s24le"},
		{"32-bit", tts.WavFormat{AudioFormat: 1, Channels: 2, SampleRate: 48000, BitsPerSample: 32}, "pcm_s32le"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := &fakeFFmpeg{
				format:    tt.format,
				durations: []time.Duration{10 * time.Second, 12 * time.Second},
			}
			f := &tts.FFmpeg{Path: "/opt/homebrew/bin/ffmpeg", Run: fake.run}

			jingles, err := f.DecodeJingles(context.Background(), dir, tt.format)
			require.NoError(t, err)

			require.Len(t, fake.calls, 2, "opening と ending で1回ずつ")
			for _, args := range fake.calls {
				assert.Equal(t, strconv.Itoa(tt.format.Channels), argValue(t, args, "-ac"))
				assert.Equal(t, strconv.Itoa(tt.format.SampleRate), argValue(t, args, "-ar"))
				assert.Equal(t, tt.wantCodec, argValue(t, args, "-c:a"))
				assert.Equal(t, "wav", argValue(t, args, "-f"))
				assert.Contains(t, args, "-vn",
					"mp3 の埋め込みアートワークは映像ストリームとして落とす")
				assert.Contains(t, args, "-map_metadata")
				assert.Contains(t, args, "-y")
			}

			// 出力はテンポラリディレクトリ内 (§6-6)、引数の最後。
			assert.Equal(t, filepath.Join(dir, "jingle_opening.wav"), jingles.Opening.Path)
			assert.Equal(t, filepath.Join(dir, "jingle_ending.wav"), jingles.Ending.Path)
			assert.Equal(t, jingles.Opening.Path, fake.calls[0][len(fake.calls[0])-1])
			assert.Equal(t, jingles.Ending.Path, fake.calls[1][len(fake.calls[1])-1])

			// 尺は「デコード後の wav の実測値」— duration_sec の元になる。
			assert.Equal(t, 10*time.Second, jingles.Opening.Duration)
			assert.Equal(t, 12*time.Second, jingles.Ending.Duration)
			assert.Equal(t, 22*time.Second, jingles.Duration())
		})
	}
}

// TestFFmpeg_DecodeJingles_EmbeddedSources pins that the embedded mp3s are
// real files (a zero-byte asset still compiles) and that ffmpeg is pointed
// at the copies written into the run's temp dir.
func TestFFmpeg_DecodeJingles_EmbeddedSources(t *testing.T) {
	dir := t.TempDir()
	format := voicevoxFormat()
	fake := &fakeFFmpeg{format: format, durations: []time.Duration{time.Second}}
	f := &tts.FFmpeg{Run: fake.run}

	_, err := f.DecodeJingles(context.Background(), dir, format)
	require.NoError(t, err)

	for i, name := range []string{"opening", "ending"} {
		srcPath := filepath.Join(dir, "jingle_"+name+".mp3")
		assert.Equal(t, srcPath, argValue(t, fake.calls[i], "-i"))
		src, err := os.ReadFile(srcPath) // #nosec G304 -- test temp dir
		require.NoError(t, err)
		assert.Greater(t, len(src), 1024,
			"embedded %s.mp3 must carry actual audio", name)
	}
}

// TestFFmpeg_DecodeJingles_Errors: every failure surfaces as an error so the
// radio pipeline can degrade to a jingle-less episode (§8). None of these is
// allowed to yield a silently wrong wav.
func TestFFmpeg_DecodeJingles_Errors(t *testing.T) {
	tests := []struct {
		name    string
		format  tts.WavFormat
		fake    *fakeFFmpeg
		wantSub string
		wantRun bool // ffmpeg was invoked at all
	}{
		{
			name:    "ffmpeg failure (不在・破損)",
			format:  voicevoxFormat(),
			fake:    &fakeFFmpeg{err: errors.New("exit status 1: No such file or directory")},
			wantSub: "No such file or directory",
			wantRun: true,
		},
		{
			name:    "non-PCM target format",
			format:  tts.WavFormat{AudioFormat: 3, Channels: 1, SampleRate: 24000, BitsPerSample: 32},
			fake:    &fakeFFmpeg{},
			wantSub: "non-PCM target format tag 3",
		},
		{
			name:    "unreadable engine format (zero value)",
			format:  tts.WavFormat{},
			fake:    &fakeFFmpeg{},
			wantSub: "non-PCM target format tag 0",
		},
		{
			name:    "unsupported sample width",
			format:  tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 12},
			fake:    &fakeFFmpeg{},
			wantSub: "unsupported sample width 12",
		},
		{
			name:    "zero channels",
			format:  tts.WavFormat{AudioFormat: 1, Channels: 0, SampleRate: 24000, BitsPerSample: 16},
			fake:    &fakeFFmpeg{},
			wantSub: "invalid target format",
		},
		{
			name:   "decoded format mismatch is rejected (§12-5)",
			format: voicevoxFormat(),
			fake: &fakeFFmpeg{
				format:    tts.WavFormat{AudioFormat: 1, Channels: 2, SampleRate: 44100, BitsPerSample: 16},
				durations: []time.Duration{time.Second},
			},
			wantSub: "decoded as",
			wantRun: true,
		},
		{
			name:    "output never produced",
			format:  voicevoxFormat(),
			fake:    &fakeFFmpeg{skipWrite: true},
			wantSub: "read decoded jingle",
			wantRun: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &tts.FFmpeg{Path: "ffmpeg", Run: tt.fake.run}

			jingles, err := f.DecodeJingles(context.Background(), t.TempDir(), tt.format)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
			assert.Zero(t, jingles, "部分的に成功した Jingles を返さない")
			if !tt.wantRun {
				assert.Empty(t, tt.fake.calls,
					"フォーマット検証は ffmpeg 起動より前に落ちる")
			}
		})
	}
}

// TestJingles_NilIsNoOp pins the degraded path's contract: callers hold a
// *Jingles and never branch on it — a nil pair leaves the concat list and
// the recorded duration exactly as they were (§8).
func TestJingles_NilIsNoOp(t *testing.T) {
	wavs := []string{"/tmp/a.wav", "/tmp/b.wav"}

	var absent *tts.Jingles
	assert.Equal(t, wavs, absent.Wrap(wavs))
	assert.Equal(t, time.Duration(0), absent.Duration())

	present := &tts.Jingles{
		Opening: tts.Jingle{Path: "/tmp/open.wav", Duration: 10 * time.Second},
		Ending:  tts.Jingle{Path: "/tmp/end.wav", Duration: 12 * time.Second},
	}
	assert.Equal(t,
		[]string{"/tmp/open.wav", "/tmp/a.wav", "/tmp/b.wav", "/tmp/end.wav"},
		present.Wrap(wavs))
	assert.Equal(t, 22*time.Second, present.Duration())
	assert.Equal(t, []string{"/tmp/a.wav", "/tmp/b.wav"}, wavs, "入力を破壊しない")
}
