package bal

import (
	"context"
	"fmt"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dal/query"
)

// TouchInstance records that this console process is alive. Device tunnels are
// held in process memory, so a second replica sharing the database silently
// sees half the fleet as offline; recording instances makes that detectable.
func (s *Store) TouchInstance(ctx context.Context, id, version string) error {
	now := time.Now()
	statement, args := query.Into("console_instances").
		Columns("id", "version", "started_at", "last_seen").
		Values(id, version, dal.TimeArg(s.Dialect(), now), dal.TimeArg(s.Dialect(), now)).
		OnConflict([]string{"id"}, []string{"version", "last_seen"}).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: touch instance: %w", err)
	}
	return nil
}

// LiveInstances counts console processes that reported within the window.
func (s *Store) LiveInstances(ctx context.Context, within time.Duration) (int, error) {
	statement := "SELECT COUNT(*) FROM " + query.Quote("console_instances") +
		" WHERE " + query.Quote("last_seen") + " >= " + query.Placeholder(s.Dialect(), 1)
	var count int
	cutoff := dal.TimeArg(s.Dialect(), time.Now().Add(-within))
	if err := s.conn.QueryRow(ctx, statement, cutoff).Scan(&count); err != nil {
		return 0, fmt.Errorf("bal: count instances: %w", err)
	}
	return count, nil
}

// DeleteStaleInstances drops instance rows nobody has refreshed. A process
// gets a new id on every boot, so without this the table grows for ever even
// though LiveInstances ignores the old rows.
func (s *Store) DeleteStaleInstances(ctx context.Context, olderThan time.Duration) (int64, error) {
	statement, args := query.DeleteFrom("console_instances").
		Where("last_seen", "<", dal.TimeArg(s.Dialect(), time.Now().Add(-olderThan))).
		Build(s.Dialect())
	return s.conn.Exec(ctx, statement, args...)
}
