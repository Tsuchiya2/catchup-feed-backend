package radio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
)

// BookReviewStore is the §7.3 book_review persistence: the active review book,
// its unreviewed chunks, the same-day dedupe and the cursor advance.
// Satisfied by repository.BookReviewRepository. A nil BookReview (or a nil
// BookReviewLLM) disables book_review entirely — the private episode is
// news+quiz only (§7.3 縮退と同じ扱い). It is deliberately separate from
// LearningStore: this side owns the book cursor, the learning side owns the
// quiz item.
type BookReviewStore interface {
	ActiveBook(ctx context.Context) (repository.ActiveReviewBook, bool, error)
	NextChunks(ctx context.Context, bookID int64, cursor, limit int) ([]repository.BookReviewChunk, error)
	HasBookReviewOn(ctx context.Context, day time.Time) (bool, error)
	AdvanceCursor(ctx context.Context, bookID int64, fromCursor, newCursor int, finished bool) error
}

// BookReviewer generates the §7.3 book_review script and the §5.3 book quiz
// from chunks, using a LOCAL model only. Satisfied by
// *script.BookReviewGenerator, whose OllamaLLM field cannot be the cloud
// chain (§12-4: 書籍テキストは Gemini/Groq に1バイトも渡さない — 構造で
// 保証). The pipeline never passes book chunks anywhere else.
type BookReviewer interface {
	Generate(ctx context.Context, bookTitle string, chunks []script.BookChunk) (script.BookReviewResult, error)
}

// bookReviewSelection is the read-only choice of the day's book_review target
// (§7.3): the active book and the chunks to cover, plus the cursor advance
// they imply. Producing it makes no cloud call and no forward DB write (it may
// finish an already-exhausted book — a no-op cursor move).
type bookReviewSelection struct {
	book      repository.ActiveReviewBook
	chunks    []repository.BookReviewChunk
	newCursor int
	finished  bool
}

// bookReviewPlan is a fully prepared book_review ready to inject into the
// private episode: the segment row, its synthesized wavs and length, the
// optional book quiz (§5.3), and the cursor advance to commit ONLY after the
// episode is stored (§7.3: 生成失敗で先にカーソルだけ進む事故を防ぐ).
type bookReviewPlan struct {
	segment   *entity.Segment
	wavs      []string
	duration  time.Duration
	quiz      *script.BookQuizDraft
	book      repository.ActiveReviewBook
	newCursor int
	finished  bool
}

// selectBookReview chooses the day's book_review target read-only (§7.3):
// active book → same-day dedupe → next chunks. It returns nil (book_review
// skipped, private stays news+quiz) when book_review is disabled, no book is
// active, the day already has a book_review (same-day rev, §12-2), or any
// lookup fails (§9 縮退). An active book with no remaining chunks is marked
// finished (a no-op cursor move, so the "advance only after success" rule
// holds) and skipped; the finish write is suppressed in dry-run.
func (p *Pipeline) selectBookReview(ctx context.Context, logger *slog.Logger, now time.Time, dryRun bool) *bookReviewSelection {
	if p.BookReview == nil || p.BookReviewLLM == nil {
		return nil
	}
	day := learning.BroadcastDay(now)

	book, ok, err := p.BookReview.ActiveBook(ctx)
	if err != nil {
		logger.Warn("book_review skipped: active book lookup failed (§9)", slog.Any("error", err))
		return nil
	}
	if !ok {
		return nil // no active book — skip (§7.3, D-20: activate 待ち)
	}

	has, err := p.BookReview.HasBookReviewOn(ctx, day)
	if err != nil {
		logger.Warn("book_review skipped: same-day dedupe check failed (§9)", slog.Any("error", err))
		return nil
	}
	if has {
		logger.Info("book_review already done today, skipping (same-day rev re-run, §12-2)",
			slog.String("day", learning.FormatDay(day)))
		return nil
	}

	chunks, err := p.BookReview.NextChunks(ctx, book.ID, book.Cursor, p.Config.BookReviewChunks)
	if err != nil {
		logger.Warn("book_review skipped: chunk fetch failed (§9)", slog.Any("error", err))
		return nil
	}
	if len(chunks) == 0 {
		// The active book is already exhausted (total_chunks==0 or a stale
		// end-of-book cursor). Finish it so the dashboard shows 読了 and radio
		// stops retrying; newCursor == cursor consumes nothing. Dry-run makes
		// no writes.
		if !dryRun {
			if err := p.BookReview.AdvanceCursor(ctx, book.ID, book.Cursor, book.Cursor, true); err != nil {
				logger.Warn("failed to finish exhausted active book (§9)", slog.Any("error", err))
			} else {
				logger.Info("active book has no more chunks, marked finished (§7.3)", slog.Int64("book_id", book.ID))
			}
		}
		return nil
	}

	newCursor := book.Cursor + len(chunks)
	return &bookReviewSelection{
		book:      book,
		chunks:    chunks,
		newCursor: newCursor,
		finished:  newCursor >= book.TotalChunks,
	}
}

