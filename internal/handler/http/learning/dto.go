// Package learning provides the §8.1 learning admin API handlers (C-21
// flat routes under /learning/*, JWT required on every route). 理解状態
// (stage・result) はこの API の応答にのみ載る — question/answer 全文を
// ログに流さないこと(§10。slog は id・件数まで)。
package learning

import (
	"net/http"
	"strconv"
	"time"

	"catchup-feed/internal/handler/http/pathutil"
	learncore "catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
)

// PendingReviewDTO is one grading card (GET /learning/reviews/pending):
// the ungraded asking (log_id is what POST /learning/reviews/{id}/grade
// takes) plus the item's quiz content. asked_on is the JST broadcast day.
type PendingReviewDTO struct {
	LogID    int64  `json:"log_id" example:"12"`
	ItemID   int64  `json:"item_id" example:"3"`
	AskedOn  string `json:"asked_on" example:"2026-07-07"`
	Concept  string `json:"concept" example:"goroutine リーク検出"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func toPendingDTO(p repository.PendingReview) PendingReviewDTO {
	return PendingReviewDTO{
		LogID:    p.LogID,
		ItemID:   p.ItemID,
		AskedOn:  learncore.FormatDay(p.AskedOn),
		Concept:  p.Concept,
		Question: p.Question,
		Answer:   p.Answer,
	}
}

// GradeRequest is the POST /learning/reviews/{id}/grade body. result is
// the manual verdict (○△×); 'auto' is reserved for the radio batch's
// 48h auto-resolve and is rejected. binding:"required" makes swag mark the
// field required in the schema, matching the handler's reality: Service.Grade
// rejects an empty or unknown result with ErrInvalidResult → 400 (the 400
// branch itself is unchanged — validation stays in the usecase).
type GradeRequest struct {
	Result string `json:"result" binding:"required" example:"good" enums:"good,fuzzy,forgot"`
}

// GradeResponse reports the item state after the grade (§6.1 遷移後) —
// the material for the grading screen's optimistic update. retired=true
// means the item completed the ladder (卒業) or was already archived.
type GradeResponse struct {
	LogID   int64  `json:"log_id" example:"12"`
	ItemID  int64  `json:"item_id" example:"3"`
	Result  string `json:"result" example:"good" enums:"good,fuzzy,forgot"`
	Stage   int    `json:"stage" example:"1"`
	DueOn   string `json:"due_on" example:"2026-07-14"`
	Retired bool   `json:"retired" example:"false"`
}

// ItemDTO is one tracker row (GET /learning/items): the item, its SRS
// state and a minimal asking-history summary. last_result is null when the
// item was never asked or its latest asking is still ungraded.
type ItemDTO struct {
	ID          int64      `json:"id" example:"3"`
	Kind        string     `json:"kind" example:"article" enums:"article,book"`
	ArticleID   *int64     `json:"article_id,omitempty" example:"42"`
	BookID      *int64     `json:"book_id,omitempty" example:"7"`
	Concept     string     `json:"concept" example:"goroutine リーク検出"`
	Question    string     `json:"question"`
	Answer      string     `json:"answer"`
	Provider    string     `json:"provider" example:"gemini"`
	Stage       int        `json:"stage" example:"1"`
	DueOn       string     `json:"due_on" example:"2026-07-14"`
	RetiredAt   *time.Time `json:"retired_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	TimesAsked  int        `json:"times_asked" example:"2"`
	LastResult  *string    `json:"last_result,omitempty" example:"good"`
	LastAskedOn *string    `json:"last_asked_on,omitempty" example:"2026-07-07"`
}

func toItemDTO(s repository.LearningItemSummary) ItemDTO {
	dto := ItemDTO{
		ID:         s.ID,
		Kind:       s.Kind,
		ArticleID:  s.ArticleID,
		BookID:     s.BookID,
		Concept:    s.Concept,
		Question:   s.Question,
		Answer:     s.Answer,
		Provider:   s.Provider,
		Stage:      s.Stage,
		DueOn:      learncore.FormatDay(s.DueOn),
		RetiredAt:  s.RetiredAt,
		CreatedAt:  s.CreatedAt,
		TimesAsked: s.TimesAsked,
		LastResult: s.LastResult,
	}
	if s.LastAskedOn != nil {
		day := learncore.FormatDay(*s.LastAskedOn)
		dto.LastAskedOn = &day
	}
	return dto
}

// RetireResponse is the POST /learning/items/{id}/retire result. On a
// repeated retire it carries the original retired_at (冪等).
type RetireResponse struct {
	ID        int64     `json:"id" example:"3"`
	RetiredAt time.Time `json:"retired_at"`
}

// BookDTO is one book management row (D-20): §7.3 review state plus the
// chunk total — 進捗率 = review_cursor / total_chunks.
type BookDTO struct {
	ID           int64  `json:"id" example:"7"`
	Title        string `json:"title" example:"リーダブルコード"`
	ReviewStatus string `json:"review_status" example:"active" enums:"idle,active,finished"`
	ReviewCursor int    `json:"review_cursor" example:"12"`
	TotalChunks  int    `json:"total_chunks" example:"180"`
}

func toBookDTO(b repository.ReviewBook) BookDTO {
	return BookDTO{
		ID:           b.ID,
		Title:        b.Title,
		ReviewStatus: b.ReviewStatus,
		ReviewCursor: b.ReviewCursor,
		TotalChunks:  b.TotalChunks,
	}
}

// pathID extracts the {id} wildcard as a positive int64.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, pathutil.ErrInvalidID
	}
	return id, nil
}
