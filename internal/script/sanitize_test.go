package script

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/learning"
)

// 2026-09-25 の -dry-run で gpt-oss (openai/gpt-oss-120b) が実際に出した逸脱。
// D-41 改訂の根拠そのものなので、実測の文字列のまま回帰テストの題材にする
// (U+2011 NON-BREAKING HYPHEN と全角括弧・全角スラッシュは意図的に生のまま
// 埋めてある — エスケープに直すとテストが実測から離れる)。
const (
	// segment 8: SHA-256 ハッシュの断片がそのまま音声になっていた。
	measuredHashSentence = "gem の正当性を検証する際は、SHA‑256 ハッシュが提供されており、" +
		"例として activesupport‑7.2.4.gem のハッシュは b97027b31e111 となっています。"
	// segment 4 / 6: 全角括弧と全角スラッシュ。
	measuredBracketSentence = "継続的インテグレーション、デリバリーの環境では、" +
		"CI/CD（継続的インテグレーション／デリバリー）の設定を見直す必要があります。"
)

func TestSanitizeScript_MeasuredDeviations(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		want        string
		wantDropped int
		wantHyphens int
		assertFn    func(t *testing.T, got string)
	}{
		{
			// 実測1: ハッシュの断片を含む文ごと落とす。文の途中から羅列だけを
			// 抜くと「ハッシュは となっています」になるため。
			name: "measured: the SHA-256 fragment sentence is dropped whole",
			in: "きょうは Ruby の gem の話題です。" + measuredHashSentence +
				"公式のドキュメントを確認してください。",
			want:        "きょうは Ruby の gem の話題です。公式のドキュメントを確認してください。",
			wantDropped: 1,
			// 落とした文の中の U+2011 も正規化の集計には乗る(正規化が先)。
			wantHyphens: 2,
		},
		{
			// 実測2: U+2011 は落とさず通常のハイフンに正規化する。
			// SHA-256 も activesupport-7.2.4.gem も読み上げ可能な語なので残す。
			name: "measured: U+2011 is normalized, the version-bearing words survive",
			in:   "SHA‑256 の説明です。activesupport‑7.2.4.gem を例に取ります。",
			want: "SHA-256 の説明です。activesupport-7.2.4.gem を例に取ります。",
			assertFn: func(t *testing.T, got string) {
				assert.NotContains(t, got, "‑", "U+2011 が残っていない")
				assert.Contains(t, got, "SHA-256")
				assert.Contains(t, got, "activesupport-7.2.4.gem")
			},
			wantHyphens: 2,
		},
		{
			// 実測3: 全角括弧と全角スラッシュは読点に倒す。括弧の閉じと句点が
			// 並んだ「、。」は句点だけに畳む。
			name: "measured: full-width brackets and slash become 読点",
			in:   measuredBracketSentence,
			// N-4: 閉じ括弧の直後が1文字助詞のときは読点を出さず括弧を消す
			// だけにする(「デリバリー、の設定」→「デリバリーの設定」)。
			want: "継続的インテグレーション、デリバリーの環境では、" +
				"CI/CD、継続的インテグレーション、デリバリーの設定を見直す必要があります。",
			assertFn: func(t *testing.T, got string) {
				assert.NotContains(t, got, "（")
				assert.NotContains(t, got, "）")
				assert.NotContains(t, got, "／")
				assert.NotContains(t, got, "、、")
				assert.NotContains(t, got, "、。")
			},
		},
		{
			// 実測4: 「SHA256 チェックサムの列挙に言及する文」。実際の羅列が
			// 無いので落とさない — 「チェックサムが列挙されています」は日本語
			// として読める(狭く落とす方針、過剰削除をしない側に倒す)。
			name: "measured: a sentence that only MENTIONS checksums is kept",
			in:   "リリースノートには各ファイルの SHA256 チェックサムが列挙されています。",
			want: "リリースノートには各ファイルの SHA256 チェックサムが列挙されています。",
		},
		{
			name: "a full 64-char hex digest takes its sentence with it",
			in: "確認してください。ハッシュ値は " +
				"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 です。" +
				"以上です。",
			want:        "確認してください。以上です。",
			wantDropped: 1,
		},
		{
			name: "a base64url token is dropped even though it is digit-poor",
			in:   "前の文。トークンは dGhpc0lzQVZlcnlMb25nVG9rZW5WYWx1ZQ です。次の文。",
			want: "前の文。次の文。",

			wantDropped: 1,
		},
		{
			name: "全角スペースは半角に倒す (D-41 当初観測)",
			in:   "今日は　いい天気です。",
			want: "今日は いい天気です。",
		},
		{
			name: "newlines and paragraph structure survive",
			in:   "一行目です。\n二行目です。",
			want: "一行目です。\n二行目です。",
		},
		{
			name: "clean script is returned byte-identical",
			in:   "きょうのニュースをお伝えします。Rails 8.1.4 が公開されました。",
			want: "きょうのニュースをお伝えします。Rails 8.1.4 が公開されました。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rep := sanitizeScript(tt.in)
			assert.Equal(t, tt.want, got)
			assert.Len(t, rep.dropped, tt.wantDropped)
			if tt.wantHyphens > 0 {
				assert.Equal(t, tt.wantHyphens, rep.hyphens)
			}
			assert.False(t, rep.keptAll)
			if tt.assertFn != nil {
				tt.assertFn(t, got)
			}
		})
	}
}

