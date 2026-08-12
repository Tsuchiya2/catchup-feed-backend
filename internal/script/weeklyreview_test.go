package script

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/learning"
)

// TestBuildWeeklyReview walks every combination of the three materials and
// pins the full read (D-37: 文言は format.go、組み立てはここ). Full-string
// equality rather than Contains: 「いっぽうで」「また、」のような接続は直前の
// 文の有無で変わるので、欠けた組み合わせで日本語が壊れていないことは全文でしか
// 担保できない。
//
// 卒業の文が「今週学んだ項目」の部分集合を主張しないことも、ここで固定して
// いる(format.go weeklyReviewGraduated 参照: 2つの材料は母集合が違い、既定の
// ラダーでは必ず素集合になる)。「このうちN個が」に戻すとこのテストが落ちる。
func TestBuildWeeklyReview(t *testing.T) {
	const lead = "ここで、今週の学びを振り返ります。"
	const closing = "来週も少しずつ続けていきましょう。"

	tests := []struct {
		name     string
		material learning.WeeklyReview
		wantOK   bool
		want     string
	}{
		{
			name:     "empty week is skipped (§7.4: 空の振り返りを作らない)",
			material: learning.WeeklyReview{},
			wantOK:   false,
		},
		{
			name:     "concepts only",
			material: learning.WeeklyReview{Concepts: []string{"コンテキスト伝播", "select 文"}},
			wantOK:   true,
			want:     lead + "今週学んだ項目は、コンテキスト伝播、select 文 の2つです。" + closing,
		},
		{
			name:     "graduations only — 先行文が無いので接続詞を落とす",
			material: learning.WeeklyReview{GraduatedCount: 1},
			wantOK:   true,
			want:     lead + "今週は1つの項目が繰り返しの復習を経て定着し、復習リストを卒業しました。" + closing,
		},
		{
			name:     "reintroduction only — 逆接の「いっぽうで」を落とす",
			material: learning.WeeklyReview{Reintroduced: "忘れた項目"},
			wantOK:   true,
			want:     lead + "「忘れた項目」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。" + closing,
		},
		{
			name: "concepts + graduations",
			material: learning.WeeklyReview{
				Concepts:       []string{"A", "B", "C"},
				GraduatedCount: 2,
			},
			wantOK: true,
			want: lead + "今週学んだ項目は、A、B、C の3つです。" +
				"また、今週は2つの項目が繰り返しの復習を経て定着し、復習リストを卒業しました。" + closing,
		},
		{
			name: "concepts + reintroduction",
			material: learning.WeeklyReview{
				Concepts:     []string{"A"},
				Reintroduced: "難しい概念",
			},
			wantOK: true,
			want: lead + "今週学んだ項目は、A の1つです。" +
				"いっぽうで「難しい概念」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。" + closing,
		},
		{
			name: "graduations + reintroduction",
			material: learning.WeeklyReview{
				GraduatedCount: 2,
				Reintroduced:   "難しい概念",
			},
			wantOK: true,
			want: lead + "今週は2つの項目が繰り返しの復習を経て定着し、復習リストを卒業しました。" +
				"いっぽうで「難しい概念」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。" + closing,
		},
		{
			name: "concepts + graduations + reintroduction",
			material: learning.WeeklyReview{
				Concepts:       []string{"A", "B", "C"},
				GraduatedCount: 2,
				Reintroduced:   "難しい概念",
			},
			wantOK: true,
			want: lead + "今週学んだ項目は、A、B、C の3つです。" +
				"また、今週は2つの項目が繰り返しの復習を経て定着し、復習リストを卒業しました。" +
				"いっぽうで「難しい概念」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。" + closing,
		},
		{
			name: "double-digit week switches the counter (「10つ」と読ませない)",
			material: learning.WeeklyReview{
				Concepts:       []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"},
				GraduatedCount: 10,
			},
			wantOK: true,
			want: lead + "今週学んだ項目は、A、B、C、D、E、F、G、H、I、J の10個です。" +
				"また、今週は10個の項目が繰り返しの復習を経て定着し、復習リストを卒業しました。" + closing,
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
			assert.Equal(t, tt.want, body)
			// The script is a clean read — no leftover template tokens.
			assert.NotContains(t, body, "%!")
			assert.True(t, strings.HasSuffix(body, "。"), "ends on a full stop for clean TTS")
			assert.NotContains(t, body, "！", "アナウンサー調: 感嘆符を使わない (D-37 (2))")
			assert.NotContains(t, body, "このうち",
				"卒業した項目は「今週学んだ項目」の部分集合ではない (format.go weeklyReviewGraduated)")
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
