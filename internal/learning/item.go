package learning

import (
	"fmt"
	"time"
)

// Item kinds (learning_items.kind, §4). Single home for the enumeration
// (§12-6).
const (
	KindArticle = "article"
	KindBook    = "book"
)

// ProviderOllama is the only provider allowed for kind='book' items:
// book-derived text is private data and never goes through a cloud LLM
// (C-12 / §12-4). The rule is enforced in the application layer
// (NewItem.Validate), deliberately not as a DB CHECK, and pinned by tests.
const ProviderOllama = "ollama"

// Item is one row of learning_items (§4): quiz content and SRS state in a
// single row (状態テーブルの分離は過剰).
type Item struct {
	ID        int64
	Kind      string // KindArticle | KindBook
	ArticleID *int64 // set iff Kind == KindArticle
	BookID    *int64 // set iff Kind == KindBook
	Concept   string // 1行見出し(トラッカー・週次振り返り・ショーノート)
	Question  string // 読み上げ用クイズ文
	Answer    string // 読み上げ用の答え+一言解説
	Provider  string // 生成 LLM(gemini/groq/ollama)
	Stage     int    // 間隔ラダーの現在段(0 起点)
	DueOn     time.Time
	RetiredAt *time.Time // NULL = 現役
	CreatedAt time.Time
}

// NewItem is the insert shape for a freshly generated learning item
// (§5.1/§5.3). Stage and due_on are not caller-settable: every item starts
// at stage 0 with due_on = 翌日 (FirstDueDay).
type NewItem struct {
	Kind      string
	ArticleID *int64
	BookID    *int64
	Concept   string
	Question  string
	Answer    string
	Provider  string
}

// Validate checks the §4 invariants before the row reaches the database.
// It mirrors the learning_items CHECK constraints (kind ⇔ FK) and adds the
// application-layer rule the schema deliberately omits: kind='book' items
// must have provider='ollama' (C-12 / §12-4 — 書籍は私的データ、クラウド
// LLM 由来の book 項目はバグの証拠なので黙って直さずエラーにする).
func (n NewItem) Validate() error {
	if n.Concept == "" || n.Question == "" || n.Answer == "" {
		return fmt.Errorf("learning: item: concept/question/answer must be non-empty")
	}
	if n.Provider == "" {
		return fmt.Errorf("learning: item: provider must be non-empty")
	}
	switch n.Kind {
	case KindArticle:
		if n.ArticleID == nil || n.BookID != nil {
			return fmt.Errorf("learning: item: kind=article requires article_id and no book_id")
		}
	case KindBook:
		if n.BookID == nil || n.ArticleID != nil {
			return fmt.Errorf("learning: item: kind=book requires book_id and no article_id")
		}
		if n.Provider != ProviderOllama {
			return fmt.Errorf("learning: item: kind=book requires provider=%q, got %q (C-12: 書籍はローカル LLM 限定)",
				ProviderOllama, n.Provider)
		}
	default:
		return fmt.Errorf("learning: item: unknown kind %q", n.Kind)
	}
	return nil
}
