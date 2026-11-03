// Package db wires the four data layers together and exposes exactly one
// surface to the rest of the program: a *bal.Store.
//
//	dl   driver      (connection pool, concrete SQL driver)
//	dal  abstraction (dialect, query compiler, connection interface)
//	dsm  declarative schema management (declaration, reconciler, version, backup)
//	bal  business adapter (the only import business code may use)
//
// Configuration comes from the environment only, on purpose: a console that is
// deployed as a container should not need command line flags to find its
// database.
package db

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dl"
	"github.com/easytier/easytier-console/internal/db/dsm"
)

// Config is the resolved database configuration.
// legacyTables identify a database written by the 2.7.0 (Rust) console. They
// deliberately exclude "users", which our own schema also uses, so detection
// never fires on a database this build created.
var legacyTables = []string{"groups", "permissions", "users_groups", "groups_permissions"}

type Config struct {
	Driver    dal.Dialect
	DSN       string
	MaxOpen   int
	MaxIdle   int
	ConnLife  time.Duration
	AutoAlign bool
	DryRun    bool
	Backup    bool
	BackupDir string
	BackupCmd string
}

// EnvConfig reads the configuration from the process environment.
//
//	EASYTIER_DB_DRIVER       sqlite | postgres      (required)
//	EASYTIER_DB_DSN          file path or DSN       (required)
//	EASYTIER_DB_MAX_OPEN     max open connections   (default 1 sqlite / 16 postgres)
//	EASYTIER_DB_MAX_IDLE     max idle connections
//	EASYTIER_DB_CONN_LIFETIME seconds               (default 1800)
//	EASYTIER_DB_AUTO_ALIGN   1|0                    (default 1)
//	EASYTIER_DB_ALIGN_DRYRUN 1|0                    (default 0; report only)
//	EASYTIER_DB_BACKUP       1|0                    (default 1)
//	EASYTIER_DB_BACKUP_DIR   directory              (default .)
//	EASYTIER_DB_BACKUP_CMD   shell command for postgres dumps (optional)
func EnvConfig() (Config, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("EASYTIER_DB_DRIVER")))
	var driver dal.Dialect
	switch raw {
	case "sqlite", "sqlite3":
		driver = dal.SQLite
	case "postgres", "postgresql", "pg":
		driver = dal.Postgres
	case "":
		return Config{}, fmt.Errorf("db: EASYTIER_DB_DRIVER is required (sqlite|postgres)")
	default:
		return Config{}, fmt.Errorf("db: unsupported EASYTIER_DB_DRIVER %q", raw)
	}
	dsn := strings.TrimSpace(os.Getenv("EASYTIER_DB_DSN"))
	if dsn == "" {
		return Config{}, fmt.Errorf("db: EASYTIER_DB_DSN is required")
	}
	def := 16
	if driver == dal.SQLite {
		def = 1
	}
	return Config{
		Driver:    driver,
		DSN:       dsn,
		MaxOpen:   envInt("EASYTIER_DB_MAX_OPEN", def),
		MaxIdle:   envInt("EASYTIER_DB_MAX_IDLE", def),
		ConnLife:  time.Duration(envInt("EASYTIER_DB_CONN_LIFETIME", 1800)) * time.Second,
		AutoAlign: envBool("EASYTIER_DB_AUTO_ALIGN", true),
		DryRun:    envBool("EASYTIER_DB_ALIGN_DRYRUN", false),
		Backup:    envBool("EASYTIER_DB_BACKUP", true),
		BackupDir: strings.TrimSpace(os.Getenv("EASYTIER_DB_BACKUP_DIR")),
		BackupCmd: strings.TrimSpace(os.Getenv("EASYTIER_DB_BACKUP_CMD")),
	}, nil
}

// Open connects, aligns the schema and returns the business adapter.
func Open(ctx context.Context) (*bal.Store, dsm.Report, error) {
	config, err := EnvConfig()
	if err != nil {
		return nil, dsm.Report{}, err
	}
	return OpenWith(ctx, config)
}

// OpenWith connects using an explicit configuration (used by tests).
func OpenWith(ctx context.Context, config Config) (*bal.Store, dsm.Report, error) {
	driverConfig := dl.Config{
		DSN:      config.DSN,
		MaxOpen:  config.MaxOpen,
		MaxIdle:  config.MaxIdle,
		ConnLife: config.ConnLife,
	}
	var (
		conn dal.Conn
		err  error
	)
	switch config.Driver {
	case dal.SQLite:
		conn, err = dl.OpenSQLite(ctx, driverConfig)
	case dal.Postgres:
		conn, err = dl.OpenPostgres(ctx, driverConfig)
	default:
		return nil, dsm.Report{}, fmt.Errorf("db: unsupported driver %q", config.Driver)
	}
	if err != nil {
		return nil, dsm.Report{}, err
	}
	if !config.AutoAlign {
		return bal.New(conn), dsm.Report{}, nil
	}
	backup := dsm.BackupOptions{
		Enabled:    config.Backup,
		Dir:        config.BackupDir,
		Command:    config.BackupCmd,
		SQLitePath: "",
	}
	if config.Driver == dal.SQLite {
		// The DSN may be a URI (file:...?_pragma=...) while the backup
		// writer needs a path on disk; passing the raw DSN made every
		// cold start with a URI DSN abort (review team, r140).
		backup.SQLitePath = sqliteFilePath(config.DSN)
	}
	report, err := dsm.Init(ctx, conn, dsm.Options{
		Backup:       backup,
		DryRun:       config.DryRun,
		LegacyTables: legacyTables,
	})
	if err != nil {
		_ = conn.Close()
		return nil, report, err
	}
	return bal.New(conn), report, nil
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// sqliteFilePath reduces a SQLite DSN to the file it points at.
func sqliteFilePath(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	trimmed = strings.TrimPrefix(trimmed, "file:")
	if index := strings.IndexByte(trimmed, "?"[0]); index >= 0 {
		trimmed = trimmed[:index]
	}
	return trimmed
}
