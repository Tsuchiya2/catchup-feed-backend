package feed

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
)

func sampleEpisode(id int64, kind, title string, published time.Time) *entity.Episode {
	return &entity.Episode{
		ID:          id,
		FeedKind:    kind,
		Title:       title,
		ShowNotes:   "- https://example.com/article",
		AudioPath:   fmt.Sprintf("/data/episodes/%d.mp3", id),
		AudioBytes:  7_200_000,
		DurationSec: 900,
		PublishedAt: published,
	}
}

// parsedRSS mirrors the generated document for round-trip verification.
type parsedRSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title    string `xml:"title"`
		Language string `xml:"language"`
		Items    []struct {
			Title     string `xml:"title"`
			PubDate   string `xml:"pubDate"`
			GUID      string `xml:"guid"`
			Enclosure struct {
				URL    string `xml:"url,attr"`
				Length int64  `xml:"length,attr"`
				Type   string `xml:"type,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

func TestRenderRSS_PublicEnclosureUnderTokenPath(t *testing.T) {
	// C-9 のピン留め: enclosure URL はフィードと同一のトークンパス配下で
	// なければならない。素の mp3 URL は存在しない。
	const token = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG" // 43 chars, base64url-shaped
	published := time.Date(2026, 7, 4, 4, 30, 0, 0, time.UTC)
	episodes := []*entity.Episode{
		sampleEpisode(2, entity.FeedKindPublic, "pulse 2026-07-04", published),
		sampleEpisode(1, entity.FeedKindPublic, "pulse 2026-07-03", published.Add(-24*time.Hour)),
	}

	meta := channelMeta{Title: "pulse radio", Link: "https://radio.catchup-feed.com", Description: "d", Language: "ja"}
	out, err := renderRSS(meta, episodes, func(ep *entity.Episode) string {
		return publicEnclosureURL("https://radio.catchup-feed.com", token, ep.ID)
	})
	require.NoError(t, err)

	var doc parsedRSS
	require.NoError(t, xml.Unmarshal(out, &doc), "generated feed must be well-formed XML")

	assert.Equal(t, "2.0", doc.Version)
	assert.Equal(t, "ja", doc.Channel.Language)
	require.Len(t, doc.Channel.Items, 2)

	first := doc.Channel.Items[0]
	assert.Equal(t, "https://radio.catchup-feed.com/feeds/"+token+"/episodes/2.mp3", first.Enclosure.URL)
	assert.Equal(t, int64(7_200_000), first.Enclosure.Length)
	assert.Equal(t, "audio/mpeg", first.Enclosure.Type)
	assert.Equal(t, "catchup-feed:episode:2", first.GUID)

	// pubDate は RFC 1123Z(RSS 2.0 / ポッドキャストアプリの期待形式)。
	parsed, err := time.Parse(time.RFC1123Z, first.PubDate)
	require.NoError(t, err)
	assert.True(t, parsed.Equal(published))

	raw := string(out)
	assert.True(t, strings.HasPrefix(raw, xml.Header), "XML declaration must lead the document")
	assert.Contains(t, raw, `xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"`)
	assert.Contains(t, raw, "<itunes:duration>15:00</itunes:duration>")
	assert.Contains(t, raw, "<itunes:block>Yes</itunes:block>", "token URL feeds must opt out of directory listing")
}

func TestRenderRSS_PrivateEnclosureURL(t *testing.T) {
	episodes := []*entity.Episode{
		sampleEpisode(5, entity.FeedKindPrivate, "journal", time.Now()),
	}
	out, err := renderRSS(channelMeta{Title: "t", Link: "l", Description: "d", Language: "ja"},
		episodes, func(ep *entity.Episode) string {
			return privateEnclosureURL("http://pi.tailnet:8081", ep.ID)
		})
	require.NoError(t, err)
	assert.Contains(t, string(out), `url="http://pi.tailnet:8081/private/episodes/5.mp3"`)
}

func TestRenderRSS_EscapesUntrustedText(t *testing.T) {
	ep := sampleEpisode(1, entity.FeedKindPublic, `Go 1.25 <T> & "generics"`, time.Now())
	ep.ShowNotes = "a & b <c>"
	out, err := renderRSS(channelMeta{Title: "t", Link: "l", Description: "d", Language: "ja"},
		[]*entity.Episode{ep}, func(*entity.Episode) string { return "https://example.com/1.mp3" })
	require.NoError(t, err)

	// Round-trip: the escaped document must decode back to the raw text.
	var doc parsedRSS
	require.NoError(t, xml.Unmarshal(out, &doc))
	require.Len(t, doc.Channel.Items, 1)
	assert.Equal(t, `Go 1.25 <T> & "generics"`, doc.Channel.Items[0].Title)

	// And it must survive a strict full-document token walk.
	dec := xml.NewDecoder(strings.NewReader(string(out)))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
}

func TestRenderRSS_EmptyFeedIsValid(t *testing.T) {
	out, err := renderRSS(channelMeta{Title: "t", Link: "l", Description: "d", Language: "ja"},
		nil, func(*entity.Episode) string { return "" })
	require.NoError(t, err)
	var doc parsedRSS
	require.NoError(t, xml.Unmarshal(out, &doc))
	assert.Empty(t, doc.Channel.Items)
}

func TestItunesDuration(t *testing.T) {
	tests := []struct {
		name string
		sec  int
		want string
	}{
		{"zero is omitted", 0, ""},
		{"negative is omitted", -5, ""},
		{"under a minute", 59, "0:59"},
		{"minutes", 900, "15:00"},
		{"hours", 3725, "1:02:05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, itunesDuration(tt.sec))
		})
	}
}
