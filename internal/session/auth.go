package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// ErrUnauthorized is returned when a device token cannot be resolved.
var ErrUnauthorized = errors.New("session: unauthorized")

// ErrBlocked is returned when a device was explicitly blocked.
var ErrBlocked = errors.New("session: device blocked")

// Authenticator resolves a device token into a tenant.
type Authenticator interface {
	AuthenticateToken(ctx context.Context, token, machineID string) (int64, error)
}

// LocalAuthenticator is the central-mode authenticator: the token in
// "--config-server udp://host:port/<username>" is the console username.
type LocalAuthenticator struct {
	Store           *bal.Store
	AllowAutoCreate bool
	// AutoCreateAllowed, when set, decides whether an unknown device token may
	// create an account. It must consult the tenant policy (registration switch,
	// reserved names); the boolean above is only the operator switch.
	AutoCreateAllowed func(ctx context.Context, username string) error
}

// AuthenticateToken implements Authenticator.
func (a *LocalAuthenticator) AuthenticateToken(ctx context.Context, token, machineID string) (int64, error) {
	if token == "" || machineID == "" {
		return 0, ErrUnauthorized
	}
	user, err := a.Store.GetUserByName(ctx, token)
	if errors.Is(err, bal.ErrNotFound) {
		if !a.AllowAutoCreate {
			return 0, ErrUnauthorized
		}
		if a.AutoCreateAllowed != nil {
			if err := a.AutoCreateAllowed(ctx, token); err != nil {
				return 0, err
			}
		}
		hash, hashErr := auth.Hash(randomPassword())
		if hashErr != nil {
			return 0, hashErr
		}
		created, createErr := a.Store.CreateUser(ctx, token, hash)
		if createErr != nil {
			return 0, fmt.Errorf("session: auto-create user: %w", createErr)
		}
		return created.ID, nil
	}
	if err != nil {
		return 0, err
	}
	blocked, err := a.Store.IsBlocked(ctx, user.ID, machineID)
	if err != nil {
		return 0, err
	}
	if blocked {
		_ = a.Store.RecordBlockedAttempt(ctx, user.ID, machineID)
		return user.ID, ErrBlocked
	}
	return user.ID, nil
}

func randomPassword() string {
	token, err := auth.RandomToken(24)
	if err != nil {
		return "unusable-password"
	}
	return token
}