// TestSanitizeScript_KeepsVersionsAndCommonWords pins the "落としてはいけない"
// 側。版数や一般的な語で文が消えると番組の中身が抜けるため、閾値を動かすとき
// はこのテストが先に落ちること。
func TestSanitizeScript_KeepsVersionsAndCommonWords(t *testing.T) {
	keep := []string{
		"Rails 8.1.4",
		"SHA-256",
		"v1.16.6",
		"0.57.0",
		"PostgreSQL17",
		"Kubernetes 1.31",
		"activesupport-7.2.4.gem",
		"HTTP/2",
		"OAuth 2.0",
		"20260925",
		// N-3 レビュー実測: 技術番組で普通に出る表記。ここが落ちると番組の
		// 中身が抜けるので、閾値を動かすときは必ずこの表を先に通すこと。
		"CVE-2026-12345",
		"x86_64",
		"RFC9110",
		"OpenSSL3.0.14",
		"gemini-2.5-flash",
		"AES256GCM",
		"ECMAScript2015", // 14文字/数字4 = 28.6% — 閾値 30% のすぐ下
	}
	for _, s := range keep {
		t.Run(s, func(t *testing.T) {
			in := "きょうの話題は " + s + " についてです。"
			got, rep := sanitizeScript(in)
			assert.Equal(t, in, got)
			assert.Empty(t, rep.dropped, "%q を含む文は落とさない", s)
			// keptAll でも原文が返るので、「落とさない」と「落としたが
			// 取りやめた」を取り違えないようここまで見る(N-3)。
			assert.False(t, rep.keptAll, "%q は判定自体が false であること", s)
		})
	}
}

