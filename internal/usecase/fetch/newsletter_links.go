package fetch

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// newsletterLink is one candidate article extracted from a newsletter issue:
// the normalized target URL plus the anchor text (リンク集型ニュースレターでは
// アンカーテキストがほぼ記事タイトル). Title falls back to the URL when the
// anchor has no text (e.g. image links).
type newsletterLink struct {
	URL   string
	Title string
}

// newsletterDenyHosts is the small static noise denylist for newsletter link
// expansion: SNS share buttons and publisher self-promotion that survive the
// own-host exclusion. Matching includes subdomains (host == entry or
// host endswith "."+entry). Deliberately tiny (単一ユーザー右サイズ) — the
// own-host / feed-host exclusion already removes issue navigation and
// subscribe links; this only catches the cross-host leftovers.
var newsletterDenyHosts = []string{
	"twitter.com",
	"x.com",
	"facebook.com",
	"linkedin.com",
	"instagram.com",
	"threads.net",
	"bsky.app",
	"cooperpress.com", // Cooperpress 系フッターの発行元リンク
}

// newsletterTrackingParams are query parameters stripped during URL
// normalization (dedupe 用の正規化と INSERT する URL を同一にするため、除去は
// ここ一箇所で行う)。utm_* はプレフィックス一致で別途落とす。
var newsletterTrackingParams = map[string]bool{
	"fbclid": true,
	"gclid":  true,
	"mc_cid": true,
	"mc_eid": true,
}

// extractNewsletterLinks parses issue HTML (RSS description or the fetched
// issue page) and returns candidate article links in document order:
// absolute http/https URLs only, own-host (issue URL / feed URL) and
// denylisted hosts excluded, tracking params and fragments stripped, and
// deduplicated on the NORMALIZED url (first occurrence wins) — the same
// string later used for both ExistsByURLBatch keys and the INSERT, so the
// dedupe key and the stored value can never diverge (service.go:
// articleURLForItem と同じ罠への対策).
//
// html.Parse never fails on arbitrary input (it error-corrects like a
// browser), so a parse error simply yields no links.
func extractNewsletterLinks(htmlSrc, issueURL, feedURL string) []newsletterLink {
	doc, err := html.Parse(strings.NewReader(htmlSrc))
	if err != nil {
		return nil
	}

	ownHosts := make(map[string]bool, 2)
	for _, u := range []string{issueURL, feedURL} {
		if parsed, err := url.Parse(u); err == nil && parsed.Hostname() != "" {
			ownHosts[canonicalHost(parsed.Hostname())] = true
		}
	}

	seen := make(map[string]bool)
	var links []newsletterLink
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if link, ok := newsletterLinkFromAnchor(n, ownHosts); ok && !seen[link.URL] {
				seen[link.URL] = true
				links = append(links, link)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links
}

// newsletterLinkFromAnchor filters and normalizes one <a> element.
func newsletterLinkFromAnchor(n *html.Node, ownHosts map[string]bool) (newsletterLink, bool) {
	var href string
	for _, attr := range n.Attr {
		if attr.Key == "href" {
			href = strings.TrimSpace(attr.Val)
			break
		}
	}
	if href == "" {
		return newsletterLink{}, false
	}

	normalized, host, ok := normalizeNewsletterURL(href)
	if !ok {
		return newsletterLink{}, false
	}
	if ownHosts[canonicalHost(host)] || isDeniedNewsletterHost(host) {
		return newsletterLink{}, false
	}

	title := collapseWhitespace(anchorText(n))
	if title == "" {
		title = normalized
	}
	return newsletterLink{URL: normalized, Title: title}, true
}

// normalizeNewsletterURL validates and normalizes a candidate link:
// absolute http/https only (relative URLs and mailto:/javascript: are
// dropped), host lowercased, fragment removed, tracking query params (utm_*
// and newsletterTrackingParams) stripped while preserving the order of the
// remaining params. Returns the normalized URL and its hostname.
func normalizeNewsletterURL(raw string) (normalized, host string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", false
	}
	host = u.Hostname()
	if host == "" {
		return "", "", false
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""
	u.RawQuery = stripTrackingParams(u.RawQuery)
	return u.String(), strings.ToLower(host), true
}

// stripTrackingParams filters a raw query string, dropping utm_* and the
// static tracking params. Operates on the raw string (not url.Values) so the
// order of surviving params is preserved — normalization must be
// deterministic because the result is the dedupe key AND the stored URL.
func stripTrackingParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	var kept []string
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		name := pair
		if i := strings.IndexByte(pair, '='); i >= 0 {
			name = pair[:i]
		}
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "utm_") || newsletterTrackingParams[lower] {
			continue
		}
		kept = append(kept, pair)
	}
	return strings.Join(kept, "&")
}

// isDeniedNewsletterHost reports whether host (lowercased) matches the noise
// denylist, including subdomains (www.twitter.com, l.facebook.com, ...).
func isDeniedNewsletterHost(host string) bool {
	for _, deny := range newsletterDenyHosts {
		if host == deny || strings.HasSuffix(host, "."+deny) {
			return true
		}
	}
	return false
}

// canonicalHost lowercases a hostname and trims a leading "www." so the
// own-host exclusion treats www.golangweekly.com and golangweekly.com as the
// same publisher.
func canonicalHost(host string) string {
	return strings.TrimPrefix(strings.ToLower(host), "www.")
}

// anchorText concatenates the text descendants of an anchor node.
func anchorText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// collapseWhitespace trims and collapses runs of whitespace to single
// spaces (newsletter HTML wraps anchor text across indented lines).
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
