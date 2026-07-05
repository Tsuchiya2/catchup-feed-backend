package tts_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/tts"
)

// voicevoxStub fakes the two-step VOICEVOX Engine API.
type voicevoxStub struct {
	t *testing.T

	mu            sync.Mutex
	queryTexts    []string  // text params seen by /audio_query
	querySpeakers []string  // speaker params seen by /audio_query
	synthSpeakers []string  // speaker params seen by /synthesis
	synthSpeeds   []float64 // speedScale in /synthesis request bodies
	queryStatus   int       // non-zero forces /audio_query failure
	synthStatus   int       // non-zero forces /synthesis failure
	wav           []byte    // /synthesis response body
}

func (s *voicevoxStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/audio_query", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.queryTexts = append(s.queryTexts, r.URL.Query().Get("text"))
		s.querySpeakers = append(s.querySpeakers, r.URL.Query().Get("speaker"))
		s.mu.Unlock()
		if s.queryStatus != 0 {
			http.Error(w, "engine error", s.queryStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accent_phrases":[],"speedScale":1.0,"pitchScale":0.0,"outputSamplingRate":24000}`))
	})
	mux.HandleFunc("/synthesis", func(w http.ResponseWriter, r *http.Request) {
		var query map[string]any
		require.NoError(s.t, json.NewDecoder(r.Body).Decode(&query))
		s.mu.Lock()
		s.synthSpeakers = append(s.synthSpeakers, r.URL.Query().Get("speaker"))
		if speed, ok := query["speedScale"].(float64); ok {
			s.synthSpeeds = append(s.synthSpeeds, speed)
		}
		s.mu.Unlock()
		if s.synthStatus != 0 {
			http.Error(w, "synthesis error", s.synthStatus)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(s.wav)
	})
	return mux
}

func newVoicevoxTest(t *testing.T, stub *voicevoxStub, cfg tts.VoicevoxConfig) (*tts.Voicevox, *voicevoxStub) {
	t.Helper()
	stub.t = t
	if stub.wav == nil {
		stub.wav = buildWAV(t, 24000, 48000) // 1 second
	}
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)
	cfg.BaseURL = srv.URL
	return tts.NewVoicevox(cfg), stub
}

func TestVoicevox_SynthesizeScript(t *testing.T) {
	client, stub := newVoicevoxTest(t, &voicevoxStub{},
		tts.VoicevoxConfig{Speaker: 13, SpeedScale: 1.2, Timeout: 5 * time.Second})

	audios, err := client.SynthesizeScript(context.Background(), "おはようございます。今日のニュースです。")
	require.NoError(t, err)
	require.Len(t, audios, 2, "one synthesis per sentence (§6-3)")

	assert.Equal(t, []string{"おはようございます。", "今日のニュースです。"}, stub.queryTexts)
	assert.Equal(t, []string{"13", "13"}, stub.querySpeakers, "speaker ID from config (D-2)")
	assert.Equal(t, []string{"13", "13"}, stub.synthSpeakers)
	assert.Equal(t, []float64{1.2, 1.2}, stub.synthSpeeds, "speedScale overridden from config (D-2)")

	for _, a := range audios {
		assert.Equal(t, time.Second, a.Duration)
		assert.NotEmpty(t, a.Data)
	}
}

func TestVoicevox_Errors(t *testing.T) {
	tests := []struct {
		name    string
		stub    *voicevoxStub
		script  string
		wantSub string
	}{
		{
			name:    "audio_query failure aborts the day",
			stub:    &voicevoxStub{queryStatus: http.StatusInternalServerError},
			script:  "こんにちは。",
			wantSub: "audio_query",
		},
		{
			name:    "synthesis failure aborts the day",
			stub:    &voicevoxStub{synthStatus: http.StatusUnprocessableEntity},
			script:  "こんにちは。",
			wantSub: "synthesis",
		},
		{
			name:    "invalid wav payload is an error",
			stub:    &voicevoxStub{wav: []byte("not a wav")},
			script:  "こんにちは。",
			wantSub: "RIFF",
		},
		{
			name:    "empty script is an error",
			stub:    &voicevoxStub{},
			script:  "   \n ",
			wantSub: "no sentences",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newVoicevoxTest(t, tt.stub, tts.VoicevoxConfig{Timeout: 5 * time.Second})
			_, err := client.SynthesizeScript(context.Background(), tt.script)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
		})
	}
}

func TestVoicevox_UnreachableEngine(t *testing.T) {
	// Connection refused (engine down) must surface as an error so the
	// batch skips the day (§8: VOICEVOX 障害→当日スキップ).
	client := tts.NewVoicevox(tts.VoicevoxConfig{
		BaseURL: "http://127.0.0.1:1", // reserved port, nothing listens
		Timeout: time.Second,
	})
	_, err := client.SynthesizeScript(context.Background(), "こんにちは。")
	assert.Error(t, err)
}