func TestIsHashLikeRun(t *testing.T) {
	tests := []struct {
		run  string
		want bool
	}{
		{"b97027b31e111", true}, // 実測の断片 (13文字, 数字 77%)
		{"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true}, // SHA-256 全長
		{"dGhpc0lzQVZlcnlMb25nVG9rZW5WYWx1ZQ", true},                               // base64url (20文字以上)
		{"a1b2c3d4e5f6", true},        // 下限ちょうど (12文字, 数字 50%)
		{"PostgreSQL17", false},       // 12文字だが数字 16.7%
		{"Kubernetes1", false},        // 11文字
		{"activesupport", false},      // 英字のみ
		{"authentication", false},     // 英字のみの長い語
		{"20260925", false},           // 数字のみ
		{"123456789012345678", false}, // 数字のみは長くても読める
		{"SHA", false},
		{"256", false},
		{"", false},
		// --- N-3: レビュー実測の境界。閾値 (12文字/数字30%, 20文字) を動かす
		// 人が最初に読む表。落とす側の誤爆と、残る側の見逃しを並べて仕様として
		// 固定する。誤爆は1文が消えるだけ、見逃しは羅列1本が読まれるだけ、と
		// いう非対称性のうえで「狭く落とす」側に倒している。
		//
		// 落ちる側(誤爆しうると分かっていて許容する):
		{"Windows11Pro22H2", true},  // 16文字/数字5 = 31.3%
		{"ISBN9784873119045", true}, // 17文字/数字13
		{"20260925T043014Z", true},  // ISO 8601 basic 形式のタイムスタンプ
		// 残る側(見逃すと分かっていて許容する):
		{"aGVsbG8gd29ybGQh", false}, // base64 16文字/数字3 = 18.8%
		{"dQw4w9WgXcQ", false},      // YouTube の動画 ID (11文字)
		{"a1b2c3d", false},          // 短縮 SHA
		// 正しく残る側:
		{"AES256GCM", false},
		{"ECMAScript2015", false}, // 14文字/数字4 = 28.6%
		{"RFC9110", false},
		{"OpenSSL3", false},
	}
	for _, tt := range tests {
		t.Run(tt.run, func(t *testing.T) {
			assert.Equal(t, tt.want, isHashLikeRun(tt.run))
		})
	}
}

// TestSanitizeScript_NeverEmpties は §8(縮退許容: 放送を止めない)の固定。
// 全文がハッシュ持ちの文だった場合、削除を取りやめて正規化だけの台本を返す —
// 空台本は empty script エラー = エピソード欠番に直結するため。
func TestSanitizeScript_NeverEmpties(t *testing.T) {
	in := "ハッシュは b97027b31e111 です。" +
		"次の値は e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 です。"
	got, rep := sanitizeScript(in)

	assert.True(t, rep.keptAll, "全文が落ちるので削除を取りやめる")
	assert.Empty(t, rep.dropped, "取りやめた削除はログにも出さない")
	assert.Equal(t, in, got, "正規化だけ掛けた台本が残る")
	require.NotEmpty(t, strings.TrimSpace(got))
}

func TestSanitizeScript_EmptyInputStaysEmpty(t *testing.T) {
	got, rep := sanitizeScript("")
	assert.Empty(t, got)
	assert.False(t, rep.keptAll)
	assert.False(t, rep.changed())
}

// TestSplitSentencesKeepingText pins the round-trip property the sanitizer
// depends on: 文に割っても連結すれば元通りでなければ、残した文が壊れる。
func TestSplitSentencesKeepingText(t *testing.T) {
	tests := []string{
		"一文目。二文目。",
		"終止符のない末尾",
		"感嘆符です!?続きます。",
		"　全角スペース始まり。",
		"",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, in, strings.Join(splitSentencesKeepingText(in), ""))
		})
	}
	assert.Equal(t, []string{"一文目。", "二文目。"}, splitSentencesKeepingText("一文目。二文目。"))
	assert.Equal(t, []string{"感嘆符です!?", "続きます。"}, splitSentencesKeepingText("感嘆符です!?続きます。"))
}

