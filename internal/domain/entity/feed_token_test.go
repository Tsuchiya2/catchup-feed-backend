package entity

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFeedTokenPlaintext(t *testing.T) {
	plaintext, err := NewFeedTokenPlaintext()
	require.NoError(t, err)

	// 32 bytes -> 43 chars of unpadded base64url (§5.2)
	assert.Len(t, plaintext, 43)
	assert.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9_-]+$`), plaintext,
		"token must be URL-safe without padding")

	decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)

	// Two generations must differ (crypto/rand)
	other, err := NewFeedTokenPlaintext()
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, other)
}

func TestHashFeedToken(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "typical token", plaintext: "0X3RvcHVsc2UtcGhhc2UxLXRlc3QtdG9rZW4tMzJi"},
		{name: "empty string still hashes deterministically", plaintext: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HashFeedToken(tt.plaintext)

			// SHA-256 hex is 64 lowercase hex chars (feed_tokens.token_hash, D-5)
			assert.Len(t, got, 64)
			want := sha256.Sum256([]byte(tt.plaintext))
			assert.Equal(t, hex.EncodeToString(want[:]), got)

			// deterministic
			assert.Equal(t, got, HashFeedToken(tt.plaintext))
		})
	}
}

func TestGenerateFeedToken(t *testing.T) {
	plaintext, hash, err := GenerateFeedToken()
	require.NoError(t, err)

	assert.Equal(t, HashFeedToken(plaintext), hash,
		"returned hash must be the hash of the returned plaintext")
	assert.NotEqual(t, plaintext, hash)
}

func TestFeedToken_IsRevoked(t *testing.T) {
	var token FeedToken
	assert.False(t, token.IsRevoked())

	now := token.CreatedAt
	token.RevokedAt = &now
	assert.True(t, token.IsRevoked())
}
