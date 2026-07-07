package script

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// OllamaLLM is the LOCAL-only text generator behind book_review and its quiz
// (§7.3 / §5.3, C-12: 書籍は私的データ). Its 2-value Generate signature is
// deliberately NARROWER than script.LLM, which also returns the winning
// provider name: *summarizer.Chain satisfies script.LLM but its Generate
// returns THREE values, so it can NOT satisfy this interface. The Gemini→Groq
// cloud chain is therefore excluded from the book path at COMPILE TIME —
// book-derived text can never reach a cloud provider (§12-4: プロバイダ連鎖
// との構造的分離、テストで固定). Only *summarizer.Ollama satisfies it.
type OllamaLLM interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// BookChunk is one source excerpt for book_review (book_chunks, position 順).
type BookChunk struct {
	Position int
	Content  string
}

// BookReviewResult is the generated book_review segment plus an optional book
// quiz draft (§5.3). Quiz is nil when the quiz section was missing or
// unparseable — the review itself still ships (§5.3 縮退: book_review 本体は
// 出す、書籍クイズなし).
type BookReviewResult struct {
	// Script is the read-aloud book_review text (segment script, §7.3).
	Script string
	// Quiz is the piggybacked book quiz, or nil (縮退).
	Quiz *BookQuizDraft
}

// BookQuizDraft is a kind='book' learning item candidate (§5.3): the same
// three read-aloud fields as an article quiz. It carries no provider field —
// a book item's provider is 'ollama' by construction (this generator only
// ever calls the local model) and the pipeline pins it (learning.NewItem
// enforces provider='ollama' for kind='book', §12-4).
type BookQuizDraft struct {
	Concept  string
	Question string
	Answer   string
}

// bookQuizMarker separates the read-aloud review from the piggybacked quiz in
// the single Ollama completion (§5.3: 同じ Ollama 呼び出しに相乗り). Same
// loud-marker discipline as the article quiz (quizSectionMarker): JSON breaks
// too easily, a unique marker line fails loudly and lets the caller degrade.
// Everything before the FIRST exact occurrence is the book_review script.
const bookQuizMarker = "===書籍クイズ==="

// bookReviewData feeds prompts/bookreview.tmpl. Content here is book-derived
// (private) text — this prompt is only ever sent to the local Ollama model by
// the type of BookReviewGenerator.llm (§12-4), never to a cloud provider.
type bookReviewData struct {
	ShowName string
	Title    string
	Chunks   []BookChunk
	Marker   string
}

// BookReviewGenerator produces the §7.3 book_review segment (and, riding on
// the same local call, the §5.3 book quiz) from book_chunks. It holds an
// OllamaLLM, so the cloud fallback chain is structurally unreachable here.
type BookReviewGenerator struct {
	llm      OllamaLLM
	showName string
	logger   *slog.Logger
}

// NewBookReviewGenerator creates a generator over the given LOCAL model. Pass
// a *summarizer.Ollama — the interface makes it impossible to pass the cloud
// chain (§12-4). A nil logger falls back to slog.Default().
func NewBookReviewGenerator(llm OllamaLLM, showName string, logger *slog.Logger) *BookReviewGenerator {
	if logger == nil {
		logger = slog.Default()
	}
	return &BookReviewGenerator{llm: llm, showName: showName, logger: logger}
}

// Generate renders the book_review script and one book quiz from chunks of
// bookTitle in a SINGLE Ollama call (§5.3). By the type of g.llm no cloud
// provider is involved (§12-4). A generation error — engine down, empty
// completion, empty review body — is returned to the caller, which skips
// book_review for the day (§7.3 縮退: 翌日カーソル位置から再開). A missing or
// unparseable quiz section is NOT an error: Quiz is nil and the review still
// ships (§5.3 縮退: book_review 本体は出す).
func (g *BookReviewGenerator) Generate(ctx context.Context, bookTitle string, chunks []BookChunk) (BookReviewResult, error) {
	if len(chunks) == 0 {
		return BookReviewResult{}, fmt.Errorf("script: book_review: no chunks")
	}
	prompt, err := renderPrompt("bookreview.tmpl", bookReviewData{
		ShowName: g.showName,
		Title:    bookTitle,
		Chunks:   chunks,
		Marker:   bookQuizMarker,
	})
	if err != nil {
		return BookReviewResult{}, err
	}

	raw, err := g.llm.Generate(ctx, prompt)
	if err != nil {
		return BookReviewResult{}, fmt.Errorf("script: book_review generate (ollama): %w", err)
	}

	body, quiz := g.splitBookReview(ctx, raw)
	body = strings.TrimSpace(body)
	if body == "" {
		return BookReviewResult{}, fmt.Errorf("script: book_review: empty script")
	}
	g.logger.InfoContext(ctx, "book_review script generated (ollama)",
		slog.String("book_title", bookTitle),
		slog.Int("chunks", len(chunks)),
		slog.Int("script_chars", len([]rune(body))),
		slog.Bool("has_quiz", quiz != nil))
	return BookReviewResult{Script: body, Quiz: quiz}, nil
}

// splitBookReview separates the review body from the piggybacked quiz at the
// first marker line. A missing marker or an incomplete quiz degrades to a nil
// quiz with a warning (§5.3): book_review is private-only (D-16), so a
// marker-mangled review body carrying a stray quiz sentence is at most an odd
// read for the admin, never a public/cloud leak — no stripQuizLeak safety net
// is needed here.
func (g *BookReviewGenerator) splitBookReview(ctx context.Context, raw string) (string, *BookQuizDraft) {
	i := strings.Index(raw, bookQuizMarker)
	if i < 0 {
		g.logger.WarnContext(ctx, "book quiz section missing from book_review output, shipping review without a quiz (§5.3)")
		return raw, nil
	}
	body := raw[:i]
	section := raw[i+len(bookQuizMarker):]

	var concept, question, answer string
	var cur *string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, bookQuizMarker) {
			continue
		}
		switch {
		case matchLabel(line, "概念", &concept):
			cur = &concept
		case matchLabel(line, "問題", &question):
			cur = &question
		case matchLabel(line, "答え", &answer):
			cur = &answer
		case cur != nil:
			// Unlabeled line: a model line-wrap. Join to the current field
			// (read-aloud text, so newline-joining is fine).
			if *cur == "" {
				*cur = line
			} else {
				*cur += "\n" + line
			}
		}
	}
	if concept == "" || question == "" || answer == "" {
		g.logger.WarnContext(ctx, "book quiz section incomplete, shipping review without a quiz (§5.3)",
			slog.Bool("has_concept", concept != ""),
			slog.Bool("has_question", question != ""),
			slog.Bool("has_answer", answer != ""))
		return body, nil
	}
	return body, &BookQuizDraft{Concept: concept, Question: question, Answer: answer}
}

// matchLabel applies cutLabel (shared with the article quiz parser: tolerates
// full-width colons and spacing) and writes the value into dst on a match.
func matchLabel(line, label string, dst *string) bool {
	v, ok := cutLabel(line, label)
	if !ok {
		return false
	}
	*dst = v
	return true
}
