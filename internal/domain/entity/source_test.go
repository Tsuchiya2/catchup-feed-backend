package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSource_Validate(t *testing.T) {
	tests := []struct {
		name      string
		source    Source
		wantError string // empty = no error
		wantLang  string
		wantKind  string
	}{
		{
			name: "valid source with explicit lang",
			source: Source{
				Name:     "Publickey",
				FeedURL:  "https://www.publickey1.jp/atom.xml",
				Category: "community",
				Lang:     "ja",
			},
			wantLang: "ja",
			wantKind: SourceKindRSS,
		},
		{
			name: "empty lang defaults to en",
			source: Source{
				Name:     "Golang Weekly",
				FeedURL:  "https://example.com/feed.xml",
				Category: "dev",
			},
			wantLang: DefaultSourceLang,
			wantKind: DefaultSourceKind,
		},
		{
			name: "youtube kind is accepted",
			source: Source{
				Name:     "Some Channel",
				FeedURL:  "https://www.youtube.com/feeds/videos.xml?channel_id=UC123",
				Category: "dev",
				Kind:     SourceKindYouTube,
			},
			wantLang: DefaultSourceLang,
			wantKind: SourceKindYouTube,
		},
		{
			name: "podcast kind is accepted",
			source: Source{
				Name:     "fukabori.fm",
				FeedURL:  "https://example.com/podcast.rss",
				Category: "dev",
				Kind:     SourceKindPodcast,
			},
			wantLang: DefaultSourceLang,
			wantKind: SourceKindPodcast,
		},
		{
			name: "invalid kind is rejected",
			source: Source{
				Name:     "Golang Weekly",
				FeedURL:  "https://example.com/feed.xml",
				Category: "dev",
				Kind:     "newsletter",
			},
			wantError: "kind",
		},
		{
			name: "missing name",
			source: Source{
				FeedURL:  "https://example.com/feed.xml",
				Category: "dev",
			},
			wantError: "name",
		},
		{
			name: "missing feed URL",
			source: Source{
				Name:     "Golang Weekly",
				Category: "dev",
			},
			wantError: "feedURL",
		},
		{
			name: "missing category",
			source: Source{
				Name:    "Golang Weekly",
				FeedURL: "https://example.com/feed.xml",
			},
			wantError: "category",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()

			if tt.wantError != "" {
				assert.Error(t, err)
				var vErr *ValidationError
				assert.ErrorAs(t, err, &vErr)
				assert.Equal(t, tt.wantError, vErr.Field)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantLang, tt.source.Lang)
			assert.Equal(t, tt.wantKind, tt.source.Kind)
		})
	}
}

func TestValidSourceKind(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{SourceKindRSS, true},
		{SourceKindYouTube, true},
		{SourceKindPodcast, true},
		{"", false},
		{"newsletter", false},
		{"RSS", false}, // case-sensitive: CHECK 制約と同じ判定
	}
	for _, tt := range tests {
		t.Run("kind="+tt.kind, func(t *testing.T) {
			assert.Equal(t, tt.want, ValidSourceKind(tt.kind))
		})
	}
}
