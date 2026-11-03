package query

import (
	"fmt"
	"strings"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// Placeholder renders the nth parameter marker for a dialect.
func Placeholder(d dal.Dialect, position int) string {
	if d == dal.Postgres {
		return fmt.Sprintf("$%d", position)
	}
	return "?"
}

// Quote escapes an identifier. Both supported dialects use double quotes, but
// the helper exists so a future dialect only changes this function.
func Quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// Join renders a comma separated identifier list.
func Join(identifiers ...string) string {
	quoted := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		quoted = append(quoted, Quote(identifier))
	}
	return strings.Join(quoted, ", ")
}
