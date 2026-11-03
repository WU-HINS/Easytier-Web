package dl

import (
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// TestSQLiteDSNKeepsThePragmasWhenItAlreadyHasOptions covers F-230: a DSN with a
// query string used to be returned untouched, so WAL, foreign keys and the busy
// timeout were silently dropped (review team, r140).
func TestSQLiteDSNKeepsThePragmasWhenItAlreadyHasOptions(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
	}{
		{"plain path", "/tmp/et.db"},
		{"uri form", "file:/tmp/et.db"},
		{"with options", "/tmp/et.db?cache=shared"},
		{"with one pragma", "file:/tmp/et.db?_pragma=busy_timeout(9000)"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			normalized, err := NormalizeDSN(dal.SQLite, testCase.dsn)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			for _, wanted := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout("} {
				if !strings.Contains(normalized, wanted) {
					t.Fatalf("%s lost %q: %s", testCase.dsn, wanted, normalized)
				}
			}
			if strings.Count(normalized, "?") > 1 {
				t.Fatalf("%s grew more than one query separator: %s", testCase.dsn, normalized)
			}
		})
	}
}
