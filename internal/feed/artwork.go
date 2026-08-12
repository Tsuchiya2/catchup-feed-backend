package feed

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"net/http"
	"time"
)

// artworkJPEG is the channel artwork (1536x1536 JPEG), embedded into the
// server binary so delivery can never fail on a missing file — the
// "artwork lost on disk" failure mode simply does not exist (縮退許容).
// Replacing the artwork is a file swap of assets/artwork.jpg plus a
// rebuild; no runtime configuration, no resizing pipeline (single-user
// right-sizing).
//
//go:embed assets/artwork.jpg
var artworkJPEG []byte

// artworkFingerprintLen is how much of the digest goes into the URL.
// Eight hex chars keep the subscription URL readable, and distinguishing
// the handful of artwork revisions this project will ever have needs no
// more (D-33). It is a cache key, not a security boundary — and
// deliberately not what the ETag is built from, see artworkETag.
const artworkFingerprintLen = 8

// artworkDigest is the hex SHA-256 of the embedded artwork, computed once
// at process start. Both the URL fingerprint and the ETag derive from it,
// so identical bytes always mean an identical URL and an identical
// validator, across restarts and across the two listeners.
var artworkDigest = sha256Hex(artworkJPEG)

// artworkFingerprint is the leading slice of artworkDigest that the
// artwork URL carries.
//
// Podcast apps key channel artwork on its URL, and Apple Podcasts copies
// the image onto its own servers: a file swap behind a fixed URL is
// invisible to subscribers no matter what cache headers we send. So the
// URL carries the content's fingerprint and changes whenever the bytes
// change — a rebuilt binary serves a new artwork URL, which the apps read
// as a new image.
var artworkFingerprint = artworkDigest[:artworkFingerprintLen]

// artworkETag is the strong validator for the artwork body, letting a
// conditional GET answer 304 instead of resending ~320 KB.
//
// It uses the FULL digest, not the 8-char URL fingerprint. A strong
// validator promises byte equality, and 32 bits is small enough for two
// different artwork files to collide — which would answer 304 for changed
// bytes and leave the old image in place, reproducing the very bug this
// whole change exists to fix. The URL keeps the short form for
// readability; the ETag pays 56 more characters for the guarantee.
var artworkETag = `"` + artworkDigest + `"`

// sha256Hex returns the hex-encoded SHA-256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// artworkFileName is the {file} segment of the canonical fingerprinted
// artwork route: "<fingerprint>.jpg". The fingerprint occupies a whole
// path segment because Go's ServeMux wildcards cannot be mixed with
// literals inside one segment ("artwork-{fp}.jpg" is not a legal
// pattern). Built once, like the fingerprint it derives from: it is
// compared against on every artwork request.
var artworkFileName = artworkFingerprint + ".jpg"

const (
	// artworkCacheControl applies wherever the URL is not bound to the
	// bytes it returns: the legacy fixed /artwork.jpg, and any
	// fingerprinted URL carrying a fingerprint other than the current one.
	// One day is long enough to make refetches rare, short enough that a
	// rebuild propagates.
	artworkCacheControl = "public, max-age=86400"

	// artworkImmutableCacheControl applies to the one canonical URL whose
	// fingerprint matches the bytes served. That URL is bound to its
	// content, so it can be cached forever: a new image arrives as a new
	// URL, never as a new body at the old one.
	artworkImmutableCacheControl = "public, max-age=31536000, immutable"
)

// handleArtwork serves the embedded artwork at the legacy fixed URL
// (/feeds/{token}/artwork.jpg, /private/artwork.jpg). The route is kept
// alive indefinitely: apps that subscribed before fingerprinted URLs
// existed still hold it, and answering 404 there would blank out their
// channel art.
func (s *Server) handleArtwork(w http.ResponseWriter, r *http.Request) {
	s.serveArtwork(w, r, artworkCacheControl)
}

// handleArtworkFingerprinted serves the embedded artwork at the
// content-addressed URL (/feeds/{token}/artwork/{file},
// /private/artwork/{file}).
//
// The {file} segment is deliberately NOT checked for serving purposes: an
// app holding a stale URL gets the current image rather than a 404
// (D-33(3), 縮退許容 — a slightly wrong cache key beats a missing image).
//
// It IS checked to pick the caching policy (D-33(4)). "immutable" is only
// justified where the URL is bound to the content, which holds for the
// current fingerprint alone; every other {file} value is just another
// alias for whatever the binary happens to embed today, so it gets the
// same one-day TTL as the legacy fixed URL. This also stops a token
// holder from minting unbounded distinct URLs that each pin a year-long
// edge-cache entry.
func (s *Server) handleArtworkFingerprinted(w http.ResponseWriter, r *http.Request) {
	cacheControl := artworkCacheControl
	if r.PathValue("file") == artworkFileName {
		cacheControl = artworkImmutableCacheControl
	}
	s.serveArtwork(w, r, cacheControl)
}

// serveArtwork writes the artwork with the given caching policy.
// http.ServeContent does the conditional-request work: If-None-Match
// against the ETag we set answers 304 with no body. The zero modtime
// suppresses Last-Modified — the embedded bytes have no meaningful
// timestamp, and the ETag is the stronger validator anyway.
//
// Artwork fetches are deliberately not access-logged: they are app-cache
// traffic, and a nil-episode row would be indistinguishable from a
// feed.xml poll in the §4 access log schema.
func (s *Server) serveArtwork(w http.ResponseWriter, r *http.Request, cacheControl string) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("ETag", artworkETag)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(artworkJPEG))
}
