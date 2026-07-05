package entity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// feedTokenByteLen is the entropy of a feed token: crypto/rand 32 bytes,
// rendered as base64url (§5.2, opaque token — JWT is deliberately not used).
const feedTokenByteLen = 32

// FeedToken represents one issued subscription token (feed_tokens table,
// §4). Only the SHA-256 hex hash of the token is stored (D-5); the
// plaintext is shown once at issue time and can never be re-displayed —
// only revoked and reissued as a new row.
type FeedToken struct {
	ID           int64
	SubscriberID int64
	TokenHash    string // SHA-256 hex of the base64url plaintext
	CreatedAt    time.Time
	RevokedAt    *time.Time // nil = 有効
}

// IsRevoked reports whether the token has been revoked.
func (t *FeedToken) IsRevoked() bool {
	return t.RevokedAt != nil
}

// NewFeedTokenPlaintext generates a new opaque feed token:
// crypto/rand 32 bytes encoded as unpadded base64url (§5.2).
func NewFeedTokenPlaintext() (string, error) {
	buf := make([]byte, feedTokenByteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate feed token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashFeedToken returns the SHA-256 hex digest of a plaintext token, the
// form stored in feed_tokens.token_hash (D-5). Verification hashes the
// request token and looks the digest up in the DB, so no constant-time
// comparison is needed.
func HashFeedToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// GenerateFeedToken generates a new token and returns both the plaintext
// (to display once) and its hash (to persist).
func GenerateFeedToken() (plaintext, hash string, err error) {
	plaintext, err = NewFeedTokenPlaintext()
	if err != nil {
		return "", "", err
	}
	return plaintext, HashFeedToken(plaintext), nil
}
