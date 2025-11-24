package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// FeedDiagnostic represents the diagnostic result for a single feed
type FeedDiagnostic struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Status        string `json:"status"` // "OK", "HTTP_ERROR", "PARSE_ERROR", "EMPTY", "TIMEOUT", "REDIRECT"
	HTTPCode      int    `json:"http_code"`
	ItemCount     int    `json:"item_count"`
	LatestDate    string `json:"latest_date"`
	ErrorMessage  string `json:"error_message,omitempty"`
	FeedType      string `json:"feed_type"` // "RSS", "ATOM", "UNKNOWN"
	RedirectURL   string `json:"redirect_url,omitempty"`
	ResponseTime  int64  `json:"response_time_ms"`
	ContentLength int64  `json:"content_length"`
}

// RSS structures
type RSS struct {
	Channel struct {
		Items []struct {
			Title   string `xml:"title"`
			PubDate string `xml:"pubDate"`
			Link    string `xml:"link"`
		} `xml:"item"`
	} `xml:"channel"`
}

// Atom structures
type Atom struct {
	Entries []struct {
		Title   string `xml:"title"`
		Updated string `xml:"updated"`
		Link    struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

// Source represents a feed source from database
type Source struct {
	ID      int
	Name    string
	FeedURL string
	Active  bool
}

func main() {
	// Database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://user:password@localhost:5432/catchup_feed?sslmode=disable"
		log.Println("DATABASE_URL not set, using default")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Failed to close database: %v", err)
		}
	}()

	// Fetch all sources
	sources, err := fetchSources(db)
	if err != nil {
		log.Fatalf("Failed to fetch sources: %v", err)
	}

	log.Printf("Diagnosing %d feed sources...\n", len(sources))

	// Diagnose each feed
	diagnostics := make([]FeedDiagnostic, 0, len(sources))
	for i, source := range sources {
		log.Printf("[%d/%d] Diagnosing: %s", i+1, len(sources), source.Name)
		diag := diagnoseFeed(source.Name, source.FeedURL, 30*time.Second)
		diagnostics = append(diagnostics, diag)

		// Rate limiting to be nice to servers
		time.Sleep(500 * time.Millisecond)
	}

	// Generate report
	generateReport(diagnostics)
	generateJSONReport(diagnostics)
	generateSQLFixes(diagnostics)
}

func fetchSources(db *sql.DB) ([]Source, error) {
	rows, err := db.Query("SELECT id, name, feed_url, active FROM sources ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}()

	var sources []Source
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.ID, &s.Name, &s.FeedURL, &s.Active); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

func diagnoseFeed(name, url string, timeout time.Duration) FeedDiagnostic {
	diag := FeedDiagnostic{
		Name: name,
		URL:  url,
	}

	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		diag.Status = "REQUEST_ERROR"
		diag.ErrorMessage = err.Error()
		return diag
	}

	req.Header.Set("User-Agent", "CatchupFeed-Diagnostic/1.0 (https://github.com/yourrepo)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml")

	// Execute request
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	diag.ResponseTime = time.Since(startTime).Milliseconds()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			diag.Status = "TIMEOUT"
			diag.ErrorMessage = fmt.Sprintf("Request timeout after %v", timeout)
		} else {
			diag.Status = "HTTP_ERROR"
			diag.ErrorMessage = err.Error()
		}
		return diag
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Failed to close response body: %v", err)
		}
	}()

	diag.HTTPCode = resp.StatusCode
	diag.ContentLength = resp.ContentLength

	// Check for redirects
	if resp.Request.URL.String() != url {
		diag.RedirectURL = resp.Request.URL.String()
		diag.Status = "REDIRECT"
	}

	// Check HTTP status
	if resp.StatusCode != 200 {
		diag.Status = "HTTP_ERROR"
		diag.ErrorMessage = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)
		return diag
	}

	// Read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		diag.Status = "READ_ERROR"
		diag.ErrorMessage = err.Error()
		return diag
	}

	// Detect feed type and parse
	contentType := resp.Header.Get("Content-Type")
	itemCount, latestDate, feedType, parseErr := parseFeed(body, contentType)

	if parseErr != nil {
		diag.Status = "PARSE_ERROR"
		diag.ErrorMessage = parseErr.Error()
		diag.FeedType = feedType
		return diag
	}

	diag.ItemCount = itemCount
	diag.LatestDate = latestDate
	diag.FeedType = feedType

	if itemCount == 0 {
		diag.Status = "EMPTY"
		diag.ErrorMessage = "Feed has no items"
		return diag
	}

	diag.Status = "OK"
	return diag
}

func parseFeed(body []byte, contentType string) (itemCount int, latestDate string, feedType string, err error) {
	// Try RSS first
	var rss RSS
	if err := xml.Unmarshal(body, &rss); err == nil && len(rss.Channel.Items) > 0 {
		itemCount = len(rss.Channel.Items)
		if itemCount > 0 {
			latestDate = rss.Channel.Items[0].PubDate
		}
		feedType = "RSS"
		return itemCount, latestDate, feedType, nil
	}

	// Try Atom
	var atom Atom
	if err := xml.Unmarshal(body, &atom); err == nil && len(atom.Entries) > 0 {
		itemCount = len(atom.Entries)
		if itemCount > 0 {
			latestDate = atom.Entries[0].Updated
		}
		feedType = "ATOM"
		return itemCount, latestDate, feedType, nil
	}

	// Could not parse
	feedType = "UNKNOWN"
	preview := string(body)
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return 0, "", feedType, fmt.Errorf("failed to parse as RSS or Atom. Content preview: %s", preview)
}

// writef is a helper to write to file and handle errors
func writef(f *os.File, format string, args ...interface{}) error {
	_, err := fmt.Fprintf(f, format, args...)
	return err
}

