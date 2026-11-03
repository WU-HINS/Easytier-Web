package dl

import (
	"context"

	_ "modernc.org/sqlite"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// OpenSQLite connects to SQLite. The driver is pure Go, so the console keeps
// building as a static binary without cgo.
func OpenSQLite(ctx context.Context, config Config) (dal.Conn, error) {
	dsn, err := NormalizeDSN(dal.SQLite, config.DSN)
	if err != nil {
		return nil, err
	}
	config.DSN = dsn
	if config.MaxOpen == 0 {
		// SQLite serialises writers; one connection removes SQLITE_BUSY noise.
		config.MaxOpen = 1
	}
	return baseDriver{name: "sqlite", dialect: dal.SQLite}.open(ctx, config)
}
