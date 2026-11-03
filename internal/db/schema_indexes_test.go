package db_test

import (
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dsm"
)

// TestDeclaredIndexesExistInTheEngine turns the schema declaration into a checked
// fact on both dialects. The declaration is the intent; this asks each engine
// what it actually built. A reconciler that silently skips a partial unique
// index, or a dialect that rejects the syntax, shows up here instead of as a
// duplicate row in production.
func TestDeclaredIndexesExistInTheEngine(t *testing.T) {
	target := freshTarget(t)
	dialect := target.store.Dialect()
	tables := dsm.Export()
	if len(tables) == 0 {
		t.Fatal("the schema declares no tables")
	}
	checked := 0
	for _, table := range tables {
		for _, index := range table.Indexes {
			checked++
			query := indexQuery(dialect, table.Name, index.Name)
			args := []any{index.Name}
			if dialect == dal.Postgres {
				args = []any{table.Name, index.Name}
			}
			definition, err := target.live(query, args...)
			if err != nil {
				t.Fatalf("%s.%s is declared but missing in the engine: %v", table.Name, index.Name, err)
			}
			lower := strings.ToLower(definition)
			if index.Unique != strings.Contains(lower, "unique") {
				t.Fatalf("%s.%s uniqueness mismatch (declared %v): %s", table.Name, index.Name, index.Unique, definition)
			}
			if index.Where != "" {
				predicate := strings.ToLower(strings.Join(strings.Fields(index.Where), " "))
				if !strings.Contains(strings.Join(strings.Fields(lower), " "), predicate) {
					t.Fatalf("%s.%s lost its partial predicate %q: %s", table.Name, index.Name, index.Where, definition)
				}
			}
		}
	}
	if checked < 8 {
		t.Fatalf("only %d indexes were checked; the schema export looks broken", checked)
	}
	t.Logf("verified %d indexes across %d tables on %s", checked, len(tables), dialect)
}

// indexQuery asks the engine for one index definition. Both dialects answer with
// the full CREATE INDEX text, so the caller can inspect uniqueness and the
// partial predicate without dialect specific branches.
func indexQuery(dialect dal.Dialect, table, index string) string {
	if dialect == dal.Postgres {
		return "SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND tablename = $1 AND indexname = $2"
	}
	return "SELECT sql FROM sqlite_master WHERE type = 'index' AND name = $1"
}
