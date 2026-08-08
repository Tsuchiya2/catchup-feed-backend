package fetch

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cooperpressIssueHTML is a trimmed-down replica of a Cooperpress issue body
// (Golang Weekly 風): headline links first, own-host navigation, sponsor
// link, SNS share links, tracking params, in-issue duplicates.
const cooperpressIssueHTML = `
<table><tr><td>
  <p><a href="https://golangweekly.com/issues/560">Read on the web</a></p>
  <h2><a href="https://go.dev/blog/loopvar-preview?utm_source=golangweekly&utm_medium=email">
    Fixing Go's For Loops
  </a></h2>
  <p>The Go team lands a long-awaited change.</p>
  <h2><a href="https://antonz.org/go-1-22/#generics">Go 1.22 Interactive Tour</a></h2>
  <p><a href="https://example-sponsor.dev/landing?gclid=abc123&plan=team">Sponsor: Ship faster</a></p>
  <ul>
    <li><a href="https://research.swtch.com/telemetry">Telemetry in Go</a> — deep dive</li>
    <li><a href="https://research.swtch.com/telemetry">Telemetry in Go (again)</a></li>
    <li><a href="/issues/559">Previous issue</a></li>
    <li><a href="mailto:hello@cooperpress.com">Email us</a></li>
    <li><a href="https://twitter.com/intent/tweet?url=https%3A%2F%2Fgolangweekly.com">Share on Twitter</a></li>
    <li><a href="https://www.facebook.com/sharer.php?u=x">Share</a></li>
    <li><a href="https://cooperpress.com/publications">Published by Cooperpress</a></li>
  </ul>
  <p><img src="banner.png"><a href="https://tip.golang.org/doc/go1.23"><img src="thumb.png"></a></p>
</td></tr></table>`

func TestExtractNewsletterLinks_CooperpressIssue(t *testing.T) {
	links := extractNewsletterLinks(
		cooperpressIssueHTML,
		"https://golangweekly.com/issues/560",
		"https://golangweekly.com/rss",
	)

	// Document order, own-host / SNS / publisher / mailto / relative links
	// removed, tracking params stripped, duplicates collapsed.
	wantURLs := []string{
		"https://go.dev/blog/loopvar-preview",
		"https://antonz.org/go-1-22/", // fragment removed
		"https://example-sponsor.dev/landing?plan=team",
		"https://research.swtch.com/telemetry",
		"https://tip.golang.org/doc/go1.23",
	}
	gotURLs := make([]string, 0, len(links))
	for _, ln := range links {
		gotURLs = append(gotURLs, ln.URL)
	}
	assert.Equal(t, wantURLs, gotURLs)

	// Anchor text becomes the title, whitespace collapsed; an image-only
	// anchor falls back to the URL.
	require.Len(t, links, 5)
	assert.Equal(t, "Fixing Go's For Loops", links[0].Title)
	assert.Equal(t, "Go 1.22 Interactive Tour", links[1].Title)
	assert.Equal(t, "https://tip.golang.org/doc/go1.23", links[4].Title)
}

func TestExtractNewsletterLinks_Table(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		issueURL string
		feedURL  string
		want     []string
	}{
		{
			name:     "empty html yields no links",
			html:     "",
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://weekly.example.com/rss",
			want:     nil,
		},
		{
			name:     "plain text without anchors yields no links",
			html:     "<p>Just a teaser. Read the issue on the web!</p>",
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://weekly.example.com/rss",
			want:     nil,
		},
		{
			name:     "www-prefixed own host is excluded",
			html:     `<a href="https://www.weekly.example.com/subscribe">Subscribe</a><a href="https://a.dev/x">X</a>`,
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://weekly.example.com/rss",
			want:     []string{"https://a.dev/x"},
		},
		{
			name:     "feed host differing from issue host is also excluded",
			html:     `<a href="https://cdn-feeds.example.net/promo">Promo</a><a href="https://a.dev/x">X</a>`,
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://cdn-feeds.example.net/rss",
			want:     []string{"https://a.dev/x"},
		},
		{
			name:     "non-http schemes are dropped",
			html:     `<a href="ftp://a.dev/f">f</a><a href="javascript:void(0)">j</a><a href="http://a.dev/ok">ok</a>`,
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://weekly.example.com/rss",
			want:     []string{"http://a.dev/ok"},
		},
		{
			name: "dedupe happens on the normalized URL",
			html: `<a href="https://a.dev/x?utm_source=nl">first</a>` +
				`<a href="https://a.dev/x#section">second</a>`,
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://weekly.example.com/rss",
			want:     []string{"https://a.dev/x"},
		},
		{
			name:     "denylisted subdomains are dropped",
			html:     `<a href="https://l.facebook.com/l.php?u=x">share</a><a href="https://x.com/user/status/1">post</a><a href="https://a.dev/x">X</a>`,
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://weekly.example.com/rss",
			want:     []string{"https://a.dev/x"},
		},
		{
			name:     "host casing is normalized",
			html:     `<a href="https://A.Dev/Path">X</a>`,
			issueURL: "https://weekly.example.com/1",
			feedURL:  "https://weekly.example.com/rss",
			want:     []string{"https://a.dev/Path"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := extractNewsletterLinks(tt.html, tt.issueURL, tt.feedURL)
			got := make([]string, 0, len(links))
			for _, ln := range links {
				got = append(got, ln.URL)
			}
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeNewsletterURL(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"https://a.dev/x?utm_source=nl&utm_campaign=weekly", "https://a.dev/x", true},
		{"https://a.dev/x?keep=1&utm_source=nl&page=2", "https://a.dev/x?keep=1&page=2", true},
		{"https://a.dev/x?fbclid=123", "https://a.dev/x", true},
		{"https://a.dev/x?gclid=1&mc_cid=2&mc_eid=3", "https://a.dev/x", true},
		{"https://a.dev/x#readme", "https://a.dev/x", true},
		{"HTTPS://A.DEV/x", "https://a.dev/x", true}, // url.Parse lowercases the scheme; host lowered here
		{"/relative/path", "", false},
		{"mailto:x@example.com", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, _, ok := normalizeNewsletterURL(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestExtractNewsletterLinks_ManyLinksKeepDocumentOrder(t *testing.T) {
	// The cap itself lives in processNewsletterIssue; extraction must keep
	// strict document order so "先頭から N 件" is meaningful.
	html := ""
	want := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		html += fmt.Sprintf(`<a href="https://site%02d.dev/post">Post %02d</a>`, i, i)
		want = append(want, fmt.Sprintf("https://site%02d.dev/post", i))
	}
	links := extractNewsletterLinks(html, "https://weekly.example.com/1", "https://weekly.example.com/rss")
	got := make([]string, 0, len(links))
	for _, ln := range links {
		got = append(got, ln.URL)
	}
	assert.Equal(t, want, got)
}
