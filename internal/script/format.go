package script

import (
	"fmt"
	"strings"
	"time"

	"catchup-feed/internal/repository"
)

// --- 番組フォーマット (D-37) ---
//
// 台本に出る言い回し(= VOICEVOX が読み上げる文字列と、そのままの表記で
// 書かせたい定型句)は、すべてこのファイルに置く。プロンプトに渡す定型句も、
// テンプレート生成の固定文(復習コーナー・週次振り返り・記事ゼロの日)も、
// ショーノートの見出しも例外にしない。
//
// これは D-37 (9) の結論そのものである: 改訂前は prompts/*.tmpl 4本 +
// quizcorner.go + weeklyreview.go の6箇所に文言が散っており、「番組として
// 統一されている」状態を維持できなかった。文言の追加・変更は必ずここへ入れ、
// 呼び出し側(prompts.go / generator.go / quizcorner.go / weeklyreview.go)は
// ここを参照するだけにすること。
//
// D-37 (10) の段階的引き上げに備えて、定型句は「プロンプトへ渡す文字列」と
// 「Go 側で機械的に前置する文字列」のどちらにもそのまま使える素の文字列で
// 定義してある。-dry-run で LLM が守らない箇所が判明したら、渡すのをやめて
// 生成結果の前後に連結すればよく、文言の書き直しは発生しない。

// spokenShowName は「読み上げ用の番組名」。台本・プロンプト・固定文で番組名を
// 出すときは必ずこれを使う (D-37 (3))。
//
// 【重要】radio.Config.ShowName(環境変数 RADIO_SHOW_NAME、既定
// "catchup-feed")とは別物であり、意図的に連動していない:
//
//   - RADIO_SHOW_NAME … エピソードタイトルと mp3 の ID3 タグ用。過去回との
//     連続性を優先して ASCII のまま据え置く(D-37 (3))。
//   - spokenShowName … 音声になる文字列用。ASCII を TTS に渡すと読みが崩れる
//     ため(実測: intro/outro 48件中33件が "catchup-feed" のまま合成されて
//     いた)、カナで固定する。
//
// 両者が独立しているということは、RADIO_SHOW_NAME を変えても読み上げ名は
// 変わらないということである。番組名を改名するときは必ず両方を直すこと。
// 逆に、この定数を環境変数化してはいけない(D-37: 新しい環境変数を増やさない)。
const spokenShowName = "キャッチアップフィード"

// programFormat は全プロンプトデータ構造体に埋め込む共通部品。Show() を
// メソッドとして生やすことで、テンプレート側の {{.Show}} が常に
// spokenShowName に解決される — 呼び出し側が番組名フィールドの設定を
// 忘れる余地も、ASCII の番組名を渡してしまう余地も構造的に無くなる。
type programFormat struct{}

// Show returns the spoken program name for prompt templates.
func (programFormat) Show() string { return spokenShowName }

// weekdayNames は time.Weekday 順(日曜始まり)の読み上げ用曜日名。
var weekdayNames = [...]string{"日曜日", "月曜日", "火曜日", "水曜日", "木曜日", "金曜日", "土曜日"}

// spokenDate renders the broadcast date as "8月12日水曜日" (D-37 (5))。
//
// 曜日は Go 側で算出する。改訂前の formatDate は "2026年8月12日" しか渡して
// おらず、曜日はモデルに日付から導出させていた(実測では5日とも正しかったが、
// 保証の無い推論に番組の定型句を預ける理由が無い)。年を落としたのは、毎朝の
// ニュース番組で放送日の年を読み上げる必然性が無いため。
//
// 引数は放送日のタイムゾーン(radio.Config.Location = JST)に変換済みの時刻で
// あること。呼び出し元の Pipeline.Run が now.In(loc) を通してから
// GenerateEpisode / QuizOnlyIntro へ渡しているため、ここでは受け取った時刻を
// そのまま使う(このパッケージが場所を知る必要は無い)。
func spokenDate(t time.Time) string {
	return fmt.Sprintf("%d月%d日%s", int(t.Month()), t.Day(), weekdayNames[t.Weekday()])
}

