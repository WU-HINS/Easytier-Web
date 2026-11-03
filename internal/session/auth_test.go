package session_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/session"
)

// TestAutoCreateRespectsPolicy covers F-093: --allow-auto-create-user used to
// create an account for any unknown device token, bypassing the registration
// switch and the reserved name list.
func TestAutoCreateRespectsPolicy(t *testing.T) {
	ctx := context.Background()
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	machine := "abcd5555-0000-4000-8000-000000000001"

	// Registration closed: no account is created.
	closed := &session.LocalAuthenticator{
		Store:           store,
		AllowAutoCreate: true,
		AutoCreateAllowed: func(ctx context.Context, username string) error {
			return errors.New("registration is closed")
		},
	}
	if _, err := closed.AuthenticateToken(ctx, "stranger", machine); err == nil {
		t.Fatal("auto-create ignored the policy")
	}
	if _, err := store.GetUserByName(ctx, "stranger"); err == nil {
		t.Fatal("an account was created even though the policy refused")
	}

	// Policy allows it: the account appears.
	open := &session.LocalAuthenticator{
		Store:           store,
		AllowAutoCreate: true,
		AutoCreateAllowed: func(ctx context.Context, username string) error {
			return nil
		},
	}
	if _, err := open.AuthenticateToken(ctx, "newcomer", machine); err != nil {
		t.Fatalf("auto-create with an allowing policy failed: %v", err)
	}
	if _, err := store.GetUserByName(ctx, "newcomer"); err != nil {
		t.Fatalf("account was not created: %v", err)
	}

	// The operator switch alone still blocks everything.
	off := &session.LocalAuthenticator{Store: store, AllowAutoCreate: false}
	if _, err := off.AuthenticateToken(ctx, "another", machine); err == nil {
		t.Fatal("auto-create ran with the switch off")
	}
}
