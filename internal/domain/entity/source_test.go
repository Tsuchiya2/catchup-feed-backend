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
		},
		{
			name: "empty lang defaults to en",
			source: Source{
				Name:     "Golang Weekly",
				FeedURL:  "https://example.com/feed.xml",
				Category: "dev",
			},
			wantLang: DefaultSourceLang,
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
		})
	}
}
