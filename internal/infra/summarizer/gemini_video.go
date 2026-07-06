package summarizer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Phase 2 §5.1 第1段(C-14): Gemini に公開 YouTube 動画の URL を直接入力し、
// 「詳細な内容書き起こし+日本語要約」を1リクエストで得る。
//
// Gemini API の YouTube URL 対応(2026-07 実装時に確認した現行仕様):
//   - v1beta generateContent の file_data.file_uri に watch URL を渡す
//     (MIME type 不要。素の HTTP、SDK なし — 既存プロバイダと同じ流儀)
//   - 公開動画のみ。非公開・限定公開はエラーになる(C-12 の分界とも一致)
//   - 無料枠は YouTube 動画あわせて 8 時間/日。動画は既定解像度で約 300
//     トークン/秒消費するため、実際には無料枠のトークンレート上限が先に
//     効き、長尺(目安 15 分超)はクオータエラーで返ることが多い
//   - 1M コンテキストのモデル(gemini-2.5-flash 含む)の理論上限は既定
//     解像度で約1時間
//
// どの失敗(クオータ・非対応・長尺超過・タイムアウト・応答形式破損)も
// 呼び出し側が第2段(字幕)へフォールバックするだけで、ここでは再試行
// しない(C-3 / C-14: 次段が回収する)。
const (
	// videoDescribeTimeout is the per-request deadline for DescribeVideo.
	// Video ingestion is far slower than text summarization (the API
	// processes the whole video before answering), so this extends the
	// text default (Options.Timeout, 60s) instead of reusing it. A cap of
	// YouTubeDirectMaxPerCycle sequential attempts keeps the worst case
	// well inside the 30-minute crawl budget.
	videoDescribeTimeout = 5 * time.Minute

	// transcriptMarker / summaryMarker split the single response into the
	// transcript (→ articles.content) and the summary (→ summaries.body).
	// Markers instead of JSON: a transcript is thousands of Japanese
	// characters with newlines, which models escape unreliably inside
	// JSON strings; unique marker lines fail loudly (→ fallback) instead
	// of silently corrupting.
	transcriptMarker = "===TRANSCRIPT==="
	summaryMarker    = "===SUMMARY==="
)

// NewVideoDescriberFromEnv returns the Gemini provider used as the §5.1
// stage-1 video describer, or nil when GEMINI_API_KEY is unset — the caller
// must then leave the fetch Service's VideoDescriber nil so stage 1 is
// skipped entirely and every new video goes straight to the transcribe
// queue (stage 2/3 on the Mac). Reuses the summarizer environment knobs
// (GEMINI_MODEL, SUMMARIZER_CHAR_LIMIT); the request timeout is the video
// constant above, not SUMMARIZER_TIMEOUT.
func NewVideoDescriberFromEnv(logger *slog.Logger) *Gemini {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := LoadGeminiConfig(LoadOptions())
	if cfg.APIKey == "" {
		logger.Info("youtube direct description disabled: GEMINI_API_KEY not set")
		return nil
	}
	logger.Info("youtube direct description enabled",
		slog.String("model", cfg.Model),
		slog.Duration("timeout", videoDescribeTimeout))
	return NewGemini(cfg)
}

// DescribeVideo sends one generateContent request with the public video URL
// as file_data and asks for a detailed transcript plus a Japanese summary,
// separated by marker lines. Implements the fetch usecase VideoDescriber.
// Any error — HTTP failure, quota, unsupported/too-long video, timeout, or
// a response that does not parse into both sections — is returned as-is;
// the caller falls back to the transcribe queue and never retries here.
func (g *Gemini) DescribeVideo(ctx context.Context, videoURL string) (transcript, summary string, err error) {
	ctx, cancel := context.WithTimeout(ctx, videoDescribeTimeout)
	defer cancel()

	parts := []geminiPart{
		{FileData: &geminiFileData{FileURI: videoURL}},
		{Text: buildVideoPrompt(g.config.Options.CharacterLimit)},
	}
	out, err := g.generate(ctx, parts)
	if err != nil {
		return "", "", err
	}

	transcript, summary, err = parseVideoDescription(out)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", ProviderGemini, err)
	}
	return transcript, summary, nil
}

// buildVideoPrompt constructs the §5.1 stage-1 prompt. Only the (public)
// video referenced by file_data is involved (C-12). The transcript section
// is mandatory by design: Phase 3's comprehension tracker needs the source
// text, so a summary-only shortcut is not allowed (§5.1).
func buildVideoPrompt(charLimit int) string {
	return fmt.Sprintf(`添付の動画の内容を確認し、次の2つのセクションを順に出力してください。

%s
このマーカー行の直後に、動画で話された内容の詳細な書き起こしを日本語で出力する。話題の流れ・具体例・結論を省略せず文章化する。

%s
このマーカー行の直後に、日本語で%d文字以内の要約を出力する。

2つのマーカー行(%s と %s)は必ずこの表記のまま1回ずつ出力し、マーカー行とセクション本文以外の前置き・後書きは出力しないでください。`,
		transcriptMarker, summaryMarker, charLimit, transcriptMarker, summaryMarker)
}

// parseVideoDescription splits the model output into transcript and summary.
// Both sections must be present, non-empty, and free of residual marker
// lines; anything else is an error so the caller falls back to the
// transcribe queue (壊れた応答を保存しない).
func parseVideoDescription(out string) (transcript, summary string, err error) {
	ti := strings.Index(out, transcriptMarker)
	if ti < 0 {
		return "", "", fmt.Errorf("video response missing %s marker", transcriptMarker)
	}
	rest := out[ti+len(transcriptMarker):]

	si := strings.Index(rest, summaryMarker)
	if si < 0 {
		return "", "", fmt.Errorf("video response missing %s marker after transcript", summaryMarker)
	}

	transcript = strings.TrimSpace(rest[:si])
	summary = strings.TrimSpace(rest[si+len(summaryMarker):])
	if transcript == "" {
		return "", "", fmt.Errorf("video response has empty transcript section")
	}
	if summary == "" {
		return "", "", fmt.Errorf("video response has empty summary section")
	}
	// Strict marker check: the prompt requires each marker exactly once, so
	// a marker surviving inside a section body means the model deviated from
	// the format and the split cannot be trusted (e.g. a duplicated
	// transcript block, or a second summary appended after the first). Fail
	// so the caller falls back to the transcribe queue instead of persisting
	// a corrupted section. Note the split above already guarantees the
	// transcript section cannot contain summaryMarker (si is its first
	// occurrence), so only three residual cases exist.
	if strings.Contains(transcript, transcriptMarker) {
		return "", "", fmt.Errorf("video response has duplicate %s marker inside transcript section", transcriptMarker)
	}
	if strings.Contains(summary, transcriptMarker) || strings.Contains(summary, summaryMarker) {
		return "", "", fmt.Errorf("video response has stray marker inside summary section")
	}
	return transcript, summary, nil
}
