package fetcher

// UserAgent is the User-Agent string shared by every outbound crawl request:
// the RSS/Atom feed fetch (internal/infra/scraper) and the article-body fetch
// (ReadabilityFetcher). Crawl-side HTTP fetch policy is consolidated in this
// package, as with the SSRF policy (SSRFCheckRedirect) — though the SSRF hook
// is injected at client construction in cmd/, so this constant is the first
// direct fetcher import from scraper's non-test code. The dependency is
// one-way (scraper -> fetcher; fetcher never imports scraper), so no cycle.
// Keeping it in one constant prevents the two paths from drifting apart.
//
// The string is an honest feed-reader identity rather than a "*Bot" name:
// some sites (e.g. selfh.st) intermittently 403 bot-styled User-Agents while
// accepting ordinary reader UAs. Verified 2026-07-12: this UA returns 200 for
// both https://selfh.st/rss and its article pages.
const UserAgent = "catchup-feed/1.0 (personal RSS reader)"