// TestSanitizeSegmentScript_Logs pins the「効いたら見える」requirement: 黙って
// 台本を書き換えない。落とした件数とサンプルが radio のログに残る。
func TestSanitizeSegmentScript_Logs(t *testing.T) {
	newLogger := func(buf *bytes.Buffer) *slog.Logger {
		return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	t.Run("dropped sentences are logged at WARN with a sample", func(t *testing.T) {
		var buf bytes.Buffer
		got := sanitizeSegmentScript(context.Background(), newLogger(&buf), "news",
			"前の文。"+measuredHashSentence+"後の文。")
		assert.Equal(t, "前の文。後の文。", got)
		log := buf.String()
		assert.Contains(t, log, "script sanitizer dropped sentences carrying an unreadable alphanumeric run (D-41)")
		assert.Contains(t, log, "kind=news")
		assert.Contains(t, log, "dropped_sentences=1")
		assert.Contains(t, log, "b97027b31e111", "落とした根拠の羅列がログに残る")
	})

	t.Run("character normalization is logged at INFO with per-class counts", func(t *testing.T) {
		var buf bytes.Buffer
		got := sanitizeSegmentScript(context.Background(), newLogger(&buf), "news", measuredBracketSentence)
		assert.NotContains(t, got, "（")
		log := buf.String()
		assert.Contains(t, log, "script sanitizer normalized speech-hostile characters (D-41)")
		assert.Contains(t, log, "brackets=2")
		assert.Contains(t, log, "slashes=1")
	})

	t.Run("a clean script logs nothing", func(t *testing.T) {
		var buf bytes.Buffer
		in := "きょうのニュースをお伝えします。"
		assert.Equal(t, in, sanitizeSegmentScript(context.Background(), newLogger(&buf), "intro", in))
		assert.Empty(t, buf.String())
	})

	t.Run("the never-empty degradation is logged at WARN (§8)", func(t *testing.T) {
		var buf bytes.Buffer
		in := "ハッシュは b97027b31e111 です。"
		assert.Equal(t, in, sanitizeSegmentScript(context.Background(), newLogger(&buf), "outro", in))
		assert.Contains(t, buf.String(),
			"script sanitizer would have emptied the script, keeping the normalized text as-is (§8)")
	})

	t.Run("a nil logger is tolerated", func(t *testing.T) {
		assert.NotPanics(t, func() {
			sanitizeSegmentScript(context.Background(), nil, "news", measuredBracketSentence)
		})
	})
}

// TestBuildQuizCorner_Sanitizes pins that the §7.2 復習コーナー goes through the
// sanitizer too — 出題文は当時のクラウドモデルが書いた文字列がそのまま DB に
// 残っている。定型句(出題番号・解答)は削除対象にならないこと、そして TTS が
// 読む文字列 (QuizRead) と segments に残る文字列 (Segments) が一致することを
// 同時に固定する。
//
// 【注意】この設問は**2文**なので削除が効く。1文の設問では効かない(支配的な
// 実ケースはむしろそちら) — その限界は
// TestBuildQuizCorner_SingleSentenceLimitation が明示的に固定している。
func TestBuildQuizCorner_Sanitizes(t *testing.T) {
	articleID := int64(77)
	items := []learning.Item{{
		ID: 101, Kind: learning.KindArticle, ArticleID: &articleID,
		Concept:  "gem の検証",
		Question: "gem の正当性はどう確認しますか?" + measuredHashSentence,
		Answer:   "公開されているハッシュと突き合わせます。",
	}}
	corner := BuildQuizCorner(context.Background(), items, nil)
	require.Len(t, corner.Items, 1)

	read := corner.Items[0]
	assert.NotContains(t, read.Question, "b97027b31e111", "ハッシュを含む文は落ちている")
	assert.Contains(t, read.Question, "gem の正当性はどう確認しますか?", "設問本体は残る")
	assert.Equal(t, quizReadQuestion(1, "gem の正当性はどう確認しますか?"), read.Question,
		"format.go の定型句はサニタイズの後に被さるので無傷")

	segs := corner.Segments(2)
	require.Len(t, segs, 2) // lead + 1項目
	assert.Equal(t, read.Question+"\n\n"+read.Answer, segs[1].Script,
		"音声(QuizRead)と segments の内容が一致する")
}

// TestBuildWeeklyReview_Sanitizes: 週次振り返りは固定文+concept の組み立て。
// concept 側に羅列が混じっても固定文は残る。
func TestBuildWeeklyReview_Sanitizes(t *testing.T) {
	body, ok := BuildWeeklyReview(context.Background(), learning.WeeklyReview{
		Concepts: []string{"ハッシュ検証 b97027b31e111"},
	}, nil)
	require.True(t, ok)
	assert.NotContains(t, body, "b97027b31e111")
	assert.Contains(t, body, weeklyReviewLead)
	assert.Contains(t, body, weeklyReviewClosing)
}

// TestSanitizeScript_ThresholdBoundary は N-3: 閾値の境界を**文の単位**で固定
// する。区切り文字でランが割れるかどうかまで含めた実際の挙動がここに出る
// (UUID や IPv6 はハイフン・コロンで割れるため、全体は長くても落ちない)。
//
// 入力を2文にしてあるのは意図的: 1文しかないテキストは、落とすと空になるので
// §8 の never-empty が効いて原文のまま残る(sanitizeScript のドキュメント参照)。
// 閾値そのものを見たいこの表では、その経路に入らないようにしている。
func TestSanitizeScript_ThresholdBoundary(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		wantDropped bool
	}{
		// 落ちる側。誤爆しうると分かっていて許容する — 誤爆の代償は1文が
		// 消えることで、見逃しの代償は読み上げ不能な羅列が音声に乗ること。
		{name: "product name with version digits", token: "Windows11Pro22H2", wantDropped: true},
		{name: "ISBN with the prefix glued on", token: "ISBN9784873119045", wantDropped: true},
		{name: "ISO 8601 basic timestamp", token: "20260925T043014Z", wantDropped: true},
		// 残る側。見逃すと分かっていて許容する(UUID を拾う特別扱いは入れない)。
		{name: "UUID splits on hyphens, longest run is digits only",
			token: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "IPv6 splits on colons", token: "2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
		{name: "short base64 (16 chars, digit-poor)", token: "aGVsbG8gd29ybGQh"},
		{name: "YouTube video ID (11 chars)", token: "dQw4w9WgXcQ"},
		{name: "abbreviated SHA (7 chars)", token: "a1b2c3d"},
		// 正しく残る側。
		{name: "CVE ID", token: "CVE-2026-12345"},
		{name: "architecture", token: "x86_64"},
		{name: "RFC number", token: "RFC9110"},
		{name: "library version", token: "OpenSSL3.0.14"},
		{name: "model name", token: "gemini-2.5-flash"},
		{name: "cipher suite", token: "AES256GCM"},
		{name: "spec year", token: "ECMAScript2015"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const lead = "きょうのニュースです。"
			in := lead + "値は " + tt.token + " です。"
			got, rep := sanitizeScript(in)
			assert.False(t, rep.keptAll, "2文あるので never-empty 経路には入らない")
			if tt.wantDropped {
				assert.Equal(t, lead, got)
				require.Len(t, rep.dropped, 1)
				return
			}
			assert.Equal(t, in, got)
			assert.Empty(t, rep.dropped)
		})
	}
}

