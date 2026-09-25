package summarizer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTailRunes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{name: "shorter than n returns everything", input: "あいう", n: 40, want: "あいう"},
		{name: "exactly n returns everything", input: "あいう", n: 3, want: "あいう"},
		{name: "longer than n keeps the tail", input: "あいうえお", n: 2, want: "えお"},
		{name: "multibyte is not split", input: strings.Repeat("攻", 100), n: 5, want: strings.Repeat("攻", 5)},
		{name: "empty input", input: "", n: 40, want: ""},
		{name: "non-positive n returns empty", input: "あいう", n: 0, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tailRunes(tt.input, tt.n)
			assert.Equal(t, tt.want, got)
			assert.True(t, strings.HasSuffix(tt.input, got))
		})
	}
}

// TestNormalFinishReasonsCoverEveryProvider keeps the table in step with the
// chain: a provider missing from it would warn on every single response.
func TestNormalFinishReasonsCoverEveryProvider(t *testing.T) {
	for _, provider := range []string{ProviderGemini, ProviderGroq, ProviderOllama} {
		normal, ok := normalFinishReasons[provider]
		assert.True(t, ok, "provider %s must declare its normal finish reason", provider)
		assert.NotEmpty(t, normal)
	}
}
