package script

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/repository"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func parseArticles() []repository.RadioArticle {
	return []repository.RadioArticle{
		{ID: 10, Title: "A"},
		{ID: 20, Title: "B"},
		{ID: 30, Title: "C"},
	}
}

func TestCutQuizSection(t *testing.T) {
	tests := []struct {
		name        string
		out         string
		wantBody    string
		wantSection string
		wantFound   bool
	}{
		{
			name:        "marker splits body and section",
			out:         "アウトロ。\n===LEARNING_ITEMS===\n記事番号: 1",
			wantBody:    "アウトロ。\n",
			wantSection: "\n記事番号: 1",
			wantFound:   true,
		},
		{
			name:      "no marker keeps everything as body",
			out:       "アウトロだけ。",
			wantBody:  "アウトロだけ。",
			wantFound: false,
		},
		{
			name:        "first marker wins — a duplicated marker lands in the section",
			out:         "本文。\n===LEARNING_ITEMS===\nx\n===LEARNING_ITEMS===\ny",
			wantBody:    "本文。\n",
			wantSection: "\nx\n===LEARNING_ITEMS===\ny",
			wantFound:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, section, found := cutQuizSection(tt.out)
			assert.Equal(t, tt.wantBody, body,
				"the broadcast outro must never contain the marker or anything after it (§12-1)")
			assert.Equal(t, tt.wantSection, section)
			assert.Equal(t, tt.wantFound, found)
		})
	}
}

// TestStripQuizLeak pins the §12-1 safety net for marker-mangling models
// (レビュー差し戻し B-1): item traces that survive the exact-marker split
// are truncated out of the broadcast body.
func TestStripQuizLeak(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		want       string
		wantLeaked bool
	}{
		{
			name:       "clean outro untouched",
			body:       "今日の振り返りでした。また明日。",
			want:       "今日の振り返りでした。また明日。",
			wantLeaked: false,
		},
		{
			name:       "whitespace-mangled marker cut at its line start (no === residue)",
			body:       "アウトロ。\n=== LEARNING_ITEMS ===\n記事番号: 1",
			want:       "アウトロ。\n",
			wantLeaked: true,
		},
		{
			name:       "bare item lines cut at the 記事番号 line start",
			body:       "アウトロ。\n記事番号: 1\n概念: c",
			want:       "アウトロ。\n",
			wantLeaked: true,
		},
		{
			name:       "full-width colon variant caught",
			body:       "アウトロ。\n記事番号:2\n概念:c",
			want:       "アウトロ。\n",
			wantLeaked: true,
		},
		{
			name:       "indented item line caught",
			body:       "アウトロ。\n  記事番号: 1\n概念: c",
			want:       "アウトロ。\n",
			wantLeaked: true,
		},
		{
			name:       "earlier of the two traces wins",
			body:       "アウトロ。\n記事番号: 1\n=== LEARNING_ITEMS ===",
			want:       "アウトロ。\n",
			wantLeaked: true,
		},
		{
			name:       "記事番号 without a colon is prose, not a leak",
			body:       "アウトロで記事番号という言葉に触れる。\n記事番号 はただの語。",
			want:       "アウトロで記事番号という言葉に触れる。\n記事番号 はただの語。",
			wantLeaked: false,
		},
		{
			name:       "whole body is item lines — truncates to empty",
			body:       "記事番号: 1\n概念: c\n問題: q\n答え: a",
			want:       "",
			wantLeaked: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, leaked := stripQuizLeak(tt.body)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantLeaked, leaked)
		})
	}
}

