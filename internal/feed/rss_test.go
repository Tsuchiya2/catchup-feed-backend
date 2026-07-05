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
		Title       string `xml:"title"`
		Description string `xml:"description"`
		Language    string `xml:"language"`
		// ItunesImage is declared before Image so the namespaced field
		// claims the itunes:image element and the plain <image> falls
		// through to Image (encoding/xml matches fields in order).
		ItunesImage struct {
			Href string `xml:"href,attr"`
		} `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
		Image struct {
			URL   string `xml:"url"`
			Title string `xml:"title"`
			Link  string `xml:"link"`
		} `xml:"image"`
		Items []struct {
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

// TestRenderRSS_ChannelDescriptionCarriesVoicevoxCredit pins U-13: every
// generated channel description names the synthesis engine, regardless of
// how FEED_CHANNEL_DESCRIPTION is configured. The per-speaker
// 「VOICEVOX:話者名」 credit lives in each episode's show notes.
func TestRenderRSS_ChannelDescriptionCarriesVoicevoxCredit(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			name:        "configured description keeps the credit appended",
			description: "毎朝の技術ニュースラジオ",
			want:        "毎朝の技術ニュースラジオ\n\n音声合成: VOICEVOX",
		},
		{
			name:        "empty description still carries the credit",
			description: "",
			want:        "音声合成: VOICEVOX",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := renderRSS(
				channelMeta{Title: "t", Link: "l", Description: tt.description, Language: "ja"},
				nil, func(*entity.Episode) string { return "" })
			require.NoError(t, err)

			var doc parsedRSS
			require.NoError(t, xml.Unmarshal(out, &doc))
			assert.Equal(t, tt.want, doc.Channel.Description)
		})
	}
}

// ---- channel artwork ----

func TestRenderRSS_ChannelArtwork(t *testing.T) {
	const artworkURL = "https://radio.catchup-feed.com/feeds/tok/artwork.jpg"
	meta := channelMeta{
		Title:       "pulse radio",
		Link:        "https://radio.catchup-feed.com",
		Description: "d",
		Language:    "ja",
		ImageURL:    artworkURL,
	}
	out, err := renderRSS(meta, nil, func(*entity.Episode) string { return "" })
	require.NoError(t, err)

	var doc parsedRSS
	require.NoError(t, xml.Unmarshal(out, &doc))

	// itunes:image は Apple Podcasts / Overcast が読む側。
	assert.Equal(t, artworkURL, doc.Channel.ItunesImage.Href)
	// RSS 2.0 <image> はフォールバック側。title/link はチャンネル自身を映す。
	assert.Equal(t, artworkURL, doc.Channel.Image.URL)
	assert.Equal(t, "pulse radio", doc.Channel.Image.Title)
	assert.Equal(t, "https://radio.catchup-feed.com", doc.Channel.Image.Link)
}

func TestRenderRSS_NoImageURLOmitsArtworkTags(t *testing.T) {
	out, err := renderRSS(channelMeta{Title: "t", Link: "l", Description: "d", Language: "ja"},
		nil, func(*entity.Episode) string { return "" })
	require.NoError(t, err)
	raw := string(out)
	assert.NotContains(t, raw, "itunes:image")
	assert.NotContains(t, raw, "<image>")
}

func TestArtworkURLs(t *testing.T) {
	assert.Equal(t,
		"https://radio.catchup-feed.com/feeds/tok%2Fen/artwork.jpg",
		publicArtworkURL("https://radio.catchup-feed.com", "tok/en"),
		"the token segment must be path-escaped like the enclosure URLs")
	assert.Equal(t,
		"http://pi.tailnet:8081/private/artwork.jpg",
		privateArtworkURL("http://pi.tailnet:8081"))
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
