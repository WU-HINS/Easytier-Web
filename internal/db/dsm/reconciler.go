package dsm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// Change is one step needed to bring a database in line with the declaration.
type Change struct {
	Action string // create_table | add_column | create_index | extra_column | fingerprint
	Table  string
	Column string // column or index name, when relevant
	SQL    string // empty for report-only changes
	// Order is the declaration index. Sorting must keep it inside a phase,
	// otherwise a table with a foreign key can be created before its parent,
	// which PostgreSQL rejects (SQLite happens to tolerate it).
	Order int
}

func (c Change) String() string {
	switch c.Action {
	case "create_table":
		return "create table " + c.Table
	case "add_column":
		return "add column " + c.Table + "." + c.Column
	case "create_index":
		return "create index " + c.Column
	case "extra_column":
		return "extra column " + c.Table + "." + c.Column + " (left in place)"
	default:
		return c.Action + " " + c.Table
	}
}

// Fingerprint hashes the declaration so a startup can skip work when the live
// schema already matches, and so two binaries can prove they agree.
func Fingerprint(tables []Table) string {
	hash := sha256.New()
	for _, table := range tables {
		hash.Write([]byte(table.Name))
		for _, column := range table.Columns {
			hash.Write([]byte(column.Name + string(column.Kind)))
			if column.NotNull {
				hash.Write([]byte("!"))
			}
			if column.PrimaryKey {
				hash.Write([]byte("P"))
			}
			hash.Write([]byte(column.Default))
		}
		for _, index := range table.Indexes {
			hash.Write([]byte(index.Name + strings.Join(index.Columns, ",") + index.Where))
		}
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

// Plan diffs the live database against the declaration.
func Plan(ctx context.Context, conn dal.Conn, dialect Dialect, want []Table) ([]Change, error) {
	live, err := inspect(ctx, conn, dialect)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for order, table := range want {
		liveTable, exists := live[table.Name]
		if !exists {
			changes = append(changes, Change{Action: "create_table", Table: table.Name, Order: order, SQL: table.CreateTable(dialect)})
			for _, index := range table.Indexes {
				changes = append(changes, Change{
					Action: "create_index",
					Table:  table.Name,
					Column: index.Name,
					Order:  order,
					SQL:    table.IndexDDL(dialect, index),
				})
			}
			continue
		}
		for _, column := range table.Columns {
			if _, ok := liveTable.Columns[column.Name]; ok {
				continue
			}
			changes = append(changes, Change{
				Action: "add_column",
				Table:  table.Name,
				Column: column.Name,
				Order:  order,
				SQL:    addColumnSQL(dialect, table, column),
			})
		}
		for _, index := range table.Indexes {
			if liveTable.Indexes[index.Name] {
				continue
			}
			changes = append(changes, Change{
				Action: "create_index",
				Table:  table.Name,
				Column: index.Name,
				SQL:    table.IndexDDL(dialect, index),
			})
		}
		for name := range liveTable.Columns {
			if _, ok := table.Column(name); ok {
				continue
			}
			if name == "rowid" {
				continue
			}
			changes = append(changes, Change{Action: "extra_column", Table: table.Name, Column: name, Order: order})
		}
	}
	// Statements must run in phase order: a table has to exist before its
	// columns are altered or its indexes are created.
	sort.SliceStable(changes, func(i, j int) bool {
		left, right := phaseRank(changes[i]), phaseRank(changes[j])
		if left != right {
			return left < right
		}
		// Inside a phase keep the declaration order (which Export already
		// topologically sorted by foreign key dependency).
		if changes[i].Order != changes[j].Order {
			return changes[i].Order < changes[j].Order
		}
		return changes[i].String() < changes[j].String()
	})
	return changes, nil
}

// phaseRank orders DDL statements so dependencies come first.
func phaseRank(change Change) int {
	switch change.Action {
	case "create_table":
		return 0
	case "add_column":
		return 1
	case "create_index":
		return 2
	default:
		return 3
	}
}

func addColumnSQL(dialect Dialect, table Table, column Column) string {
	definition := column.Name + " " + sqlType(dialect, column.Kind)
	if column.Default != "" {
		definition += " NOT NULL DEFAULT " + column.Default
	} else if column.NotNull {
		// Adding a NOT NULL column without a default would fail on non-empty
		// tables; add it nullable and report the mismatch on the next pass.
		definition += " NULL"
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table.Name, definition)
}

// Apply executes a plan. Report-only changes carry no SQL and are skipped.
func Apply(ctx context.Context, conn dal.Conn, changes []Change) error {
	for _, change := range changes {
		if change.SQL == "" {
			continue
		}
		if _, err := conn.Exec(ctx, change.SQL); err != nil {
			return fmt.Errorf("schema: apply %s: %w", change, err)
		}
	}
	return nil
}

// Ensure aligns the database and records the declaration fingerprint.
func Ensure(ctx context.Context, conn dal.Conn, dialect Dialect, want []Table) ([]Change, error) {
	changes, err := Plan(ctx, conn, dialect, want)
	if err != nil {
		return nil, err
	}
	if err := Apply(ctx, conn, changes); err != nil {
		return changes, err
	}
	fingerprint := Fingerprint(want)
	query := "INSERT INTO schema_state (id, fingerprint, applied_at) VALUES (1, ?, ?) " +
		"ON CONFLICT (id) DO UPDATE SET fingerprint = excluded.fingerprint, applied_at = excluded.applied_at"
	statement := rebind(dialect, query)
	if _, err := conn.Exec(ctx, statement, fingerprint, dal.TimeArg(dialect, time.Now())); err != nil {
		return changes, fmt.Errorf("schema: record fingerprint: %w", err)
	}
	return changes, nil
}

func rebind(dialect Dialect, query string) string {
	if dialect != Postgres {
		return query
	}
	var builder strings.Builder
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			builder.WriteString(fmt.Sprintf("$%d", n))
			continue
		}
		builder.WriteByte(query[i])
	}
	return builder.String()
}

