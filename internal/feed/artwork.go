package feed

import (
	_ "embed"
	"net/http"
	"strconv"
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

// artworkCacheControl keeps podcast apps from re-fetching the (practically
// immutable) artwork on every feed poll. One day is long enough to make
// refetches rare and short enough that a rebuilt image propagates without
// cache busting.
const artworkCacheControl = "public, max-age=86400"

// handleArtwork serves the embedded channel artwork. It backs both the
// token-verified public route and the unauthenticated tailnet route.
// Artwork fetches are deliberately not access-logged: they are app-cache
// traffic, and a nil-episode row would be indistinguishable from a
// feed.xml poll in the §4 access log schema.
func (s *Server) handleArtwork(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(artworkJPEG)))
	w.Header().Set("Cache-Control", artworkCacheControl)
	_, _ = w.Write(artworkJPEG)
}
