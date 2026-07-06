package summarizer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/infra/summarizer"
)

const videoResponseOK = `===TRANSCRIPT===
この動画では Go のジェネリクスについて解説しています。まず型パラメータの基本から始まり、制約の書き方、実際のユースケースまで順に説明されます。

===SUMMARY===
Go のジェネリクス入門動画。型パラメータと制約の基礎を実例で解説する。`

func TestGemini_DescribeVideo_Success(t *testing.T) {
	var gotPath string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(geminiSuccessBody(videoResponseOK)))
	}))
	defer srv.Close()

	g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

	transcript, summary, err := g.DescribeVideo(context.Background(), "https://www.youtube.com/watch?v=abc123")

	require.NoError(t, err)
	assert.Contains(t, transcript, "ジェネリクスについて解説")
	assert.Equal(t, "Go のジェネリクス入門動画。型パラメータと制約の基礎を実例で解説する。", summary)
	assert.NotContains(t, transcript, "===")
	assert.NotContains(t, summary, "===")

	assert.Equal(t, "/v1beta/models/gemini-2.5-flash:generateContent", gotPath)

	// The video URL must be sent as file_data.file_uri (§5.1: generateContent
	// の file_data で URL 指定), together with the marker-based prompt.
	var req struct {
		Contents []struct {
			Parts []struct {
				Text     string `json:"text"`
				FileData *struct {
					FileURI string `json:"file_uri"`
				} `json:"file_data"`
			} `json:"parts"`
		} `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(gotBody, &req))
	require.Len(t, req.Contents, 1)
	require.Len(t, req.Contents[0].Parts, 2)
	require.NotNil(t, req.Contents[0].Parts[0].FileData)
	assert.Equal(t, "https://www.youtube.com/watch?v=abc123", req.Contents[0].Parts[0].FileData.FileURI)
	assert.Empty(t, req.Contents[0].Parts[0].Text) // media part carries no text field
	assert.Contains(t, req.Contents[0].Parts[1].Text, "===TRANSCRIPT===")
	assert.Contains(t, req.Contents[0].Parts[1].Text, "===SUMMARY===")
	assert.Contains(t, req.Contents[0].Parts[1].Text, "900文字以内")
	// Thinking must stay disabled for video too (ゼロ円運用).
	assert.Contains(t, string(gotBody), `"thinkingBudget":0`)
}

func TestGemini_DescribeVideo_Errors(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErrSub string
	}{
		{
			name: "quota exceeded (429)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"message":"quota exceeded"}}`, http.StatusTooManyRequests)
			},
			wantErrSub: "status 429",
		},
		{
			name: "unsupported / too long video (400)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"message":"video too long"}}`, http.StatusBadRequest)
			},
			wantErrSub: "status 400",
		},
		{
			name: "missing transcript marker",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody("===SUMMARY===\n要約だけ")))
			},
			wantErrSub: "missing ===TRANSCRIPT===",
		},
		{
			name: "missing summary marker",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody("===TRANSCRIPT===\n書き起こしだけ")))
			},
			wantErrSub: "missing ===SUMMARY===",
		},
		{
			name: "markers reversed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody("===SUMMARY===\n要約\n===TRANSCRIPT===\n書き起こし")))
			},
			wantErrSub: "missing ===SUMMARY=== marker after transcript",
		},
		{
			name: "empty transcript section",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody("===TRANSCRIPT===\n \n===SUMMARY===\n要約")))
			},
			wantErrSub: "empty transcript",
		},
		{
			name: "empty summary section",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody("===TRANSCRIPT===\n書き起こし\n===SUMMARY===\n  ")))
			},
			wantErrSub: "empty summary",
		},
		{
			// プロンプトは「各マーカー1回ずつ」を要求する。逸脱応答は保存
			// せずエラー(→ 第2段フォールバック)にする(strict 分割)。
			name: "duplicate transcript marker inside transcript section",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody(
					"===TRANSCRIPT===\n前半\n===TRANSCRIPT===\n後半\n===SUMMARY===\n要約")))
			},
			wantErrSub: "duplicate ===TRANSCRIPT=== marker inside transcript",
		},
		{
			name: "duplicate summary marker inside summary section",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody(
					"===TRANSCRIPT===\n書き起こし\n===SUMMARY===\n要約その1\n===SUMMARY===\n要約その2")))
			},
			wantErrSub: "stray marker inside summary section",
		},
		{
			name: "transcript marker leaking into summary section",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody(
					"===TRANSCRIPT===\n書き起こし\n===SUMMARY===\n要約\n===TRANSCRIPT===\n追加の書き起こし")))
			},
			wantErrSub: "stray marker inside summary section",
		},
		{
			name: "no candidates",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"candidates":[]}`))
			},
			wantErrSub: "no candidates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

			transcript, summary, err := g.DescribeVideo(context.Background(), "https://www.youtube.com/watch?v=abc")

			require.Error(t, err)
			assert.Empty(t, transcript)
			assert.Empty(t, summary)
			assert.Contains(t, err.Error(), tt.wantErrSub)
			assert.Contains(t, err.Error(), "gemini")
		})
	}
}

func TestGemini_DescribeVideo_CanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(geminiSuccessBody(videoResponseOK)))
	}))
	defer srv.Close()

	g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := g.DescribeVideo(ctx, "https://www.youtube.com/watch?v=abc")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestNewVideoDescriberFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantNil bool
	}{
		{"enabled with API key", "test-key", false},
		{"disabled without API key (stage 1 skipped)", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GEMINI_API_KEY", tt.apiKey)
			t.Setenv("GEMINI_MODEL", "")

			g := summarizer.NewVideoDescriberFromEnv(nil)

			if tt.wantNil {
				assert.Nil(t, g)
			} else {
				require.NotNil(t, g)
				assert.Equal(t, "gemini", g.Name())
			}
		})
	}
}
