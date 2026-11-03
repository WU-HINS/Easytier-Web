package dl

import (
	"context"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// OpenPostgres connects to PostgreSQL through the pgx stdlib driver.
func OpenPostgres(ctx context.Context, config Config) (dal.Conn, error) {
	dsn, err := NormalizeDSN(dal.Postgres, config.DSN)
	if err != nil {
		return nil, err
	}
	config.DSN = dsn
	if config.MaxOpen == 0 {
		config.MaxOpen = 16
	}
	return baseDriver{name: "pgx", dialect: dal.Postgres}.open(ctx, config)
}
