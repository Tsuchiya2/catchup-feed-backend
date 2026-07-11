package scraper_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/infra/scraper"
)

func TestRSSFetcher_Fetch_Success(t *testing.T) {
	// モックRSSフィードを提供するHTTPサーバー
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <description>Test Description</description>
    <item>
      <title>Article 1</title>
      <link>https://example.com/article1</link>
      <description>Description 1</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
    </item>
    <item>
      <title>Article 2</title>
      <link>https://example.com/article2</link>
      <description>Description 2</description>
      <pubDate>Tue, 02 Jan 2024 00:00:00 +0000</pubDate>
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

	items, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("items length = %d, want 2", len(items))
	}

	if items[0].Title != "Article 1" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Article 1")
	}
	if items[0].URL != "https://example.com/article1" {
		t.Errorf("items[0].URL = %q, want %q", items[0].URL, "https://example.com/article1")
	}
	if items[0].Content != "Description 1" {
		t.Errorf("items[0].Content = %q, want %q", items[0].Content, "Description 1")
	}

	if items[1].Title != "Article 2" {
		t.Errorf("items[1].Title = %q, want %q", items[1].Title, "Article 2")
	}
}

func TestRSSFetcher_Fetch_Atom(t *testing.T) {
	// Atomフィードのテスト
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atom := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Atom Feed</title>
  <link href="https://example.com"/>
  <updated>2024-01-01T00:00:00Z</updated>
  <entry>
    <title>Atom Article 1</title>
    <link href="https://example.com/atom1"/>
    <id>atom1</id>
    <updated>2024-01-01T00:00:00Z</updated>
    <summary>Atom Summary 1</summary>
  </entry>
</feed>`
		w.Header().Set("Content-Type", "application/atom+xml")
		if _, err := w.Write([]byte(atom)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRSSFetcher(client)

	items, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	if items[0].Title != "Atom Article 1" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Atom Article 1")
	}
}

// TestRSSFetcher_Fetch_Enclosures: Phase 2 §5.2 — podcast RSS の enclosure
// (音声 URL)を FeedItem.EnclosureURL に載せる。audio/* を優先し video/*
// にフォールバック。それ以外(image/* 等)や enclosure なしは空文字のまま
// (呼び出し側が SkippedNoMedia としてスキップ)。
func TestRSSFetcher_Fetch_Enclosures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Podcast</title>
    <link>https://example.com</link>
    <description>Podcast</description>
    <item>
      <title>Ep 1: audio enclosure</title>
      <link>https://example.com/ep1</link>
      <enclosure url="https://cdn.example.com/ep1.mp3" length="123" type="audio/mpeg"/>
    </item>
    <item>
      <title>Ep 2: image first, audio wins</title>
      <link>https://example.com/ep2</link>
      <enclosure url="https://cdn.example.com/ep2.jpg" length="10" type="image/jpeg"/>
      <enclosure url="https://cdn.example.com/ep2.mp3" length="456" type="audio/mpeg"/>
    </item>
    <item>
      <title>Ep 3: no enclosure</title>
      <link>https://example.com/ep3</link>
    </item>
    <item>
      <title>Ep 4: video enclosure only</title>
      <link>https://example.com/ep4</link>
      <enclosure url="https://cdn.example.com/ep4.mp4" length="789" type="video/mp4"/>
    </item>
    <item>
      <title>Ep 5: image enclosure only</title>
      <link>https://example.com/ep5</link>
      <enclosure url="https://cdn.example.com/ep5.jpg" length="10" type="image/jpeg"/>
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

	items, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("items length = %d, want 5", len(items))
	}

	tests := []struct {
		idx  int
		want string
	}{
		{0, "https://cdn.example.com/ep1.mp3"},
		{1, "https://cdn.example.com/ep2.mp3"}, // audio/* が image より優先
		{2, ""},                                // enclosure なし → 空
		{3, "https://cdn.example.com/ep4.mp4"}, // audio がなければ video/* にフォールバック
		{4, ""},                                // image のみ → media なし(スキップ対象)
	}
	for _, tt := range tests {
		if items[tt.idx].EnclosureURL != tt.want {
			t.Errorf("items[%d].EnclosureURL = %q, want %q", tt.idx, items[tt.idx].EnclosureURL, tt.want)
		}
	}
}

func TestRSSFetcher_Fetch_EmptyFeed(t *testing.T) {
	// 空のフィード
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Empty Feed</title>
    <link>https://example.com</link>
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

	items, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 0 {
		t.Fatalf("items length = %d, want 0", len(items))
	}
}

func TestRSSFetcher_Fetch_InvalidURL(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRSSFetcher(client)

	// 存在しないURL
	_, err := fetcher.Fetch(context.Background(), "http://nonexistent-domain-12345.com/feed")
	if err == nil {
		t.Fatal("Fetch() error = nil, want error")
	}
}

func TestRSSFetcher_Fetch_InvalidXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte("Invalid XML <><><>")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRSSFetcher(client)

	_, err := fetcher.Fetch(context.Background(), server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want error")
	}
}

func TestRSSFetcher_Fetch_ContextCanceled(t *testing.T) {
	// レスポンスを遅延させるサーバー
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		if _, err := w.Write([]byte("<rss></rss>")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{}
	fetcher := scraper.NewRSSFetcher(client)

	// 即座にキャンセルするコンテキスト
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want context canceled error")
	}
}

func TestRSSFetcher_Fetch_WithContent(t *testing.T) {
	// Content優先度のテスト（ContentがあればDescriptionより優先）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Article with Content</title>
      <link>https://example.com/article</link>
      <description>Short description</description>
      <content:encoded><![CDATA[Full content here]]></content:encoded>
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

	items, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	// ContentがDescriptionより優先されることを確認
	if items[0].Content != "Full content here" {
		t.Errorf("items[0].Content = %q, want %q", items[0].Content, "Full content here")
	}
}

// TestRSSFetcher_Fetch_SendsSharedUserAgent verifies that the feed fetch
// announces the crawler-wide User-Agent (fetcher.UserAgent). The RSS path and
// the article-body path share this constant so the two cannot drift apart
// (some sites, e.g. selfh.st, 403 bot-styled User-Agents).
func TestRSSFetcher_Fetch_SendsSharedUserAgent(t *testing.T) {
	gotUA := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA <- r.Header.Get("User-Agent")
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>UA Test Feed</title>
    <link>https://example.com</link>
    <description>UA test</description>
  </channel>
</rss>`
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(rss)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	rf := scraper.NewRSSFetcher(client)

	if _, err := rf.Fetch(context.Background(), server.URL); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if ua := <-gotUA; ua != fetcher.UserAgent {
		t.Errorf("User-Agent = %q, want %q", ua, fetcher.UserAgent)
	}
}
