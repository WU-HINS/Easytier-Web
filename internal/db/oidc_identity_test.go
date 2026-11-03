package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestBindingOneIdentityTwiceIsAConflict covers F-220: the store wrapped the unique
// violation as an opaque error, so the handler could only answer 500. The sentinel
// is what makes the 409 branch reachable (review team, r134).
func TestBindingOneIdentityTwiceIsAConflict(t *testing.T) {
	ctx := context.Background()
	target := freshTarget(t)
	store := target.store
	first, err := store.CreateUser(ctx, "alice-sso", "hash")
	if err != nil {
		t.Fatalf("first user: %v", err)
	}
	second, err := store.CreateUser(ctx, "bob-sso", "hash")
	if err != nil {
		t.Fatalf("second user: %v", err)
	}
	if err := store.SetUserOIDC(ctx, first.ID, "https://idp.example", "subject-1"); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	err = store.SetUserOIDC(ctx, second.ID, "https://idp.example", "subject-1")
	if !errors.Is(err, bal.ErrOIDCIdentityTaken) {
		t.Fatalf("second bind = %v, want ErrOIDCIdentityTaken", err)
	}
}
