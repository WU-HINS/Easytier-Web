package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestCreateUserReportsDuplicateNames covers F-142 at the storage layer: a taken
// name has to come back as a sentinel the handler can turn into 409, never as a
// driver error carrying table and index names.
func TestCreateUserReportsDuplicateNames(t *testing.T) {
	ctx := context.Background()
	target := freshTarget(t)
	store := target.store
	if _, err := store.CreateUser(ctx, "duplicate", "hash"); err != nil {
		t.Fatalf("first user: %v", err)
	}
	_, err := store.CreateUser(ctx, "duplicate", "hash")
	if !errors.Is(err, bal.ErrUsernameTaken) {
		t.Fatalf("duplicate name = %v, want ErrUsernameTaken", err)
	}
	// The message must not leak engine details.
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "unique") ||
		strings.Contains(strings.ToLower(err.Error()), "users.")) {
		t.Fatalf("error leaks driver text: %v", err)
	}
	// A different name still works.
	if _, err := store.CreateUser(ctx, "unique", "hash"); err != nil {
		t.Fatalf("distinct name: %v", err)
	}
}