// cornerNameBySlug は sources.category(DB のスラッグ)→ 読み上げ用コーナー名
// の対応表 (D-37 (4))。モデルに訳させない: 「AI」と「人工知能」が日によって
// 混在する実測事故の原因がそれだった。
var cornerNameBySlug = map[string]string{
	"ai":        "AI",
	"community": "コミュニティ",
	"dev":       "開発",
	"infra":     "インフラ",
	"security":  "セキュリティ",
}

// cornerName maps a sources.category slug to its spoken corner name.
//
// 【未知のスラッグの扱い】対応表に無いスラッグはそのまま返す。カテゴリは
// ダッシュボードから自由に追加できるため、未知の値はいずれ必ず来る。そこで
//
//	(a) そのまま返す        … 台本に ASCII が出るが、情報は失われない
//	(b) 「そのほか」等に寄せる … カナだが、別カテゴリが同名のコーナーに潰れ、
//	                            対応表の追従漏れが誰にも気づかれなくなる
//
// のうち (a) を採る。ASCII が読み上げに出るのは D-37 が潰したかった事象その
// ものだが、これは「対応表を更新しろ」という可聴かつ可観測な欠陥として現れる
// のに対し、(b) は静かに間違い続ける。generator 側は未知スラッグを WARN で
// 記録するので、ログを見て対応表に足せば済む。
//
// 恒久的な新カテゴリを足すときは、この表に1行足すこと。
func cornerName(slug string) string {
	if name, ok := cornerNameBySlug[slug]; ok {
		return name
	}
	return slug
}

// isKnownCorner reports whether the slug is in the spoken-name table. Used by
// the generator to WARN once per unknown slug (see cornerName).
func isKnownCorner(slug string) bool {
	_, ok := cornerNameBySlug[slug]
	return ok
}

// cornerNames returns the distinct spoken corner names in on-air order.
// plan.Plan already groups the featured articles by category, so the order
// here is the running order of the corners.
func cornerNames(articles []repository.RadioArticle) []string {
	var out []string
	seen := make(map[string]bool, len(articles))
	for _, a := range articles {
		if !seen[a.Category] {
			seen[a.Category] = true
			out = append(out, cornerName(a.Category))
		}
	}
	return out
}

// --- 定型句 (D-37 (6)) ---
//
// ここから下は「そのままの表記で台本に出したい文」。プロンプトに渡して LLM に
// 書かせるのが現状の方式で、守られない箇所が出たら Go 側の機械的前置に
// 格上げする(そのとき文言はここのまま再利用する)。

// openingLead is the fixed first sentence of the opening (D-37 (6))。
func openingLead(date time.Time) string {
	return fmt.Sprintf("おはようございます。%s、%sの放送です。", spokenShowName, spokenDate(date))
}

// openingHandoff is the fixed last sentence of the opening.
const openingHandoff = "それでは、最初のニュースです。"

// newsLeadContinued is the fixed first sentence of a news segment that stays
// inside the same corner as the previous article.
const newsLeadContinued = "続いてのニュースです。"

// cornerLead is the fixed first sentence of a segment that opens a corner —
// ニュースだけでなく復習コーナーも書籍コーナーもこの1関数を通る。番組の
// 「コーナーの入り方」はこれ1つで、ここを変えると全コーナーが揃って変わる
// (D-37 (9))。ニュースの1本目にもこれを使う — オープニングはコーナー名を
// 紹介するだけで、最初のコーナーに入る宣言はしていないため。
//
// 【この関数に引数やフラグを足さないこと】コーナーごとに違う枠(例: 書籍だけ
// 別の入り方にしたい)が必要になったら、ここに if を足すのではなく format.go
// に別の関数を足すこと。分岐を増やすと「どのコーナーがどの枠か」がこの1関数
// の中に埋もれ、集約の意味が失われる。
func cornerLead(corner string) string {
	return fmt.Sprintf("ここからは、%sのコーナーです。", corner)
}

// bookReviewLead is the fixed first sentence of the §7.3 書籍コーナー. It is
// the same corner-opening 定型句 as every other corner; bookreview.tmpl gets
// it as a template variable so the wording exists in exactly one place.
func bookReviewLead() string { return cornerLead("いま読んでいる本") }

