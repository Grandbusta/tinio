// Package utils is what the controllers share: answers, codes, tokens, email
// and limits.
package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
)

const tokenPrefix = "tns_" // so a leaked session token is recognisable

// Email is the address as it's kept: trimmed, lower case, checked.
func Email(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s[strings.LastIndex(s, "@")+1:], ".") {
		return "", ErrNotEmail
	}
	return s, nil
}

// NewCode is a random 6-digit code.
func NewCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// CodeHash keys a code with the secret, so the database alone can't be used to
// try all million codes, and ties it to its type and target, so a code is
// only ever good for what it was sent for.
func CodeHash(secret []byte, typ, target, code string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(typ + "\x00" + target + "\x00" + code))
	return m.Sum(nil)
}

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
