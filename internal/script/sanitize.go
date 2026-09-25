package script

import (
	"context"
	"log/slog"
	"strings"
)

// --- 台本サニタイズ (D-41 改訂, 2026-09-25) ---
//
// prompts/common.tmpl の persona は既に「英数字の羅列を避ける」「記号を書か
// ない」と指示しているが、実運用の主力プロバイダである gpt-oss
// (openai/gpt-oss-120b) はこれを守らない。D-41 (2026-08-15) の「まず素で運用
// して様子を見る」裁定は Groq の発火が日に数件という前提に立っていたが、
// Gemini 無料枠 (20req/日) が毎日枯れるため実際にはほぼ全セグメントが Groq に
// 落ちている。前提が変わったので、指示を強める方向ではなく Go 側で決定論的に
// 落とす方向へ改める(2026-09-25 実測の逸脱例はこのファイルの回帰テストに
// そのまま入っている)。
//
// 適用範囲はセグメント台本(= 音声になり segments.script に残る文字列)のみ。
// ショーノートには掛けない — URL を意図的に含み、音声にならないため。
// 呼び出しは cutQuizSection / stripQuizLeak の後でなければならない(マーカー
// 検出を乱さない)。
//
// 設計方針は「狭く落とす」: 落とすのはハッシュ・チェックサムのような読み上げ
// 不能な英数字の羅列を含む**文**だけで、版数 (Rails 8.1.4 / v1.16.6 / 0.57.0)
// や一般的な語 (SHA-256) は必ず残す。記号側は置換のみで、日本語の語句は一切
// 挿入しない(挿入する場合 D-37 により format.go に置く必要がある — 読点は
// 「言い回し」ではなく句読点なのでここに置いてよい)。

const (
	// hashRunMinLen は「読み上げ不能な羅列」とみなす英数字ランの下限。
	// 実測の断片 "b97027b31e111"(13文字)を捕まえ、"PostgreSQL17"(12文字)の
	// ような普通の語+版数は下の数字比率で外す。
	hashRunMinLen = 12
	// hashRunDigitRatio は hashRunMinLen〜opaqueRunMinLen の帯で要求する数字の
	// 比率。16進ハッシュは期待値 62.5%、実測断片は 77% (13文字中10文字)。
	// いっぽう "PostgreSQL17" は 16.7%、"Kubernetes1" は 9% で残る。
	hashRunDigitRatio = 0.3
	// opaqueRunMinLen 以上は数字比率を問わず羅列とみなす(base64url のトークン
	// は数字比率が 1/6 程度しかないが、20文字を超える英数字の連なりを読み上げ
	// られる形で発音する手段はない)。
	opaqueRunMinLen = 20
	// sanitizeSampleRunes はログに出す抜粋の長さ。台本全文をログに流さない。
	sanitizeSampleRunes = 80
)

// sanitizeTerminators は文の切れ目。tts.SplitSentences と同じ集合だが、この
// パッケージは tts に依存しない(層の向きが逆)。あちらは合成単位を作るために
// 改行を捨てて trim するのに対し、こちらは台本の見た目を保ったまま文を落とす
// ため、実装を共有せず意図的に別に持つ。
const sanitizeTerminators = "。!?！？"

// speechHostileChars は音声に乗せる前に潰す文字。値は置換後の文字列で、
// 日本語の語句は含まない(記号→記号/読点のみ)。
var speechHostileChars = map[rune]string{
	// 異体ハイフン・ダッシュ → 通常のハイフン。U+2011 は gpt-oss が
	// "SHA‑256" / "activesupport‑7.2.4.gem" で実際に出してくる(D-41)。
	// U+30FC(長音記号「ー」)は日本語の語の一部なので絶対に含めない。
	'‐': "-", // HYPHEN
	'‑': "-", // NON-BREAKING HYPHEN
	'‒': "-", // FIGURE DASH
	'–': "-", // EN DASH
	'—': "-", // EM DASH
	'―': "-", // HORIZONTAL BAR
	'−': "-", // MINUS SIGN
	'－': "-", // FULLWIDTH HYPHEN-MINUS
	// 全角括弧 → 読点。読み上げでは括弧そのものを読ませるのではなく、前後に
	// 間を作るのが自然(実測: 「（継続的インテグレーション／デリバリー）」)。
	'（': "、",
	'）': "、",
	// 全角スラッシュ → 読点。並列の区切りとしてそのまま間に変わる。
	'／': "、",
	// 全角スペース → 半角スペース (D-41 の当初観測)。
	'　': " ",
}

// sanitizeReport は1本の台本に対して何をしたかの記録。ログに出すためだけの
// 構造体で、黙って書き換えないという要件(「効いたら見える」)を満たす。
type sanitizeReport struct {
	hyphens  int // 異体ハイフン・ダッシュの置換数
	brackets int // 全角括弧の置換数
	slashes  int // 全角スラッシュの置換数
	spaces   int // 全角スペースの置換数
	// dropped は落とした文(ログ用に先頭のみ保持)。
	dropped []string
	// runs は落とす判断の根拠になった英数字ラン(文と1対1)。
	runs []string
	// droppedRunes は落とした文字数。
	droppedRunes int
	// keptAll は「全文が落ちるので文の削除を取りやめた」ことを示す (§8)。
	keptAll bool
}