// newsLead picks the fixed opening sentence for articles[i] (D-37 (6)):
// コーナーが切り替わるなら「ここからは、○○のコーナーです。」、同じコーナーが
// 続くなら「続いてのニュースです。」。
//
// 切替判定は直前の記事とのカテゴリ比較で足りる — Plan がカテゴリ順に並べて
// いるので、同じカテゴリが飛び飛びに現れることはない。1本目は必ずコーナー
// 切替扱いにする(オープニングはコーナー名を紹介するだけで、最初のコーナーに
// 入る宣言はしていない)。
func newsLead(articles []repository.RadioArticle, i int, corner string) string {
	if i > 0 && articles[i-1].Category == articles[i].Category {
		return newsLeadContinued
	}
	return cornerLead(corner)
}

// signOff is the program's closing signature, shared by every episode kind so
// the last thing the listener hears is always the same (D-37 (7))。
const signOff = spokenShowName + "、また明日お会いしましょう。"

// closingSignOff is the fixed last sentence of the public closing: the show
// notes pointer plus the signature.
const closingSignOff = "詳しいリンクは番組情報欄に掲載しています。" + signOff

// --- 復習コーナー(§7.2、テンプレート固定文) ---
//
// LLM を呼ばない。学習内容をクラウドに送らない(§10)ためであり、クオータを
// 使わない(§12-3)ためでもある。文言を変えても LLM 化はしないこと。

// quizCornerLead is the 復習コーナー introduction read (§7.2). コーナーの
// 入り方はニュースのコーナーと同じ定型句を使う — 言い回しの実体は
// cornerLead ただ1つ (D-37 (9))。
func quizCornerLead(itemCount int) string {
	return cornerLead("復習") + fmt.Sprintf(
		"これまでの放送でお伝えした内容から、今日は%d問おさらいします。問題のあとに少し間をあけますので、頭の中で答えてみてください。",
		itemCount)
}

// quizReadQuestion / quizReadAnswer are the numbering and answer cues wrapped
// around the verbatim learning_items fields. They are part of what goes on air
// and therefore part of the archived segment script (§7.2-4)。
func quizReadQuestion(number int, question string) string {
	return fmt.Sprintf("第%d問。%s", number, question)
}

func quizReadAnswer(answer string) string { return "答え。" + answer }

// quizOnlyIntroScript is the fixed opening of a quiz-only private episode
// (§7.1: 記事ゼロの日)。通常回と同じ冒頭定型句で始め、記事が無いことと
// 復習だけを届けることを続ける。
func quizOnlyIntroScript(date time.Time) string {
	return openingLead(date) + "今朝は新しい記事のお届けはありません。かわりに、これまでの放送のおさらいをお届けします。"
}

// quizOnlyOutroScript is the fixed closing of a quiz-only private episode。
// 番組情報欄のリンク案内は付けない(この回に記事のリンクは無い)が、締めの
// 一文は通常回と共有する。
const quizOnlyOutroScript = "今日のおさらいは以上です。" + signOff

// quizOnlyShowNotesBase opens the quiz-only episode's show notes.
const quizOnlyShowNotesBase = "今日は新しい記事がなかったため、復習のみお届けしました。"

// --- ショーノートの見出し(§4 episodes.show_notes) ---
//
// 音声にはならないが、通知メールとポッドキャストアプリの番組情報欄に出る
// 「番組の文言」なので、読み上げ文と同じくここに集約する。

const (
	// 公開版・私的版に共通の記事セクション (§6-1)。
	notesFeaturedHeading = "今日紹介した記事:\n"
	notesOverflowHeading = "\n紹介しきれなかった記事:\n"

	// notesVoicevoxCreditPrefix is the U-13 credit line prefix: VOICEVOX の
	// 利用規約上、生成音声の配布には「VOICEVOX:話者名」の表記が必須であり、
	// 配信する全エピソードがこの行を持つ(§6-7)。表記を変えるとクレジット
	// 要件を外しうるので、変更時は U-13 を読み直すこと。
	notesVoicevoxCreditPrefix = "\n\n音声合成: VOICEVOX:"

	// 私的版のみの学習セクション (§7.5)。
	quizNotesHeading     = "\n\n今日の復習:\n"
	quizNotesGradePrefix = "\n採点はこちら: "

	weeklyNotesHeading           = "\n\n今週の学び:\n"
	weeklyNotesGraduatedFormat   = "卒業した項目: %d件"
	weeklyNotesReintroducePrefix = "\nもう一度おさらい: "
)

