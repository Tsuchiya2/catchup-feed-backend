package pathutil

import (
	"errors"
	"testing"
)

func TestExtractID(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		prefix    string
		wantID    int64
		wantError error
	}{
		{
			name:      "valid article ID",
			path:      "/articles/123",
			prefix:    "/articles/",
			wantID:    123,
			wantError: nil,
		},
		{
			name:      "valid source ID",
			path:      "/sources/456",
			prefix:    "/sources/",
			wantID:    456,
			wantError: nil,
		},
		{
			name:      "invalid ID - not a number",
			path:      "/articles/abc",
			prefix:    "/articles/",
			wantID:    0,
			wantError: ErrInvalidID,
		},
		{
			name:      "invalid ID - zero",
			path:      "/articles/0",
			prefix:    "/articles/",
			wantID:    0,
			wantError: ErrInvalidID,
		},
		{
			name:      "invalid ID - negative",
			path:      "/articles/-1",
			prefix:    "/articles/",
			wantID:    0,
			wantError: ErrInvalidID,
		},
		{
			name:      "invalid ID - empty",
			path:      "/articles/",
			prefix:    "/articles/",
			wantID:    0,
			wantError: ErrInvalidID,
		},
		{
			name:      "invalid ID - with extra path",
			path:      "/articles/123/comments",
			prefix:    "/articles/",
			wantID:    0,
			wantError: ErrInvalidID,
		},
		{
			name:      "large valid ID",
			path:      "/articles/9223372036854775807",
			prefix:    "/articles/",
			wantID:    9223372036854775807,
			wantError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotErr := ExtractID(tt.path, tt.prefix)

			if gotID != tt.wantID {
				t.Errorf("ExtractID() id = %v, want %v", gotID, tt.wantID)
			}

			if !errors.Is(gotErr, tt.wantError) {
				t.Errorf("ExtractID() error = %v, want %v", gotErr, tt.wantError)
			}
		})
	}
}
