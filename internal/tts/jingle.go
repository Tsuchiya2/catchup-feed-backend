package tts

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// openingMP3 and endingMP3 are the program's jingles (§6-4), embedded into
// the radio binary for the same reason the channel artwork is embedded into
// the server binary (internal/feed/artwork.go): the "asset went missing on
// disk" failure mode simply does not exist. Replacing a jingle is a file
// swap plus a rebuild — no runtime configuration, no path environment
// variable (single-user right-sizing).
//
//go:embed assets/opening.mp3
var openingMP3 []byte

//go:embed assets/ending.mp3
var endingMP3 []byte

// Jingle is one decoded jingle: the wav path handed to the concat demuxer
// and its measured playing time. The duration is measured from the decoded
// file (data chunk / byte rate), never assumed from the source mp3 — it
// feeds episodes.duration_sec, which becomes the RSS <itunes:duration>, and
// that value has to describe the mp3 that actually ships.
type Jingle struct {
	Path     string
	Duration time.Duration
}

// Jingles is one run's opening/ending pair, decoded once and shared by every
// episode the run produces.
//
// A nil *Jingles means the decode degraded (§8): every method below is a
// no-op, so the episode ships jingle-less and its recorded duration counts
// only the audio that exists. Callers therefore never branch on failure.
type Jingles struct {
	Opening Jingle
	Ending  Jingle
}

// Wrap returns the concat list with the opening jingle in front and the
// ending jingle at the back (§6-4). A nil receiver returns wavs unchanged.
//
// An EMPTY input is returned empty — deliberately not wrapped. ConcatToMP3's
// "no input files" check is the only structural guard stopping an episode
// that carries no programme audio at all, and padding a zero-length list up
// to two entries would silently walk past it: a 22-second jingles-only mp3
// would then be rsynced to the Pi, INSERTed into episodes, enqueued for
// notify_episode and published on the public feed, with exit 0 and no error
// to detect it by. Jingles decorate a programme; they are not one.
func (j *Jingles) Wrap(wavs []string) []string {
	if j == nil || len(wavs) == 0 {
		return wavs
	}
	out := make([]string, 0, len(wavs)+2)
	out = append(out, j.Opening.Path)
	out = append(out, wavs...)
	out = append(out, j.Ending.Path)
	return out
}

// Duration is the playing time the jingles add to the episode. A nil
// receiver adds nothing — exactly what a degraded run must record.
func (j *Jingles) Duration() time.Duration {
	if j == nil {
		return 0
	}
	return j.Opening.Duration + j.Ending.Duration
}

// DecodeJingles writes the embedded mp3s into dir and decodes them to wav in
// exactly the given format, returning both paths and their measured lengths.
//
// format comes from an actual VOICEVOX output of this run (see
// Pipeline.synthesize), not from configuration: a concat list mixing sample
// rates, channel counts or sample widths is broken audio, and deriving the
// jingle format from a real engine output makes the match structural rather
// than configured — the same reasoning SilenceWav follows (§12-5).
//
// Any failure is returned as an error and the caller degrades to a
// jingle-less episode (§8); nothing here is worth losing a broadcast over.
func (f *FFmpeg) DecodeJingles(ctx context.Context, dir string, format WavFormat) (Jingles, error) {
	opening, err := f.decodeJingle(ctx, dir, "opening", openingMP3, format)
	if err != nil {
		return Jingles{}, err
	}
	ending, err := f.decodeJingle(ctx, dir, "ending", endingMP3, format)
	if err != nil {
		return Jingles{}, err
	}
	return Jingles{Opening: opening, Ending: ending}, nil
}

