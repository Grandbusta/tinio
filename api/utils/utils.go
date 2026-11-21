// Package utils is what the controllers share: answers, tokens and limits.
package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

const tokenPrefix = "tns_" // so a leaked session token is recognisable

func NewToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// TokenHash is how a session token is kept; "" for anything that isn't one.
func TokenHash(token string) []byte {
	if !strings.HasPrefix(token, tokenPrefix) {
		return nil
	}
	h := sha256.Sum256([]byte(token))
	return h[:]
}
