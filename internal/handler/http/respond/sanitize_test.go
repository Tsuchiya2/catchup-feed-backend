package respond

import (
	"errors"
	"testing"
)

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name  string
		input error
		want  string
	}{
		{
			name:  "Anthropic API key",
			input: errors.New("API error: sk-ant-api03-1234567890abcdefghijklmnopqrstuvwxyz"),
			want:  "API error: sk-ant-****",
		},
		{
			name:  "OpenAI API key",
			input: errors.New("API error: sk-1234567890abcdefghijklmnopqrstuvwxyz"),
			want:  "API error: sk-****",
		},
		{
			name:  "Database DSN",
			input: errors.New("dial tcp: postgres://user:secretpassword@localhost:5432/db"),
			want:  "dial tcp: postgres://user:****@localhost:5432/db",
		},
		{
			name:  "Multiple API keys",
			input: errors.New("Error with sk-ant-api03abcdef123456 and sk-1234567890abcdefgh"),
			want:  "Error with sk-ant-**** and sk-****",
		},
		{
			name:  "No sensitive info",
			input: errors.New("normal error message"),
			want:  "normal error message",
		},
		{
			name:  "nil error",
			input: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeError(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeError() = %q, want %q", got, tt.want)
			}
		})
	}
}