// changed reports whether anything at all was rewritten.
func (r sanitizeReport) changed() bool {
	return r.hyphens+r.brackets+r.slashes+r.spaces > 0 || len(r.dropped) > 0
}

// sanitizeScript は台本1本を決定論的に整える。手順は
//
//	記号の正規化 → ハッシュを含む文の削除 → 句読点の整え
//
// の順。文の途中から羅列だけを抜くと「ハッシュは となっています」のように
// 日本語が壊れるため、削除は必ず文単位で行う。
//
// 全文が落ちる場合は削除を取りやめ、記号を正規化しただけの台本を返す
// (§8 縮退許容: 空台本はエピソード欠番に直結するため、読み上げの不格好さより
// 放送の継続を採る)。report.keptAll が立ち、呼び出し側が WARN を出す。
//
// 【既知の限界】この never-empty は**渡された文字列の単位**で効く。したがって
// 1文しかないテキストは、羅列を含んでいても必ず原文のまま残る。セグメント台本
// (intro / news / outro / book_review / review) は複数文なので実害は無いが、
// クイズ項目の question / answer は1〜2文が普通で、削除は事実上効かない。
// クイズ側は「項目のフィールドが空になるなら**その項目を当日の出題から外す**」
// (§5.2 の既存縮退と同方向) という別の縮退が要る — 相乗りクイズの生成時点
// (QuizDraft) でのサニタイズと併せて追い PR で対応する。
func sanitizeScript(text string) (string, sanitizeReport) {
	normalized, rep := normalizeSpeechChars(text)

	var lines []string
	for _, line := range strings.Split(normalized, "\n") {
		var kept strings.Builder
		for _, sentence := range splitSentencesKeepingText(line) {
			run, hit := hashLikeRun(sentence)
			if !hit {
				kept.WriteString(sentence)
				continue
			}
			rep.dropped = append(rep.dropped, strings.TrimSpace(sentence))
			rep.runs = append(rep.runs, run)
			rep.droppedRunes += len([]rune(sentence))
		}
		lines = append(lines, kept.String())
	}
	out := tidyPunctuation(strings.Join(lines, "\n"))

	if strings.TrimSpace(out) == "" && strings.TrimSpace(normalized) != "" {
		// 全文がハッシュ持ちの文だった。空台本は empty script エラー = 当日
		// スキップになるので、削除を捨てて正規化だけの台本で放送する (§8)。
		rep.keptAll = true
		rep.dropped = nil
		rep.runs = nil
		rep.droppedRunes = 0
		return tidyPunctuation(normalized), rep
	}
	return out, rep
}

// particlesAfterBracket は閉じ括弧の直後に来たとき読点を出さない助詞
// (レビュー指摘 N-4)。「（…）の設定」が「…、の設定」になると助詞の前で間が
// 空いて不自然に聞こえる実測への対応で、閉じ括弧を単に消すだけにする。
//
// 1文字の助詞だけを列挙する。「から」「まで」のような複数文字の助詞は入れない
// — 先頭1文字 (か / ま) を見るだけでは「開発が…」「まとめると…」のような語頭と
// 区別できず誤爆するため。ここを増やすときは同じ基準で判断すること。
var particlesAfterBracket = map[rune]bool{
	'の': true, 'は': true, 'を': true, 'が': true, 'に': true,
	'で': true, 'と': true, 'も': true, 'へ': true, 'や': true,
}

// normalizeSpeechChars replaces the speech-hostile characters and counts the
// replacements per class (ログ用).
func normalizeSpeechChars(text string) (string, sanitizeReport) {
	var rep sanitizeReport
	var sb strings.Builder
	sb.Grow(len(text))
	runes := []rune(text)
	for i, r := range runes {
		repl, ok := speechHostileChars[r]
		if !ok {
			sb.WriteRune(r)
			continue
		}
		switch r {
		case '（', '）':
			rep.brackets++
			if r == '）' && i+1 < len(runes) && particlesAfterBracket[runes[i+1]] {
				// 「）の」「）は」… は読点を出さず括弧を消すだけにする (N-4)。
				continue
			}
		case '／':
			rep.slashes++
		case '　':
			rep.spaces++
		default:
			rep.hyphens++
		}
		sb.WriteString(repl)
	}
	return sb.String(), rep
}

// splitSentencesKeepingText splits one line into sentences with their
// terminators attached, WITHOUT trimming or dropping anything: concatenating
// the result reproduces the input byte for byte. 連続する終止符 ("!?") は
// 同じ文に残る。終止符のない末尾も1文として返す。
func splitSentencesKeepingText(line string) []string {
	var out []string
	var sb strings.Builder
	terminated := false
	for _, r := range line {
		isTerm := strings.ContainsRune(sanitizeTerminators, r)
		if terminated && !isTerm {
			out = append(out, sb.String())
			sb.Reset()
		}
		sb.WriteRune(r)
		terminated = isTerm
	}
	if sb.Len() > 0 {
		out = append(out, sb.String())
	}
	return out
}

