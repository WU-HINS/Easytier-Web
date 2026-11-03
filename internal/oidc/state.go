package oidc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// StateBag is the per-login material that must survive the redirect round trip.
type StateBag struct {
	State    string "json:\"state\""
	Nonce    string "json:\"nonce\""
	Verifier string "json:\"verifier\""
	Expires  int64  "json:\"exp\""
	// Redirect is the callback URL the browser produced for this attempt.
	Redirect string "json:\"redirect,omitempty\""
	// Mode is login or bind; bind links an existing account.
	Mode   string "json:\"mode,omitempty\""
	UserID int64  "json:\"uid,omitempty\""
}

// ErrInvalidState means the state cookie was tampered with or expired.
var ErrInvalidState = errors.New("oidc: invalid or expired login state")

// EncodeState signs the bag with the console session key.
func EncodeState(key []byte, bag StateBag) (string, error) {
	payload, err := json.Marshal(bag)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := sign(key, encoded)
	return encoded + "." + signature, nil
}

// DecodeState verifies the signature and the expiry.
func DecodeState(key []byte, raw string) (StateBag, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return StateBag{}, ErrInvalidState
	}
	if !hmac.Equal([]byte(parts[1]), []byte(sign(key, parts[0]))) {
		return StateBag{}, ErrInvalidState
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return StateBag{}, ErrInvalidState
	}
	var bag StateBag
	if err := json.Unmarshal(payload, &bag); err != nil {
		return StateBag{}, ErrInvalidState
	}
	if bag.Expires == 0 || time.Now().After(time.Unix(bag.Expires, 0)) {
		return StateBag{}, ErrInvalidState
	}
	return bag, nil
}

func sign(key []byte, value string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// RandomString returns a URL safe random string of n bytes of entropy.
func RandomString(n int) (string, error) {
	buffer := make([]byte, n)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
