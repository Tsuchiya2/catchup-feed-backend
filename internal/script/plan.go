package script

import (
	"sort"

	"catchup-feed/internal/repository"
)

// Plan selects and orders the articles for one episode (§6-1).
//
// Selection: the newest maxFeatured articles get on air; the rest go to the
// show notes as links only (超過分はショーノートにリンクのみ). Ordering of
// the featured set groups by sources.category (放送のコーナー分け, category
// name ascending for a stable running order) and runs oldest-first inside a
// corner. Overflow keeps newest-first (show-notes reading order).
// Everything is deterministic: ties break on article ID.
func Plan(articles []repository.RadioArticle, maxFeatured int) (featured, overflow []repository.RadioArticle) {
	byRecency := append([]repository.RadioArticle(nil), articles...)
	sort.Slice(byRecency, func(i, j int) bool {
		if !byRecency[i].PublishedAt.Equal(byRecency[j].PublishedAt) {
			return byRecency[i].PublishedAt.After(byRecency[j].PublishedAt)
		}
		return byRecency[i].ID > byRecency[j].ID
	})

	if maxFeatured < 0 {
		maxFeatured = 0
	}
	if maxFeatured > len(byRecency) {
		maxFeatured = len(byRecency)
	}
	featured = byRecency[:maxFeatured]
	overflow = byRecency[maxFeatured:]

	sort.Slice(featured, func(i, j int) bool {
		if featured[i].Category != featured[j].Category {
			return featured[i].Category < featured[j].Category
		}
		if !featured[i].PublishedAt.Equal(featured[j].PublishedAt) {
			return featured[i].PublishedAt.Before(featured[j].PublishedAt)
		}
		return featured[i].ID < featured[j].ID
	})
	return featured, overflow
}
