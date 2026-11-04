package httpapi

import (
	"errors"
	"testing"
	"time"
)

// TestPendingRegistrationSurvivesARoundTrip is the base every later round builds
// on: the identity that finished the provider round trip must reach the username
// form intact, and nothing else may be trusted.
func TestPendingRegistrationSurvivesARoundTrip(t *testing.T) {
	key := []byte("test-session-key")
	now := time.Unix(1_800_000_000, 0)
	want := pendingRegistration{Issuer: "https://idp.example", Subject: "sub-1", Email: "a@b.c", Name: "Ada"}

	value := encodePendingRegistration(key, want, now)
	if value == "" {
		t.Fatal("encoding produced an empty cookie value")
	}
	got, err := decodePendingRegistration(key, value, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Issuer != want.Issuer || got.Subject != want.Subject || got.Email != want.Email || got.Name != want.Name {
		t.Fatalf("round trip changed the identity: %+v", got)
	}
}

// TestPendingRegistrationRejectsWhatItCannotProve covers the three ways a client
// could try to hand us a fabricated identity.
func TestPendingRegistrationRejectsWhatItCannotProve(t *testing.T) {
	key := []byte("test-session-key")
	other := []byte("another-session-key")
	now := time.Unix(1_800_000_000, 0)
	value := encodePendingRegistration(key, pendingRegistration{Issuer: "https://idp.example", Subject: "sub-1"}, now)

	cases := []struct {
		name  string
		value string
		at    time.Time
	}{
		{"empty", "", now},
		{"no signature", "abcdef", now},
		{"tampered payload", "x" + value, now},
		{"expired", value, now.Add(11 * time.Minute)},
	}
	for _, tc := range cases {
		if _, err := decodePendingRegistration(key, tc.value, tc.at); !errors.Is(err, errNoPendingRegistration) {
			t.Fatalf("%s: err = %v, want errNoPendingRegistration", tc.name, err)
		}
	}
	if _, err := decodePendingRegistration(other, value, now); !errors.Is(err, errNoPendingRegistration) {
		t.Fatal("a different session key accepted the cookie")
	}
}
