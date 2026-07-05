package tts

import (
	"encoding/binary"
	"fmt"
	"time"
)

// WavDuration computes the playing time of a RIFF/WAVE file from its fmt
// byte rate and data chunk size. VOICEVOX returns linear-PCM WAVs, so this
// avoids an ffprobe round trip when accumulating episodes.duration_sec.
func WavDuration(wav []byte) (time.Duration, error) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return 0, fmt.Errorf("tts: not a RIFF/WAVE file")
	}

	var byteRate uint32
	var dataSize uint32
	seenFmt, seenData := false, false

	// Walk the chunk list: id(4) + size(4-LE) + payload (word-aligned).
	for offset := 12; offset+8 <= len(wav); {
		id := string(wav[offset : offset+4])
		size := binary.LittleEndian.Uint32(wav[offset+4 : offset+8])
		body := offset + 8

		switch id {
		case "fmt ":
			if body+12 > len(wav) {
				return 0, fmt.Errorf("tts: truncated fmt chunk")
			}
			byteRate = binary.LittleEndian.Uint32(wav[body+8 : body+12])
			seenFmt = true
		case "data":
			dataSize = size
			seenData = true
		}
		if seenFmt && seenData {
			break
		}
		offset = body + int(size)
		if size%2 == 1 { // chunks are word-aligned
			offset++
		}
	}

	if !seenFmt || !seenData {
		return 0, fmt.Errorf("tts: fmt or data chunk missing")
	}
	if byteRate == 0 {
		return 0, fmt.Errorf("tts: zero byte rate")
	}
	seconds := float64(dataSize) / float64(byteRate)
	return time.Duration(seconds * float64(time.Second)), nil
}