type liveTable struct {
	Columns map[string]bool
	Indexes map[string]bool
}

func inspect(ctx context.Context, conn dal.Conn, dialect Dialect) (map[string]liveTable, error) {
	out := map[string]liveTable{}
	var names []string
	var err error
	if dialect == SQLite {
		names, err = queryStrings(ctx, conn,
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	} else {
		names, err = queryStrings(ctx, conn,
			"SELECT tablename FROM pg_tables WHERE schemaname = current_schema()")
	}
	if err != nil {
		return nil, fmt.Errorf("schema: inspect tables: %w", err)
	}
	for _, name := range names {
		columns, err := inspectColumns(ctx, conn, dialect, name)
		if err != nil {
			return nil, err
		}
		indexes, err := inspectIndexes(ctx, conn, dialect, name)
		if err != nil {
			return nil, err
		}
		out[name] = liveTable{Columns: columns, Indexes: indexes}
	}
	return out, nil
}

func inspectColumns(ctx context.Context, conn dal.Conn, dialect Dialect, table string) (map[string]bool, error) {
	columns := map[string]bool{}
	if dialect == SQLite {
		rows, err := conn.Query(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
		if err != nil {
			return nil, fmt.Errorf("schema: pragma %s: %w", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				cid      int
				name     string
				typeName string
				notNull  int
				defaultV any
				pk       int
			)
			if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultV, &pk); err != nil {
				return nil, err
			}
			columns[name] = true
		}
		return columns, rows.Err()
	}
	names, err := queryStrings(ctx, conn,
		"SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1", table)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		columns[name] = true
	}
	return columns, nil
}

func inspectIndexes(ctx context.Context, conn dal.Conn, dialect Dialect, table string) (map[string]bool, error) {
	indexes := map[string]bool{}
	var names []string
	var err error
	if dialect == SQLite {
		rows, queryErr := conn.Query(ctx, fmt.Sprintf("PRAGMA index_list(%q)", table))
		if queryErr != nil {
			return nil, fmt.Errorf("schema: index_list %s: %w", table, queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				seq     int
				name    string
				unique  int
				origin  string
				partial int
			)
			if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
				return nil, err
			}
			indexes[name] = true
		}
		return indexes, rows.Err()
	}
	names, err = queryStrings(ctx, conn,
		"SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = $1", table)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		indexes[name] = true
	}
	return indexes, nil
}

func queryStrings(ctx context.Context, conn dal.Conn, query string, args ...any) ([]string, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
