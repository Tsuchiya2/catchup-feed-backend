package pathutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/handler/http/pathutil"
)

func TestRedactPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "feed xml token is masked",
			path: "/feeds/0123456789abcdefghijklmnopqrstuvwxyzABCDEFG/feed.xml",
			want: "/feeds/[redacted]/feed.xml",
		},
		{
			name: "episode mp3 token is masked, episode id survives",
			path: "/feeds/sometoken/episodes/12.mp3",
			want: "/feeds/[redacted]/episodes/12.mp3",
		},
		{
			name: "bare token segment is masked",
			path: "/feeds/sometoken",
			want: "/feeds/[redacted]",
		},
		{
			name: "trailing slash after token is masked",
			path: "/feeds/sometoken/",
			want: "/feeds/[redacted]",
		},
		{
			name: "double slash prefix cannot bypass the mask",
			path: "//feeds/sometoken/feed.xml",
			want: "/feeds/[redacted]/feed.xml",
		},
		{
			name: "dot segments cannot bypass the mask",
			path: "/./feeds/sometoken/feed.xml",
			want: "/feeds/[redacted]/feed.xml",
		},
		{
			name: "parent traversal into feeds is masked",
			path: "/private/../feeds/sometoken/feed.xml",
			want: "/feeds/[redacted]/feed.xml",
		},
		{
			name: "feeds root is untouched",
			path: "/feeds/",
			want: "/feeds/",
		},
		{
			name: "non-feed paths are untouched",
			path: "/articles/search",
			want: "/articles/search",
		},
		{
			name: "private feed has no token to mask",
			path: "/private/feed.xml",
			want: "/private/feed.xml",
		},
		{
			name: "empty path is untouched",
			path: "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pathutil.RedactPath(tt.path))
		})
	}
}
