package feed

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"time"

	"catchup-feed/internal/domain/entity"
)

// itunesNS is the iTunes podcast namespace. Only the minimal tags needed
// for podcast-app subscription are emitted (author, duration, explicit,
// block); artwork and directory metadata are out of scope for Phase 1.
const itunesNS = "http://www.itunes.com/dtds/podcast-1.0.dtd"

// channelVoicevoxCredit is appended to every channel description (U-13).
// The channel may mix episodes of different speakers, so it names the
// engine generically; the mandatory per-speaker 「VOICEVOX:話者名」 credit
// is inserted into each episode's show notes by the radio pipeline.
const channelVoicevoxCredit = "音声合成: VOICEVOX"

// channelMeta describes the RSS <channel>.
type channelMeta struct {
	Title       string
	Link        string
	Description string
	Language    string // "ja" for pulse
}

// rssDoc is the RSS 2.0 document. Prefixed names like "itunes:author" are
// emitted verbatim by encoding/xml, which together with the xmlns:itunes
// attribute yields a namespaced feed without an extra dependency.
type rssDoc struct {
	XMLName  xml.Name   `xml:"rss"`
	Version  string     `xml:"version,attr"`
	ItunesNS string     `xml:"xmlns:itunes,attr"`
	Channel  rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Language    string `xml:"language"`
	// itunes:block keeps token-bearing subscription URLs out of the Apple
	// directory: this is a capability URL, not a discoverable show (C-6).
	ItunesBlock    string    `xml:"itunes:block"`
	ItunesExplicit string    `xml:"itunes:explicit"`
	Items          []rssItem `xml:"item"`
}

type rssItem struct {
	Title          string       `xml:"title"`
	Description    string       `xml:"description,omitempty"` // show notes
	PubDate        string       `xml:"pubDate"`
	GUID           rssGUID      `xml:"guid"`
	Enclosure      rssEnclosure `xml:"enclosure"`
	ItunesDuration string       `xml:"itunes:duration,omitempty"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

// renderRSS builds the RSS 2.0 XML for the given episodes. enclosureURL
// maps an episode to its audio URL — for the public feed that is the
// token path (C-9), for the private feed the /private path.
func renderRSS(meta channelMeta, episodes []*entity.Episode, enclosureURL func(*entity.Episode) string) ([]byte, error) {
	items := make([]rssItem, 0, len(episodes))
	for _, ep := range episodes {
		items = append(items, rssItem{
			Title:       ep.Title,
			Description: ep.ShowNotes,
			PubDate:     ep.PublishedAt.UTC().Format(time.RFC1123Z),
			// The GUID is token-independent so that reissuing a friend's
			// token (D-5) does not re-mark old episodes as new.
			GUID: rssGUID{IsPermaLink: "false", Value: fmt.Sprintf("catchup-feed:episode:%d", ep.ID)},
			Enclosure: rssEnclosure{
				URL:    enclosureURL(ep),
				Length: ep.AudioBytes,
				Type:   "audio/mpeg",
			},
			ItunesDuration: itunesDuration(ep.DurationSec),
		})
	}

	// The credit is appended here, not in the config defaults, so it
	// survives any FEED_CHANNEL_DESCRIPTION override (U-13).
	description := meta.Description
	if description != "" {
		description += "\n\n"
	}
	description += channelVoicevoxCredit

	doc := rssDoc{
		Version:  "2.0",
		ItunesNS: itunesNS,
		Channel: rssChannel{
			Title:          meta.Title,
			Link:           meta.Link,
			Description:    description,
			Language:       meta.Language,
			ItunesBlock:    "Yes",
			ItunesExplicit: "false",
			Items:          items,
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render rss: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

// publicEnclosureURL builds the audio URL under the same token path as the
// feed itself (C-9): /feeds/{token}/episodes/{id}.mp3. No bare mp3 URL
// exists.
func publicEnclosureURL(baseURL, token string, episodeID int64) string {
	return fmt.Sprintf("%s/feeds/%s/episodes/%d.mp3", baseURL, url.PathEscape(token), episodeID)
}

// privateEnclosureURL builds the unauthenticated tailnet audio URL:
// /private/episodes/{id}.mp3.
func privateEnclosureURL(baseURL string, episodeID int64) string {
	return fmt.Sprintf("%s/private/episodes/%d.mp3", baseURL, episodeID)
}

// itunesDuration renders seconds in the M:SS / H:MM:SS form podcast apps
// display.
func itunesDuration(sec int) string {
	if sec <= 0 {
		return ""
	}
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
