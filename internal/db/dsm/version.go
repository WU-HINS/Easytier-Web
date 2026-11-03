package dsm

import (
	"context"
	"errors"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// Version is the schema version recorded in the database.
type Version struct {
	Fingerprint string
	AppliedAt   time.Time
}

// CurrentVersion reads the recorded schema version.
func CurrentVersion(ctx context.Context, conn dal.Conn) (Version, error) {
	query := "SELECT fingerprint, applied_at FROM schema_state WHERE id = 1"
	row := conn.QueryRow(ctx, rebind(conn.Dialect(), query))
	var (
		fingerprint string
		appliedAt   any
	)
	if err := row.Scan(&fingerprint, &appliedAt); err != nil {
		if errors.Is(err, dal.ErrNotFound) {
			return Version{}, dal.ErrNotFound
		}
		return Version{}, err
	}
	when, err := dal.ParseTime(appliedAt)
	if err != nil {
		return Version{}, err
	}
	return Version{Fingerprint: fingerprint, AppliedAt: when}, nil
}
