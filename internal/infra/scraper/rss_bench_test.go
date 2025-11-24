package scraper_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/infra/scraper"
)

// BenchmarkRSSFetcher_SmallFeed は小規模フィード（10件）のパース性能を測定
func BenchmarkRSSFetcher_SmallFeed(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <description>Test Description</description>`

		for i := 0; i < 10; i++ {
			rss += `
    <item>
      <title>Article ` + string(rune(i)) + `</title>
      <link>https://example.com/article` + string(rune(i)) + `</link>
      <description>Description ` + string(rune(i)) + `</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
    </item>`
		}

		rss += `
  </channel>
</rss>`
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(rss)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRSSFetcher(client)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fetcher.Fetch(context.Background(), server.URL)
	}
}

// BenchmarkRSSFetcher_MediumFeed は中規模フィード（50件）のパース性能を測定
func BenchmarkRSSFetcher_MediumFeed(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <description>Test Description</description>`

		for i := 0; i < 50; i++ {
			rss += `
    <item>
      <title>Article ` + string(rune(i)) + `</title>
      <link>https://example.com/article` + string(rune(i)) + `</link>
      <description>Description ` + string(rune(i)) + `</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
    </item>`
		}

		rss += `
  </channel>
</rss>`
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(rss)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRSSFetcher(client)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fetcher.Fetch(context.Background(), server.URL)
	}
}

// BenchmarkRSSFetcher_AtomFeed はAtomフィードのパース性能を測定
func BenchmarkRSSFetcher_AtomFeed(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atom := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Atom Feed</title>
  <link href="https://example.com"/>
  <updated>2024-01-01T00:00:00Z</updated>`

		for i := 0; i < 20; i++ {
			atom += `
  <entry>
    <title>Atom Article ` + string(rune(i)) + `</title>
    <link href="https://example.com/atom` + string(rune(i)) + `"/>
    <id>atom` + string(rune(i)) + `</id>
    <updated>2024-01-01T00:00:00Z</updated>
    <summary>Atom Summary ` + string(rune(i)) + `</summary>
  </entry>`
		}

		atom += `
</feed>`
		w.Header().Set("Content-Type", "application/atom+xml")
		if _, err := w.Write([]byte(atom)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRSSFetcher(client)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fetcher.Fetch(context.Background(), server.URL)
	}
}

// BenchmarkRSSFetcher_Parallel は並行フェッチの性能を測定
func BenchmarkRSSFetcher_Parallel(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Article</title>
      <link>https://example.com/article</link>
      <description>Description</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(rss)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRSSFetcher(client)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = fetcher.Fetch(context.Background(), server.URL)
		}
	})
}