// --- 週次振り返り(§7.4、テンプレート固定文) ---
//
// 復習コーナーと同じ理由で LLM を呼ばない(§10 / §12-3)。concepts /
// graduations / reintroduction のどれが欠けても自然に読めるよう、文単位で
// 組み立てる。

const (
	weeklyReviewLead    = "ここで、今週の学びを振り返ります。"
	weeklyReviewClosing = "来週も少しずつ続けていきましょう。"
)

// weeklyReviewConcepts is the "what was learned" sentence. 件数を末尾に置く
// のは、項目が1件のときも任意の件数のときも同じ形で自然に読めるため。
func weeklyReviewConcepts(concepts []string) string {
	return fmt.Sprintf("今週学んだ項目は、%s の%sです。",
		strings.Join(concepts, "、"), itemCount(len(concepts)))
}

// weeklyReviewGraduated is the graduation sentence.
//
// 【重要】卒業した項目が「今週学んだ項目」の部分集合であるかのような書き方
// (「このうちN個が…」)をしてはいけない。learning.WeeklyReview の2つの材料は
// 母集合が違う:
//
//   - Concepts        … created_at が直近7日の項目
//   - GraduatedCount  … retired_at が直近7日 かつ ラダー完走した項目
//
// D-18 の既定ラダー [1,7,30] では生成から卒業まで最短でも38日かかるため、
// 既定設定ではこの2つは必ず素集合になる。部分集合を主張すると毎週かならず
// 事実に反する読みになり、件数が乖離すれば「学んだ項目は1つ。このうち10個が
// 卒業」のような破綻も起きる。渡されていない事実を作らないという D-37 (5) の
// 方針は、LLM だけでなくテンプレート側にも同じく効く。
//
// afterConcepts が変えてよいのは【文頭だけ】。事実を述べる部分は
// weeklyReviewGraduatedBody 1箇所から両分岐が共有しており、分岐が事実を
// 書き換える余地は構造的に無い(テストで等式として固定してある)。
func weeklyReviewGraduated(count int, afterConcepts bool) string {
	return weeklyReviewGraduatedOpening(afterConcepts) + weeklyReviewGraduatedBody(count)
}

// weeklyReviewGraduatedBody is the factual half of the graduation sentence —
// identical in both branches.
func weeklyReviewGraduatedBody(count int) string {
	return fmt.Sprintf("%sの項目が繰り返しの復習を経て定着し、復習リストを卒業しました。", itemCount(count))
}

// weeklyReviewGraduatedOpening picks the sentence opening only.
//
// concepts の文に続くときは「今週は」を落として接続詞だけで受ける: リードが
// 「ここで、今週の学びを振り返ります。」、concepts が「今週学んだ項目は、…」
// なので、ここでも言うと「今週」が3文続き、落ち着いたアナウンサー調
// (D-37 (2))から外れる。週の範囲はリードが確定させているので事実は落ちない。
// 先行文が無いときは、この文が週の範囲を示す必要があるので「今週は」を残す。
func weeklyReviewGraduatedOpening(afterConcepts bool) string {
	if afterConcepts {
		return "また、"
	}
	return "今週は"
}

// weeklyReviewReintroduced is the "went back to the list" sentence. 先行文が
// 無い週は逆接の「いっぽうで」を落とす。
func weeklyReviewReintroduced(concept string, afterOther bool) string {
	if afterOther {
		return fmt.Sprintf("いっぽうで「%s」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。", concept)
	}
	return fmt.Sprintf("「%s」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。", concept)
}

// itemCount renders a count for the 週次振り返り reads. 1〜9 は承認済み文案の
// とおり「つ」で数え、10 以上は「10つ」が日本語にならないため「個」に切り替える
// (週の学習項目は 1日3問 × 7日 で二桁に届きうる)。
func itemCount(n int) string {
	if n >= 1 && n <= 9 {
		return fmt.Sprintf("%dつ", n)
	}
	return fmt.Sprintf("%d個", n)
}
