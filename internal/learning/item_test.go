package learning

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewItemValidate(t *testing.T) {
	articleID := int64(42)
	bookID := int64(7)
	base := func() NewItem {
		return NewItem{
			Kind:      KindArticle,
			ArticleID: &articleID,
			Concept:   "goroutine leak",
			Question:  "昨日のニュースで触れた goroutine リークですが……",
			Answer:    "context キャンセルで抜け道を作るのが定石です。",
			Provider:  "gemini",
		}
	}

	tests := []struct {
		name    string
		mutate  func(*NewItem)
		wantErr bool
	}{
		{"valid article item", func(n *NewItem) {}, false},
		{"valid book item (provider=ollama)", func(n *NewItem) {
			n.Kind = KindBook
			n.ArticleID = nil
			n.BookID = &bookID
			n.Provider = ProviderOllama
		}, false},

		// §12-4: kind='book' は provider='ollama' 固定をアプリ層で強制し、
		// テストで固定する。クラウド provider の book 項目は必ず弾く。
		{"book item with cloud provider gemini rejected (C-12)", func(n *NewItem) {
			n.Kind = KindBook
			n.ArticleID = nil
			n.BookID = &bookID
			n.Provider = "gemini"
		}, true},
		{"book item with cloud provider groq rejected (C-12)", func(n *NewItem) {
			n.Kind = KindBook
			n.ArticleID = nil
			n.BookID = &bookID
			n.Provider = "groq"
		}, true},

		// kind ⇔ FK (DB CHECK の写し)。
		{"article item without article_id", func(n *NewItem) { n.ArticleID = nil }, true},
		{"article item with book_id", func(n *NewItem) { n.BookID = &bookID }, true},
		{"book item without book_id", func(n *NewItem) {
			n.Kind = KindBook
			n.ArticleID = nil
			n.Provider = ProviderOllama
		}, true},
		{"book item with article_id", func(n *NewItem) {
			n.Kind = KindBook
			n.BookID = &bookID
			n.Provider = ProviderOllama
		}, true},
		{"unknown kind", func(n *NewItem) { n.Kind = "journal" }, true},

		// 必須テキスト。
		{"empty concept", func(n *NewItem) { n.Concept = "" }, true},
		{"empty question", func(n *NewItem) { n.Question = "" }, true},
		{"empty answer", func(n *NewItem) { n.Answer = "" }, true},
		{"empty provider", func(n *NewItem) { n.Provider = "" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := base()
			tt.mutate(&item)
			err := item.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