// generateBookReview turns a selection into an injectable plan: one Ollama
// call (book text → local model only, §12-4) then TTS into dir. Any failure
// — generation, TTS, wav write — degrades to nil (book_review skipped, no
// cursor advance): the private episode ships news+quiz and tomorrow resumes
// from the SAME cursor (§7.3 縮退).
func (p *Pipeline) generateBookReview(ctx context.Context, logger *slog.Logger, dir string, sel *bookReviewSelection) *bookReviewPlan {
	chunks := make([]script.BookChunk, len(sel.chunks))
	for i, c := range sel.chunks {
		chunks[i] = script.BookChunk{Position: c.Position, Content: c.Content}
	}
	result, err := p.BookReviewLLM.Generate(ctx, sel.book.Title, chunks)
	if err != nil {
		logger.Warn("book_review skipped: generation failed, resuming from the same cursor tomorrow (§7.3 縮退)",
			slog.Int64("book_id", sel.book.ID), slog.Any("error", err))
		return nil
	}

	audios, err := p.TTS.SynthesizeScript(ctx, result.Script)
	if err != nil {
		logger.Warn("book_review skipped: TTS failed (§9)", slog.Int64("book_id", sel.book.ID), slog.Any("error", err))
		return nil
	}
	var wavs []string
	var total time.Duration
	for j, audio := range audios {
		path := filepath.Join(dir, fmt.Sprintf("bookreview_%03d.wav", j))
		if err := os.WriteFile(path, audio.Data, 0o600); err != nil {
			logger.Warn("book_review skipped: write wav failed (§9)", slog.Any("error", err))
			return nil
		}
		wavs = append(wavs, path)
		total += audio.Duration
	}

	return &bookReviewPlan{
		segment:   &entity.Segment{Kind: entity.SegmentKindBookReview, Script: result.Script},
		wavs:      wavs,
		duration:  total,
		quiz:      result.Quiz,
		book:      sel.book,
		newCursor: sel.newCursor,
		finished:  sel.finished,
	}
}

// prepareBookReview runs the full book_review pipeline for a private episode
// whose news+quiz already run to priorDuration: selection → §7.1 length guard
// → generation. It returns nil (book_review skipped) when there is no target,
// when the estimated total would exceed the length cap (deferred to tomorrow,
// no cursor advance, §7.1), or when generation degrades (§7.3). The returned
// plan's cursor advance and quiz insert are committed by
// commitBookReviewProgress AFTER the episode is stored.
func (p *Pipeline) prepareBookReview(ctx context.Context, logger *slog.Logger, dir string, now time.Time, priorDuration time.Duration) *bookReviewPlan {
	sel := p.selectBookReview(ctx, logger, now, false)
	if sel == nil {
		return nil
	}
	if priorDuration+bookReviewEstimate > p.Config.PrivateEpisodeMax {
		// §7.1: 私的版が18分を超えるなら book_review を翌日に回す。カーソルは
		// 進めない(翌日同じ箇所から)。判定は生成前 — Ollama/TTS を無駄に
		// 回さず、想定尺で決める(単純な長さチェックで足りる)。
		logger.Info("book_review deferred to tomorrow: private episode would exceed the length cap (§7.1)",
			slog.Duration("prior", priorDuration),
			slog.Duration("estimate", bookReviewEstimate),
			slog.Duration("cap", p.Config.PrivateEpisodeMax),
			slog.Int64("book_id", sel.book.ID))
		return nil
	}
	return p.generateBookReview(ctx, logger, dir, sel)
}

// commitBookReviewProgress advances the book cursor and inserts the book quiz
// AFTER the private episode carrying the book_review is stored (§7.3). Both
// are best-effort: the episode is already shipped, so a failure only warns and
// self-heals (the cursor stays, tomorrow re-reads the same chunks; a missing
// quiz just means no book item that day, §5.3). The cursor advance is
// idempotent (AdvanceCursor's guarded WHERE), and provider='ollama' is pinned
// by learning.NewItem for the book quiz (§12-4).
func (p *Pipeline) commitBookReviewProgress(ctx context.Context, logger *slog.Logger, now time.Time, plan *bookReviewPlan) {
	if err := p.BookReview.AdvanceCursor(ctx, plan.book.ID, plan.book.Cursor, plan.newCursor, plan.finished); err != nil {
		logger.Warn("book_review cursor advance failed — same chunks may repeat tomorrow (§9)",
			slog.Int64("book_id", plan.book.ID), slog.Any("error", err))
	} else {
		logger.Info("book_review cursor advanced (§7.3)",
			slog.Int64("book_id", plan.book.ID),
			slog.Int("from", plan.book.Cursor), slog.Int("to", plan.newCursor),
			slog.Bool("finished", plan.finished))
	}

	if plan.quiz == nil || p.Learning == nil {
		return
	}
	bookID := plan.book.ID
	item := learning.NewItem{
		Kind:     learning.KindBook,
		BookID:   &bookID,
		Concept:  plan.quiz.Concept,
		Question: plan.quiz.Question,
		Answer:   plan.quiz.Answer,
		Provider: learning.ProviderOllama, // §12-4: book は ollama 固定(NewItem が検証)
	}
	dueOn := learning.FirstDueDay(now)
	id, err := p.Learning.InsertItem(ctx, item, dueOn)
	if err != nil {
		logger.Warn("book quiz insert failed, continuing (§5.3: book_review 本体は出す)",
			slog.Int64("book_id", bookID), slog.Any("error", err))
		return
	}
	logger.Info("book quiz item created (§5.3)",
		slog.Int64("item_id", id), slog.Int64("book_id", bookID),
		slog.String("due_on", learning.FormatDay(dueOn)))
}