// hashLikeRun reports the first unreadable alphanumeric run in s, if any.
// 判定は isHashLikeRun。区切り文字(ハイフン・ドット・空白・日本語)でランを
// 切るので、"Rails 8.1.4" や "v1.16.6"、"SHA-256" は短いランに分解されて残る。
func hashLikeRun(s string) (string, bool) {
	var run strings.Builder
	check := func() (string, bool) {
		v := run.String()
		run.Reset()
		if isHashLikeRun(v) {
			return v, true
		}
		return "", false
	}
	for _, r := range s {
		if isASCIIAlnum(r) {
			run.WriteRune(r)
			continue
		}
		if v, ok := check(); ok {
			return v, true
		}
	}
	return check()
}

// isHashLikeRun decides whether one alphanumeric run is a hash/checksum-like
// string that must never be read aloud. 英字を1文字も含まないランは残す
// (長い数字は年号・ID として読み上げ可能で、版数の誤爆も避けたい)。
func isHashLikeRun(run string) bool {
	n := len(run) // ASCII のみなのでバイト長 = 文字数
	if n < hashRunMinLen {
		return false
	}
	var digits, letters int
	for i := 0; i < n; i++ {
		switch c := run[i]; {
		case c >= '0' && c <= '9':
			digits++
		default:
			letters++
		}
	}
	if letters == 0 || digits == 0 {
		return false
	}
	if n >= opaqueRunMinLen {
		return true
	}
	return float64(digits) >= hashRunDigitRatio*float64(n)
}

func isASCIIAlnum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// tidyPunctuation cleans up the artifacts of the two rewrites above: the
// 読点 that full-width brackets leave next to other punctuation, and the
// double spaces / dangling 読点 a dropped sentence can expose. 文字の削除と
// 空白の畳み込みだけで、語句の挿入はしない。行末の空白は**削らない** —
// 音声には影響しない一方、サニタイザが台本へ加える変更は少ないほどよく、
// format.go の文言(末尾に空白を持つ見出しがある)を書き換えない保証にもなる
// (N-6 の TestFixedPhrasesSurviveSanitizing が実際にこれを検知した)。
func tidyPunctuation(s string) string {
	for _, pair := range [][2]string{
		{"、、", "、"},
		{"、。", "。"},
		{"、！", "！"},
		{"、？", "？"},
		{"、!", "!"},
		{"、?", "?"},
		{"。、", "。"},
		{"！、", "！"},
		{"？、", "？"},
		{" 、", "、"},
		{"、 ", "、"},
		{"  ", " "},
	} {
		for strings.Contains(s, pair[0]) {
			s = strings.ReplaceAll(s, pair[0], pair[1])
		}
	}
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		// 行頭に残った読点(「（…」で始まる行)は読み上げに不要。
		line = strings.TrimLeft(line, "、")
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// sanitizeSegmentScript は sanitizeScript を掛け、変化があればログに残す。
// セグメント台本の生成箇所はすべてこの関数を通す(TTS と segments テーブルの
// 双方の手前 = 生成直後に1回だけ掛けることで、音声と DB の内容が構造的に
// 一致する)。nil logger は slog.Default()。
func sanitizeSegmentScript(ctx context.Context, logger *slog.Logger, kind, text string) string {
	if logger == nil {
		logger = slog.Default()
	}
	out, rep := sanitizeScript(text)
	if !rep.changed() && !rep.keptAll {
		return out
	}
	if rep.hyphens+rep.brackets+rep.slashes+rep.spaces > 0 {
		logger.InfoContext(ctx, "script sanitizer normalized speech-hostile characters (D-41)",
			slog.String("kind", kind),
			slog.Int("hyphens", rep.hyphens),
			slog.Int("brackets", rep.brackets),
			slog.Int("slashes", rep.slashes),
			slog.Int("full_width_spaces", rep.spaces))
	}
	if len(rep.dropped) > 0 {
		logger.WarnContext(ctx, "script sanitizer dropped sentences carrying an unreadable alphanumeric run (D-41)",
			slog.String("kind", kind),
			slog.Int("dropped_sentences", len(rep.dropped)),
			slog.Int("dropped_chars", rep.droppedRunes),
			slog.String("run_sample", truncateRunes(rep.runs[0], sanitizeSampleRunes)),
			slog.String("sentence_sample", truncateRunes(rep.dropped[0], sanitizeSampleRunes)))
	}
	if rep.keptAll {
		logger.WarnContext(ctx, "script sanitizer would have emptied the script, keeping the normalized text as-is (§8)",
			slog.String("kind", kind),
			slog.String("sample", truncateRunes(strings.TrimSpace(out), sanitizeSampleRunes)))
	}
	return out
}

// truncateRunes shortens a log sample to n runes (台本全文をログへ流さない)。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
