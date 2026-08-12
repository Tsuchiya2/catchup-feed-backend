package tts_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
// real files (a zero-byte asset still compiles), that ffmpeg is pointed at
// the copies written into the run's temp dir, and that the two embeds are
// distinct — swapping the //go:embed lines would otherwise pass every test
// while putting the ending on the front of every episode.
func TestFFmpeg_DecodeJingles_EmbeddedSources(t *testing.T) {
	dir := t.TempDir()
	format := voicevoxFormat()
	fake := &fakeFFmpeg{format: format, durations: []time.Duration{time.Second}}
	f := &tts.FFmpeg{Run: fake.run}

	_, err := f.DecodeJingles(context.Background(), dir, format)
	require.NoError(t, err)

	sources := make([][]byte, 2)
	for i, name := range []string{"opening", "ending"} {
		srcPath := filepath.Join(dir, "jingle_"+name+".mp3")
		assert.Equal(t, srcPath, argValue(t, fake.calls[i], "-i"))
		src, err := os.ReadFile(srcPath) // #nosec G304 -- test temp dir
		require.NoError(t, err)
		assert.Greater(t, len(src), 1024,
			"embedded %s.mp3 must carry actual audio", name)
		sources[i] = src
	}
	assert.NotEqual(t, sources[0], sources[1],
		"opening と ending が同じバイト列 = //go:embed の取り違え")
}

// TestFFmpeg_DecodeJingles_RealFFmpeg runs the actual binary over the actual
// embedded assets. The fake writes back SilenceWav's canonical 44-byte
// header, so it can never catch what real ffmpeg does: its wav muxer emits
// its own LIST/INFO chunk ahead of the data, and both ParseWavFormat and
// WavDuration have to walk past it to reach fmt/data. This is also the only
// check that the embedded mp3s decode as audio at all, and the only one that
// pins which file is the opening (10s) and which the ending (12s).
func TestFFmpeg_DecodeJingles_RealFFmpeg(t *testing.T) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH (radio runs on the Mac, where it is a hard dependency)")
	}

	dir := t.TempDir()
	format := voicevoxFormat()
	f := &tts.FFmpeg{Path: bin}

	jingles, err := f.DecodeJingles(context.Background(), dir, format)
	require.NoError(t, err)

	tests := []struct {
		name    string
		jingle  tts.Jingle
		wantSec float64
	}{
		{"opening", jingles.Opening, 10},
		{"ending", jingles.Ending, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := os.ReadFile(tt.jingle.Path) // #nosec G304 -- test temp dir
			require.NoError(t, err)

			got, err := tts.ParseWavFormat(decoded)
			require.NoError(t, err)
			assert.Equal(t, format, got,
				"実 ffmpeg 出力もエンジンと同一フォーマットでなければ concat が壊れる (§12-5)")

			assert.InDelta(t, tt.wantSec, tt.jingle.Duration.Seconds(), 0.5,
				"実測尺が想定から外れている = アセットの取り違えか差し替え漏れ")

			// The measured length must be backed by real bytes: header +
			// data at the engine's byte rate. This is what proves the chunk
			// walk found the true data size rather than tripping over the
			// LIST chunk ffmpeg prepends.
			byteRate := format.SampleRate * format.Channels * format.BitsPerSample / 8
			wantData := int(tt.jingle.Duration.Seconds() * float64(byteRate))
			assert.GreaterOrEqual(t, len(decoded), 44+wantData)
		})
	}

	// The point of all of it: real jingle output and engine-format speech go
	// through the concat demuxer together.
	speechWav, err := tts.SilenceWav(format, time.Second)
	require.NoError(t, err)
	speech := filepath.Join(dir, "speech.wav")
	require.NoError(t, os.WriteFile(speech, speechWav, 0o600))

	out := filepath.Join(dir, "episode.mp3")
	require.NoError(t, f.ConcatToMP3(context.Background(),
		jingles.Wrap([]string{speech}), out,
		tts.ID3{Title: "pulse", Artist: "pulse", Album: "pulse", Date: "2026-08-12"}))

	st, err := os.Stat(out)
	require.NoError(t, err)
	assert.Greater(t, st.Size(), int64(0), "ジングル入りの mp3 が実際に生成される")
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
		{
			// exit 0 with an empty data chunk. Accepting it would put a
			// silent "jingle" into the concat list and log nothing, since
			// nothing downstream tells 0 seconds from absent.
			name:   "zero-length decode",
			format: voicevoxFormat(),
			fake: &fakeFFmpeg{
				format:    voicevoxFormat(),
				durations: []time.Duration{time.Nanosecond}, // rounds to zero frames
			},
			wantSub: "decoded to 0s of audio",
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

// TestJingles_WrapKeepsEmptyEmpty pins the guard B-1 restored: wrapping an
// empty concat list must stay empty. Padding it to [opening, ending] would
// slip past ConcatToMP3's "no input files" check — the single structural
// stop between "no programme audio" and a 22-second jingles-only mp3 being
// rsynced to the Pi, INSERTed, notified and published, all with exit 0.
func TestJingles_WrapKeepsEmptyEmpty(t *testing.T) {
	jingles := &tts.Jingles{
		Opening: tts.Jingle{Path: "/tmp/open.wav", Duration: 10 * time.Second},
		Ending:  tts.Jingle{Path: "/tmp/end.wav", Duration: 12 * time.Second},
	}

	tests := []struct {
		name string
		in   []string
	}{
		{"nil", nil},
		{"empty non-nil", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, jingles.Wrap(tt.in), "ジングルだけの番組は作らない")

			// end to end: the guard it protects is still reachable.
			f := &tts.FFmpeg{Path: "ffmpeg", Run: (&fakeFFmpeg{}).run}
			err := f.ConcatToMP3(context.Background(), jingles.Wrap(tt.in),
				filepath.Join(t.TempDir(), "out.mp3"), tts.ID3{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no input files")
		})
	}
}
