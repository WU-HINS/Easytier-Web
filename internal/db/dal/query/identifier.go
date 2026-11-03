package query

// Reserved records identifiers that need explicit handling in a future dialect.
// The supported dialects quote every identifier uniformly, so this stays a
// documentation point rather than a lookup table.
var Reserved = map[string]bool{
	"user": true, "order": true, "group": true, "table": true, "select": true,
}

// MustQuote reports whether an identifier collides with a reserved word.
func MustQuote(identifier string) bool { return Reserved[identifier] }
