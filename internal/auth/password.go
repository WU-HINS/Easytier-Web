// Package auth holds the console credential primitives.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// hashSlots bounds how many argon2 computations run at once. Each one reserves
// its memory cost up front, so unbounded concurrency is a memory amplification
// vector for unauthenticated callers.
var hashSlots = make(chan struct{}, 4)

func acquireHashSlot() { hashSlots <- struct{}{} }
func releaseHashSlot() { <-hashSlots }

// ErrMismatch means the password does not match the stored hash.
var ErrMismatch = errors.New("auth: password mismatch")

// Hash produces an argon2id PHC string.
func Hash(password string) (string, error) {
	acquireHashSlot()
	defer releaseHashSlot()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	const (
		time    = 2
		memory  = 64 * 1024
		threads = 4
		keyLen  = 32
	)
	key := argon2.IDKey([]byte(password), salt, time, memory, threads, keyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, time, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify checks a password against a PHC argon2i or argon2id hash. Both are
// accepted so hashes created by the 2.7.0 Rust server keep working after a
// database migration.
func Verify(password, encoded string) error {
	acquireHashSlot()
	defer releaseHashSlot()
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return fmt.Errorf("auth: unsupported hash format")
	}
	algo := parts[1]
	if algo != "argon2i" && algo != "argon2id" {
		return fmt.Errorf("auth: unsupported algorithm %q", algo)
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != 19 {
		return fmt.Errorf("auth: unsupported argon2 version %q", parts[2])
	}
	var memory, time uint32
	var threads uint8
	for _, field := range strings.Split(parts[3], ",") {
		kv := strings.SplitN(field, "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("auth: malformed parameters")
		}
		value, err := strconv.Atoi(kv[1])
		if err != nil {
			return fmt.Errorf("auth: malformed parameters: %w", err)
		}
		switch kv[0] {
		case "m":
			memory = uint32(value)
		case "t":
			time = uint32(value)
		case "p":
			threads = uint8(value)
		}
	}
	if memory == 0 || time == 0 || threads == 0 {
		return fmt.Errorf("auth: incomplete parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("auth: decode salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("auth: decode key: %w", err)
	}
	var got []byte
	if algo == "argon2i" {
		got = argon2.Key([]byte(password), salt, time, memory, threads, uint32(len(want)))
	} else {
		got = argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// RandomToken returns a URL-safe random secret, used for session and credential
// material.
func RandomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
