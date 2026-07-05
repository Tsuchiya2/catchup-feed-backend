package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	pkgconfig "catchup-feed/pkg/config"
)

const (
	// defaultVoicevoxURL is the local VOICEVOX Engine endpoint.
	defaultVoicevoxURL = "http://127.0.0.1:50021"

	// defaultSpeaker is the provisional voice (D-2: 仮話者で実装先行、耳での
	// 選定は radio バッチ完成後). Style ID 3 = ずんだもん(ノーマル), present
	// in every stock VOICEVOX Engine install.
	defaultSpeaker = 3

	// defaultSpeedScale is VOICEVOX's neutral speed.
	defaultSpeedScale = 1.0

	// defaultVoicevoxTimeout bounds one audio_query+synthesis round trip.
	defaultVoicevoxTimeout = 120 * time.Second
)

// VoicevoxConfig configures the VOICEVOX Engine client (§6-3).
type VoicevoxConfig struct {
	// BaseURL is the engine origin (VOICEVOX_URL).
	BaseURL string
	// Speaker is the VOICEVOX style ID (VOICEVOX_SPEAKER, D-2: config 指定).
	Speaker int
	// SpeedScale is the speaking rate multiplier (VOICEVOX_SPEED_SCALE, D-2).
	SpeedScale float64
	// Timeout bounds a single sentence synthesis (VOICEVOX_TIMEOUT).
	Timeout time.Duration
}

// LoadVoicevoxConfig reads VOICEVOX settings from environment variables:
//
//   - VOICEVOX_URL: engine origin (default http://127.0.0.1:50021)
//   - VOICEVOX_SPEAKER: style ID (default 3 = ずんだもん ノーマル、仮話者)
//   - VOICEVOX_SPEED_SCALE: speaking rate (default 1.0)
//   - VOICEVOX_TIMEOUT: per-sentence timeout (default 120s)
func LoadVoicevoxConfig() VoicevoxConfig {
	cfg := VoicevoxConfig{
		BaseURL:    pkgconfig.GetEnvString("VOICEVOX_URL", defaultVoicevoxURL),
		Speaker:    pkgconfig.GetEnvInt("VOICEVOX_SPEAKER", defaultSpeaker),
		SpeedScale: defaultSpeedScale,
		Timeout:    pkgconfig.GetEnvDuration("VOICEVOX_TIMEOUT", defaultVoicevoxTimeout),
	}
	if v := pkgconfig.GetEnvString("VOICEVOX_SPEED_SCALE", ""); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil || parsed <= 0 {
			slog.Warn("invalid VOICEVOX_SPEED_SCALE, using default",
				slog.String("value", v), slog.Float64("default", defaultSpeedScale))
		} else {
			cfg.SpeedScale = parsed
		}
	}
	return cfg
}

// Audio is one synthesized sentence: WAV bytes plus playing time (used to
// accumulate episodes.duration_sec without re-probing files).
type Audio struct {
	Data     []byte
	Duration time.Duration
}

// Voicevox synthesizes speech via the VOICEVOX Engine two-step HTTP API:
// POST /audio_query (text -> query JSON) then POST /synthesis (query -> WAV).
// Any failure is fatal for the day's episode — the radio batch exits and
// launchd retries tomorrow (§8: VOICEVOX 障害→当日スキップ).
type Voicevox struct {
	config VoicevoxConfig
	client *http.Client
}

// NewVoicevox creates a client; zero-valued config fields fall back to the
// package defaults.
func NewVoicevox(config VoicevoxConfig) *Voicevox {
	if config.BaseURL == "" {
		config.BaseURL = defaultVoicevoxURL
	}
	if config.Speaker == 0 {
		config.Speaker = defaultSpeaker
	}
	if config.SpeedScale == 0 {
		config.SpeedScale = defaultSpeedScale
	}
	if config.Timeout == 0 {
		config.Timeout = defaultVoicevoxTimeout
	}
	return &Voicevox{config: config, client: &http.Client{}}
}

// SynthesizeScript renders one segment script as a sequence of sentence
// WAVs, in reading order (§6-3: 文単位に分割して合成).
func (v *Voicevox) SynthesizeScript(ctx context.Context, script string) ([]Audio, error) {
	sentences := SplitSentences(script)
	if len(sentences) == 0 {
		return nil, fmt.Errorf("voicevox: script has no sentences")
	}
	audios := make([]Audio, 0, len(sentences))
	for i, sentence := range sentences {
		audio, err := v.synthesizeSentence(ctx, sentence)
		if err != nil {
			return nil, fmt.Errorf("voicevox: sentence %d/%d: %w", i+1, len(sentences), err)
		}
		audios = append(audios, audio)
	}
	return audios, nil
}

// synthesizeSentence runs audio_query -> speedScale override -> synthesis
// for a single sentence.
func (v *Voicevox) synthesizeSentence(ctx context.Context, sentence string) (Audio, error) {
	ctx, cancel := context.WithTimeout(ctx, v.config.Timeout)
	defer cancel()

	query, err := v.audioQuery(ctx, sentence)
	if err != nil {
		return Audio{}, err
	}
	query["speedScale"] = v.config.SpeedScale // D-2: 話速は config

	wav, err := v.synthesis(ctx, query)
	if err != nil {
		return Audio{}, err
	}
	duration, err := WavDuration(wav)
	if err != nil {
		return Audio{}, err
	}
	return Audio{Data: wav, Duration: duration}, nil
}

func (v *Voicevox) audioQuery(ctx context.Context, text string) (map[string]any, error) {
	endpoint := v.config.BaseURL + "/audio_query?" + url.Values{
		"text":    {text},
		"speaker": {strconv.Itoa(v.config.Speaker)},
	}.Encode()

	body, err := v.post(ctx, endpoint, "", nil)
	if err != nil {
		return nil, fmt.Errorf("audio_query: %w", err)
	}
	var query map[string]any
	if err := json.Unmarshal(body, &query); err != nil {
		return nil, fmt.Errorf("audio_query: decode: %w", err)
	}
	return query, nil
}

func (v *Voicevox) synthesis(ctx context.Context, query map[string]any) ([]byte, error) {
	payload, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("synthesis: marshal query: %w", err)
	}
	endpoint := v.config.BaseURL + "/synthesis?" + url.Values{
		"speaker": {strconv.Itoa(v.config.Speaker)},
	}.Encode()

	wav, err := v.post(ctx, endpoint, "application/json", payload)
	if err != nil {
		return nil, fmt.Errorf("synthesis: %w", err)
	}
	return wav, nil
}

// post sends a POST and returns the whole response body; non-2xx responses
// become errors with a body snippet for the log.
func (v *Voicevox) post(ctx context.Context, endpoint, contentType string, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		const maxErrBody = 512
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(snippet))
	}
	return io.ReadAll(resp.Body)
}