// TestSanitizeScript_ClosingBracketBeforeParticle は N-4 の例外表を固定する。
// 閉じ括弧の直後が1文字助詞のときだけ読点を出さない。「から」「まで」のような
// 複数文字の助詞を足すと語頭 (か / ま) で誤爆するので、この表は増やさない。
func TestSanitizeScript_ClosingBracketBeforeParticle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"CI/CD（継続的インテグレーション／デリバリー）の設定です。", "CI/CD、継続的インテグレーション、デリバリーの設定です。"},
		{"仕様（RFC9110）は公開されています。", "仕様、RFC9110は公開されています。"},
		{"文書（草案）を確認します。", "文書、草案を確認します。"},
		{"対象（本番環境）が停止しました。", "対象、本番環境が停止しました。"},
		{"設定（既定値）に戻します。", "設定、既定値に戻します。"},
		{"手順（付録）で説明します。", "手順、付録で説明します。"},
		{"結果（成功）と記録されました。", "結果、成功と記録されました。"},
		{"条件（例外）も同じです。", "条件、例外も同じです。"},
		{"連携（外部）へ送ります。", "連携、外部へ送ります。"},
		{"項目（追加）や変更があります。", "項目、追加や変更があります。"},
		// 助詞以外が続く場合は読点のまま(間を作るのが自然)。
		{"補足（詳細は番組情報欄）。次の文です。", "補足、詳細は番組情報欄。次の文です。"},
		{"用語（略語）。", "用語、略語。"},
		// 「から」「まで」は例外表に入れない — 読点が残る。
		{"開始（初回）から説明します。", "開始、初回、から説明します。"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, _ := sanitizeScript(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFixedPhrasesSurviveSanitizing は N-6: D-37 の境界に置く見張り。
// format.go の承認済み文案は、サニタイズを通しても1文字も変わってはいけない。
//
// 現在の定型句には全角括弧も異体ハイフンも含まれないので今は無害だが、将来
// format.go に「（…）」入りの文言を足すと承認済みの文案が黙って書き換わる。
// TestFixedPhrases はサニタイズ**前**の文字列を見るため、それを検知できない。
// ここが落ちたら、直すのは format.go 側の文言である(サニタイザを緩めない)。
func TestFixedPhrasesSurviveSanitizing(t *testing.T) {
	for _, w := range fixedWordings() {
		t.Run(w.name, func(t *testing.T) {
			got, rep := sanitizeScript(w.whole)
			assert.Equal(t, w.whole, got, "format.go の文言がサニタイズで書き換わっている")
			assert.False(t, rep.changed())
		})
	}
}

// TestBuildQuizCorner_SingleSentenceLimitation は**現在の限界**を明示的に固定
// する(レビュー指摘 N-1 / N-2)。1文しかない出題文は、羅列を含んでいても
// §8 の never-empty(文字列単位)が効いて原文のまま残る — そして D-19 の相乗り
// クイズは1〜2文が普通なので、これが支配的なケースである。
//
// このテストは「正しい姿」ではなく「いま出荷している姿」を写したもの。追い PR
// で (a) QuizDraft の生成時点でのサニタイズ、(b) 項目が空になるならその項目を
// 当日の出題から外す縮退、を入れた時点で書き換えること。
func TestBuildQuizCorner_SingleSentenceLimitation(t *testing.T) {
	articleID := int64(77)
	items := []learning.Item{{
		ID: 101, Kind: learning.KindArticle, ArticleID: &articleID,
		Question: "activesupport‑7.2.4.gem のハッシュ b97027b31e111 は何を示しますか?",
		Answer:   "gem の正当性です。",
	}}
	corner := BuildQuizCorner(context.Background(), items, nil)
	require.Len(t, corner.Items, 1)

	assert.Contains(t, corner.Items[0].Question, "b97027b31e111",
		"1文なので削除は効かない(既知の限界、追い PR で対応)")
	assert.NotContains(t, corner.Items[0].Question, "\u2011",
		"正規化は1文でも効く")
}
