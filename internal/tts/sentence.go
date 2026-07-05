// Package tts turns episode scripts into audio: a VOICEVOX HTTP API client
// (§6-3) and an ffmpeg-based combiner (§6-4). No client library — VOICEVOX
// exposes plain HTTP and ffmpeg is invoked via os/exec.
package tts

import "strings"

// sentence terminators. Long scripts are synthesized sentence by sentence
// because VOICEVOX is markedly more stable on short inputs (§6-3: 長文は
// 文単位に分割して合成).
const sentenceTerminators = "。!?!?\n"

// SplitSentences splits a script into sentences on Japanese/ASCII sentence
// terminators and newlines. Terminators stay attached to their sentence
// (VOICEVOX uses them for phrase-final intonation); consecutive terminators
// like "!?" stay together. Whitespace-only fragments are dropped.
func SplitSentences(script string) []string {
	var out []string
	var sb strings.Builder
	terminated := false

	flush := func() {
		if s := strings.TrimSpace(sb.String()); s != "" {
			out = append(out, s)
		}
		sb.Reset()
	}

	for _, r := range script {
		isTerm := strings.ContainsRune(sentenceTerminators, r)
		if terminated && !isTerm {
			flush()
		}
		if r != '\n' { // newlines split but are not read aloud
			sb.WriteRune(r)
		}
		terminated = isTerm
	}
	flush()
	return out
}
