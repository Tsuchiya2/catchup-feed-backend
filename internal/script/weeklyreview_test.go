package script

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/learning"
)

func TestBuildWeeklyReview(t *testing.T) {
	tests := []struct {
		name         string
		material     learning.WeeklyReview
		wantOK       bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:     "empty week is skipped (§7.4: 空の振り返りを作らない)",
			material: learning.WeeklyReview{},
			wantOK:   false,
		},
		{
			name: "concepts only",
			material: learning.WeeklyReview{
				Concepts: []string{"コンテキスト伝播", "select 文"},
			},
			wantOK:       true,
			wantContains: []string{"今週の学びを振り返って", "2個の項目", "コンテキスト伝播、select 文", "来週も"},
			wantAbsent:   []string{"卒業", "忘れて"},
		},
		{
			name: "concepts + graduations + reintroduction",
			material: learning.WeeklyReview{
				Concepts:       []string{"A", "B", "C"},
				GraduatedCount: 2,
				Reintroduced:   "難しい概念",
			},
			wantOK: true,
			wantContains: []string{
				"3個の項目", "A、B、C",
				"2個の項目", "卒業",
				"「難しい概念」", "忘れてしまった",
			},
		},
		{
			name: "graduations only, no items learned this week",
			material: learning.WeeklyReview{
				GraduatedCount: 1,
			},
			wantOK:       true,
			wantContains: []string{"1個の項目", "卒業", "来週も"},
			wantAbsent:   []string{"今週は全部で"}, // concepts sentence omitted
		},
		{
			name: "reintroduction only",
			material: learning.WeeklyReview{
				Reintroduced: "忘れた項目",
			},
			wantOK:       true,
			wantContains: []string{"「忘れた項目」", "もう一度おさらい"},
			wantAbsent:   []string{"個の項目", "卒業"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, ok := BuildWeeklyReview(tt.material)
			assert.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				assert.Empty(t, body)
				return
			}
			for _, s := range tt.wantContains {
				assert.Contains(t, body, s)
			}
			for _, s := range tt.wantAbsent {
				assert.NotContains(t, body, s)
			}
			// The script is a clean read — no leftover template tokens.
			assert.NotContains(t, body, "%!")
			assert.True(t, strings.HasSuffix(body, "。"), "ends on a full stop for clean TTS")
		})
	}
}

func TestAppendWeeklyReviewShowNotes(t *testing.T) {
	base := "既存のノート。"

	t.Run("empty week leaves notes unchanged", func(t *testing.T) {
		assert.Equal(t, base, AppendWeeklyReviewShowNotes(base, learning.WeeklyReview{}))
	})

	t.Run("concepts + graduation + reintroduction", func(t *testing.T) {
		out := AppendWeeklyReviewShowNotes(base, learning.WeeklyReview{
			Concepts:       []string{"概念1", "概念2"},
			GraduatedCount: 3,
			Reintroduced:   "戻した概念",
		})
		assert.True(t, strings.HasPrefix(out, base), "base notes are preserved")
		assert.Contains(t, out, "今週の学び:")
		assert.Contains(t, out, "- 概念1")
		assert.Contains(t, out, "- 概念2")
		assert.Contains(t, out, "卒業した項目: 3件")
		assert.Contains(t, out, "もう一度おさらい: 戻した概念")
	})

	t.Run("no reintroduction omits that line", func(t *testing.T) {
		out := AppendWeeklyReviewShowNotes(base, learning.WeeklyReview{
			Concepts:       []string{"概念1"},
			GraduatedCount: 0,
		})
		assert.Contains(t, out, "卒業した項目: 0件")
		assert.NotContains(t, out, "もう一度おさらい")
	})
}
