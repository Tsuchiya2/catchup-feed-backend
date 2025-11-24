package entity

import (
	"testing"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid https URL",
			url:     "https://example.com/feed",
			wantErr: false,
		},
		{
			name:    "valid http URL",
			url:     "http://example.com/feed",
			wantErr: false,
		},
		{
			name:    "valid URL with port",
			url:     "https://example.com:8080/feed",
			wantErr: false,
		},
		{
			name:    "valid URL with query",
			url:     "https://example.com/feed?param=value",
			wantErr: false,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "invalid scheme - ftp",
			url:     "ftp://example.com/feed",
			wantErr: true,
		},
		{
			name:    "invalid scheme - file",
			url:     "file:///etc/passwd",
			wantErr: true,
		},
		{
			name:    "invalid scheme - javascript",
			url:     "javascript:alert(1)",
			wantErr: true,
		},
		{
			name:    "no host",
			url:     "https://",
			wantErr: true,
		},
		{
			name:    "malformed URL",
			url:     "ht!tp://example.com",
			wantErr: true,
		},
		{
			name:    "no scheme",
			url:     "example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