// decodeJingle materializes one embedded mp3 in dir and transcodes it.
// Both files stay inside the batch's temp dir (§6-6).
func (f *FFmpeg) decodeJingle(ctx context.Context, dir, name string, mp3 []byte, format WavFormat) (Jingle, error) {
	codec, err := pcmCodec(format)
	if err != nil {
		return Jingle{}, fmt.Errorf("ffmpeg: jingle %s: %w", name, err)
	}

	srcPath := filepath.Join(dir, "jingle_"+name+".mp3")
	if err := os.WriteFile(srcPath, mp3, 0o600); err != nil {
		return Jingle{}, fmt.Errorf("ffmpeg: write jingle %s source: %w", name, err)
	}
	outPath := filepath.Join(dir, "jingle_"+name+".wav")

	args := []string{
		"-hide_banner", "-nostdin", "-y",
		"-i", srcPath,
		// An mp3's embedded cover art is a video stream to ffmpeg and the wav
		// muxer refuses it, so -vn drops it; -map_metadata -1 drops the ID3
		// frames so no source tags survive into the wav.
		//
		// This does NOT make the output a bare 44-byte canonical header:
		// ffmpeg's wav muxer still writes its own LIST/INFO chunk (ISFT:
		// Lavf…) unless -fflags +bitexact is given. That is harmless and
		// deliberately tolerated — ParseWavFormat and WavDuration both walk
		// the chunk list rather than assuming fixed offsets, so they find
		// fmt/data behind it, and the concat demuxer does the same.
		"-vn", "-map_metadata", "-1",
		"-ac", strconv.Itoa(format.Channels),
		"-ar", strconv.Itoa(format.SampleRate),
		"-c:a", codec,
		"-f", "wav",
		outPath,
	}
	if err := f.runner()(ctx, f.binary(), args...); err != nil {
		return Jingle{}, fmt.Errorf("ffmpeg: decode jingle %s: %w", name, err)
	}

	// #nosec G304 -- outPath is built from the caller's temp dir and a
	// constant basename; no user input reaches it.
	decoded, err := os.ReadFile(outPath)
	if err != nil {
		return Jingle{}, fmt.Errorf("ffmpeg: read decoded jingle %s: %w", name, err)
	}
	// Verify rather than trust: the concat compatibility guarantee is only
	// structural if the produced bytes are checked against the engine format
	// we asked for (§12-5). A mismatch degrades the jingle, never the concat.
	got, err := ParseWavFormat(decoded)
	if err != nil {
		return Jingle{}, fmt.Errorf("ffmpeg: parse decoded jingle %s: %w", name, err)
	}
	if got != format {
		return Jingle{}, fmt.Errorf("ffmpeg: jingle %s decoded as %+v, want %+v", name, got, format)
	}
	d, err := WavDuration(decoded)
	if err != nil {
		return Jingle{}, fmt.Errorf("ffmpeg: measure jingle %s: %w", name, err)
	}
	// A zero-length decode (ffmpeg exits 0 having written an empty data
	// chunk) is a failure, not a valid jingle: it would ship a silent
	// decoration into the concat list and leave no trace in the logs, since
	// nothing downstream distinguishes "0 seconds" from "not there". Fail so
	// the §8 degradation path reports it.
	if d <= 0 {
		return Jingle{}, fmt.Errorf("ffmpeg: jingle %s decoded to %s of audio", name, d)
	}
	return Jingle{Path: outPath, Duration: d}, nil
}

// pcmCodec is the ffmpeg encoder that produces the given linear-PCM sample
// width. It is derived from the measured format instead of pinned to the
// engine's current 16-bit output, so a VOICEVOX upgrade that changes the
// sample width moves the jingle with it rather than silently producing a
// concat list of two different formats.
func pcmCodec(format WavFormat) (string, error) {
	if format.AudioFormat != 1 {
		return "", fmt.Errorf("non-PCM target format tag %d", format.AudioFormat)
	}
	if format.Channels <= 0 || format.SampleRate <= 0 {
		return "", fmt.Errorf("invalid target format %+v", format)
	}
	switch format.BitsPerSample {
	case 8:
		return "pcm_u8", nil // 8-bit WAV PCM is unsigned, every wider width signed
	case 16:
		return "pcm_s16le", nil
	case 24:
		return "pcm_s24le", nil
	case 32:
		return "pcm_s32le", nil
	default:
		return "", fmt.Errorf("unsupported sample width %d", format.BitsPerSample)
	}
}