func TestParseQuizItems(t *testing.T) {
	tests := []struct {
		name    string
		section string
		max     int
		want    []QuizDraft
	}{
		{
			name:    "single well-formed item",
			section: "記事番号: 2\n概念: 見出し\n問題: 昨日のニュースで触れた件ですが。\n答え: こうです。",
			max:     1,
			want: []QuizDraft{{ArticleID: 20, Concept: "見出し",
				Question: "昨日のニュースで触れた件ですが。", Answer: "こうです。", Provider: "gemini"}},
		},
		{
			name:    "full-width colon and label spacing tolerated",
			section: "記事番号: 3\n概念 : c\n問題:q\n答え: a",
			max:     1,
			want:    []QuizDraft{{ArticleID: 30, Concept: "c", Question: "q", Answer: "a", Provider: "gemini"}},
		},
		{
			name:    "wrapped lines join the previous field",
			section: "記事番号: 1\n概念: c\n問題: 一行目、\n二行目。\n答え: a",
			max:     1,
			want: []QuizDraft{{ArticleID: 10, Concept: "c",
				Question: "一行目、\n二行目。", Answer: "a", Provider: "gemini"}},
		},
		{
			name:    "preamble before the first block is ignored",
			section: "以下が学習項目です。\n\n記事番号: 1\n概念: c\n問題: q\n答え: a",
			max:     1,
			want:    []QuizDraft{{ArticleID: 10, Concept: "c", Question: "q", Answer: "a", Provider: "gemini"}},
		},
		{
			name: "items beyond max are discarded (§6.2 飽和算術)",
			section: "記事番号: 1\n概念: c1\n問題: q1\n答え: a1\n" +
				"記事番号: 2\n概念: c2\n問題: q2\n答え: a2",
			max:  1,
			want: []QuizDraft{{ArticleID: 10, Concept: "c1", Question: "q1", Answer: "a1", Provider: "gemini"}},
		},
		{
			name: "duplicate article number dropped, later distinct one kept",
			section: "記事番号: 1\n概念: c1\n問題: q1\n答え: a1\n" +
				"記事番号: 1\n概念: c1b\n問題: q1b\n答え: a1b\n" +
				"記事番号: 3\n概念: c3\n問題: q3\n答え: a3",
			max: 2,
			want: []QuizDraft{
				{ArticleID: 10, Concept: "c1", Question: "q1", Answer: "a1", Provider: "gemini"},
				{ArticleID: 30, Concept: "c3", Question: "q3", Answer: "a3", Provider: "gemini"},
			},
		},
		{
			name: "invalid blocks skipped without poisoning valid ones",
			section: "記事番号: 0\n概念: c\n問題: q\n答え: a\n" + // out of range (low)
				"記事番号: 4\n概念: c\n問題: q\n答え: a\n" + // out of range (high)
				"記事番号: そのうち\n概念: c\n問題: q\n答え: a\n" + // non-numeric
				"記事番号: 2\n概念: c\n問題: q\n" + // answer missing
				"記事番号: 3\n概念: c3\n問題: q3\n答え: a3",
			max:  3,
			want: []QuizDraft{{ArticleID: 30, Concept: "c3", Question: "q3", Answer: "a3", Provider: "gemini"}},
		},
		{
			name:    "stray marker line inside the section never reaches a field",
			section: "記事番号: 1\n概念: c\n問題: q\n===LEARNING_ITEMS===\n答え: a",
			max:     1,
			want:    []QuizDraft{{ArticleID: 10, Concept: "c", Question: "q", Answer: "a", Provider: "gemini"}},
		},
		{
			name:    "free text only yields nothing",
			section: "今日は学習項目を作れませんでした。",
			max:     1,
			want:    nil,
		},
		{
			name:    "empty section yields nothing",
			section: "",
			max:     1,
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseQuizItems(tt.section, parseArticles(), tt.max, "gemini", discardLogger())
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCutLabel(t *testing.T) {
	tests := []struct {
		line   string
		label  string
		want   string
		wantOK bool
	}{
		{"記事番号: 2", "記事番号", "2", true},
		{"記事番号:2", "記事番号", "2", true},
		{"記事番号 : 2", "記事番号", "2", true},
		{"記事番号 2", "記事番号", "", false}, // no separator
		{"概念: ", "概念", "", true},      // empty value is the flush's problem
		{"問題です: x", "問題", "", false},  // label must be an exact prefix up to the colon
	}
	for _, tt := range tests {
		got, ok := cutLabel(tt.line, tt.label)
		assert.Equal(t, tt.wantOK, ok, "line=%q", tt.line)
		assert.Equal(t, tt.want, got, "line=%q", tt.line)
	}
}