func generateReport(diagnostics []FeedDiagnostic) {
	f, err := os.Create("feed_diagnostic_report.txt")
	if err != nil {
		log.Printf("Failed to create report file: %v", err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("Failed to close report file: %v", err)
		}
	}()

	// Helper to handle write errors
	writeErr := func(err error) bool {
		if err != nil {
			log.Printf("Failed to write to report: %v", err)
			return true
		}
		return false
	}

	if writeErr(writef(f, "===============================================\n")) {
		return
	}
	if writeErr(writef(f, "RSS Feed Diagnostic Report\n")) {
		return
	}
	if writeErr(writef(f, "Generated: %s\n", time.Now().Format(time.RFC3339))) {
		return
	}
	if writeErr(writef(f, "Total Sources: %d\n", len(diagnostics))) {
		return
	}
	if writeErr(writef(f, "===============================================\n\n")) {
		return
	}

	// Summary statistics
	statusCount := make(map[string]int)
	var okCount, errorCount int
	for _, d := range diagnostics {
		statusCount[d.Status]++
		if d.Status == "OK" || d.Status == "REDIRECT" {
			okCount++
		} else {
			errorCount++
		}
	}

	_ = writef(f, "SUMMARY:\n")
	_ = writef(f, "  ✅ Working: %d (%.1f%%)\n", okCount, float64(okCount)/float64(len(diagnostics))*100)
	_ = writef(f, "  ❌ Broken: %d (%.1f%%)\n", errorCount, float64(errorCount)/float64(len(diagnostics))*100)
	_ = writef(f, "\nSTATUS BREAKDOWN:\n")
	for status, count := range statusCount {
		_ = writef(f, "  %s: %d\n", status, count)
	}
	_ = writef(f, "\n")

	// Detailed results
	_ = writef(f, "DETAILED RESULTS:\n")
	_ = writef(f, "===============================================\n\n")

	// OK feeds
	_ = writef(f, "✅ WORKING FEEDS (%d):\n", statusCount["OK"]+statusCount["REDIRECT"])
	_ = writef(f, "-------------------------------------------\n")
	for _, d := range diagnostics {
		if d.Status == "OK" || d.Status == "REDIRECT" {
			_ = writef(f, "Name: %s\n", d.Name)
			_ = writef(f, "  URL: %s\n", d.URL)
			_ = writef(f, "  Type: %s | Items: %d | Latest: %s\n", d.FeedType, d.ItemCount, d.LatestDate)
			_ = writef(f, "  Response: %dms | HTTP: %d\n", d.ResponseTime, d.HTTPCode)
			if d.RedirectURL != "" {
				_ = writef(f, "  ⚠️  Redirected to: %s\n", d.RedirectURL)
			}
			_ = writef(f, "\n")
		}
	}

	// Error feeds
	_ = writef(f, "\n❌ BROKEN FEEDS (%d):\n", errorCount)
	_ = writef(f, "-------------------------------------------\n")
	for _, d := range diagnostics {
		if d.Status != "OK" && d.Status != "REDIRECT" {
			_ = writef(f, "Name: %s\n", d.Name)
			_ = writef(f, "  URL: %s\n", d.URL)
			_ = writef(f, "  Status: %s | HTTP: %d\n", d.Status, d.HTTPCode)
			_ = writef(f, "  Error: %s\n", d.ErrorMessage)
			_ = writef(f, "  Response: %dms\n", d.ResponseTime)
			_ = writef(f, "\n")
		}
	}

	log.Println("✅ Text report generated: feed_diagnostic_report.txt")
}

func generateJSONReport(diagnostics []FeedDiagnostic) {
	f, err := os.Create("feed_diagnostic_report.json")
	if err != nil {
		log.Printf("Failed to create JSON report: %v", err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("Failed to close JSON report file: %v", err)
		}
	}()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(diagnostics); err != nil {
		log.Printf("Failed to write JSON report: %v", err)
		return
	}

	log.Println("✅ JSON report generated: feed_diagnostic_report.json")
}

func generateSQLFixes(diagnostics []FeedDiagnostic) {
	f, err := os.Create("feed_fixes.sql")
	if err != nil {
		log.Printf("Failed to create SQL fixes file: %v", err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("Failed to close SQL fixes file: %v", err)
		}
	}()

	_ = writef(f, "-- SQL Fixes for Broken Feeds\n")
	_ = writef(f, "-- Generated: %s\n\n", time.Now().Format(time.RFC3339))

	// Redirects
	hasRedirects := false
	for _, d := range diagnostics {
		if d.RedirectURL != "" && d.RedirectURL != d.URL {
			if !hasRedirects {
				_ = writef(f, "-- Update redirected feeds\n")
				hasRedirects = true
			}
			_ = writef(f, "UPDATE sources SET feed_url = '%s' WHERE feed_url = '%s'; -- %s\n",
				strings.ReplaceAll(d.RedirectURL, "'", "''"),
				strings.ReplaceAll(d.URL, "'", "''"),
				d.Name)
		}
	}
	if hasRedirects {
		_ = writef(f, "\n")
	}

	// Broken feeds
	hasBroken := false
	for _, d := range diagnostics {
		if d.Status != "OK" && d.Status != "REDIRECT" {
			if !hasBroken {
				_ = writef(f, "-- Disable broken feeds (review and fix manually)\n")
				hasBroken = true
			}
			_ = writef(f, "UPDATE sources SET active = FALSE WHERE feed_url = '%s'; -- %s: %s\n",
				strings.ReplaceAll(d.URL, "'", "''"),
				d.Name,
				d.Status)
		}
	}

	log.Println("✅ SQL fixes generated: feed_fixes.sql")
}
