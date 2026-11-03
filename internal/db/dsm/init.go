package dsm

import (
	"context"
	"fmt"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// Options configures one reconciliation run.
type Options struct {
	Backup BackupOptions
	DryRun bool
	// LegacyTables are the tables that identify a pre-rewrite database.
	LegacyTables []string
}

// Report is the outcome of Init.
type Report struct {
	Changes     []Change
	Fingerprint string
	BackupPath  string
	Legacy      bool
	Drift       []string
}

// Init is the DSM entry point: detect a legacy database, snapshot it, reconcile
// the schema, verify the result and record the version.
//
// The legacy detection is intentionally conservative: it only reports, it never
// rewrites data. Data migration is a separate, explicit step.
func Init(ctx context.Context, conn dal.Conn, options Options) (Report, error) {
	report := Report{Fingerprint: Fingerprint(Export())}
	legacy, err := detectLegacy(ctx, conn, options.LegacyTables)
	if err != nil {
		return report, err
	}
	report.Legacy = legacy

	changes, err := Plan(ctx, conn, conn.Dialect(), Export())
	if err != nil {
		return report, err
	}
	report.Changes = changes
	for _, change := range changes {
		if change.Action == "extra_column" {
			report.Drift = append(report.Drift, change.String())
		}
	}
	if options.DryRun {
		return report, nil
	}
	if len(changes) == 0 {
		return report, nil
	}
	backupPath, err := Backup(ctx, conn, options.Backup)
	if err != nil {
		// A failing snapshot must not silently proceed into a schema change.
		return report, err
	}
	report.BackupPath = backupPath
	if _, err := Ensure(ctx, conn, conn.Dialect(), Export()); err != nil {
		return report, err
	}
	if err := verify(ctx, conn); err != nil {
		return report, err
	}
	return report, nil
}

func detectLegacy(ctx context.Context, conn dal.Conn, tables []string) (bool, error) {
	if len(tables) == 0 {
		return false, nil
	}
	present := 0
	for _, table := range tables {
		ok, err := tableExists(ctx, conn, table)
		if err != nil {
			return false, err
		}
		if ok {
			present++
		}
	}
	return present > 0, nil
}

func tableExists(ctx context.Context, conn dal.Conn, table string) (bool, error) {
	var (
		query string
		args  []any
	)
	if conn.Dialect() == dal.SQLite {
		query = "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?"
	} else {
		query = "SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename = ?"
	}
	query = rebind(conn.Dialect(), query)
	args = append(args, table)
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

// verify re-plans after applying; any remaining create/add step means the
// reconciliation did not converge.
func verify(ctx context.Context, conn dal.Conn) error {
	changes, err := Plan(ctx, conn, conn.Dialect(), Export())
	if err != nil {
		return err
	}
	for _, change := range changes {
		if change.SQL != "" {
			return fmt.Errorf("dsm: schema did not converge: %s", change)
		}
	}
	return nil
}
