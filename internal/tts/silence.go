package tts

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// WavFormat is the PCM format of a RIFF/WAVE file, read from its fmt chunk.
// The quiz corner (Phase 3 §7.2) uses it to fabricate the question→answer
// silence in exactly the format the VOICEVOX engine produced this run:
// a sample-rate / channel / bit-depth mismatch breaks the ffmpeg concat
// (§12-5: 無音 wav は VOICEVOX 出力と同一フォーマットで用意する).
type WavFormat struct {
	// AudioFormat is the WAVE format tag; 1 = linear PCM (VOICEVOX output).
	AudioFormat uint16
	Channels    int
	SampleRate  int
	// BitsPerSample is the sample width (VOICEVOX: 16).
	BitsPerSample int
}

// ParseWavFormat extracts the fmt chunk of a RIFF/WAVE file.
func ParseWavFormat(wav []byte) (WavFormat, error) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return WavFormat{}, fmt.Errorf("tts: not a RIFF/WAVE file")
	}
	for offset := 12; offset+8 <= len(wav); {
		id := string(wav[offset : offset+4])
		size := binary.LittleEndian.Uint32(wav[offset+4 : offset+8])
		body := offset + 8
		if id == "fmt " {
			if size < 16 || body+16 > len(wav) {
				return WavFormat{}, fmt.Errorf("tts: truncated fmt chunk")
			}
			return WavFormat{
				AudioFormat:   binary.LittleEndian.Uint16(wav[body : body+2]),
				Channels:      int(binary.LittleEndian.Uint16(wav[body+2 : body+4])),
				SampleRate:    int(binary.LittleEndian.Uint32(wav[body+4 : body+8])),
				BitsPerSample: int(binary.LittleEndian.Uint16(wav[body+14 : body+16])),
			}, nil
		}
		offset = body + int(size)
		if size%2 == 1 { // chunks are word-aligned
			offset++
		}
	}
	return WavFormat{}, fmt.Errorf("tts: fmt chunk missing")
}

// SilenceWav renders d of digital silence as a standalone RIFF/WAVE file in
// the given linear-PCM format. It is generated in-process — deliberately not
// via VOICEVOX (§7.2: 無音ポーズはエンジンに作らせない — 句読点の間は不安定)
// and not via an ffmpeg anullsrc subprocess: deriving the format from an
// actual engine output WAV makes the concat-compatibility guarantee (§12-5)
// structural instead of configured.
func SilenceWav(format WavFormat, d time.Duration) ([]byte, error) {
	if format.AudioFormat != 1 {
		return nil, fmt.Errorf("tts: silence: non-PCM format tag %d", format.AudioFormat)
	}
	if format.Channels <= 0 || format.SampleRate <= 0 ||
		format.BitsPerSample <= 0 || format.BitsPerSample%8 != 0 {
		return nil, fmt.Errorf("tts: silence: invalid format %+v", format)
	}
	if d <= 0 {
		return nil, fmt.Errorf("tts: silence: non-positive duration %s", d)
	}

	blockAlign := format.Channels * format.BitsPerSample / 8
	byteRate := format.SampleRate * blockAlign
	frames := int(math.Round(d.Seconds() * float64(format.SampleRate)))
	dataSize := frames * blockAlign

	buf := make([]byte, 0, 44+dataSize)
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+dataSize)) // #nosec G115 -- bounded: seconds of PCM
	buf = append(buf, "WAVE"...)
	buf = append(buf, "fmt "...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, format.AudioFormat)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(format.Channels))      // #nosec G115
	buf = binary.LittleEndian.AppendUint32(buf, uint32(format.SampleRate))    // #nosec G115
	buf = binary.LittleEndian.AppendUint32(buf, uint32(byteRate))             // #nosec G115
	buf = binary.LittleEndian.AppendUint16(buf, uint16(blockAlign))           // #nosec G115
	buf = binary.LittleEndian.AppendUint16(buf, uint16(format.BitsPerSample)) // #nosec G115
	buf = append(buf, "data"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(dataSize)) // #nosec G115
	buf = append(buf, make([]byte, dataSize)...)                  // PCM silence = zero samples
	return buf, nil
}
