// Package arch holds architecture tests: they fail the build when a layer
// boundary is crossed, which is the only way these rules survive refactors.
package arch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const NL = "\n"

const modulePrefix = "github.com/easytier/easytier-console/"

// forbiddenForBusiness are the packages business code must never import.
var forbiddenForBusiness = []string{
	"database/sql",
	modulePrefix + "internal/db/dal",
	modulePrefix + "internal/db/dal/query",
	modulePrefix + "internal/db/dl",
	modulePrefix + "internal/db/dsm",
}

func TestBusinessCodeNeverTouchesTheDatabaseInternals(t *testing.T) {
	root := repoRoot(t)
	businessDirs := businessPackages(t, root)
	for _, dir := range businessDirs {
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			continue
		}
		walkGo(t, filepath.Join(root, dir), func(path, content string) {
			if strings.HasSuffix(path, "_test.go") {
				// Tests may reach the driver layer to build fixtures.
				return
			}
			for _, forbidden := range forbiddenForBusiness {
				if strings.Contains(content, strconv.Quote(forbidden)) {
					t.Errorf("%s imports %s; business code must go through internal/db/bal", rel(root, path), forbidden)
				}
			}
		})
	}
}

func TestOnlyTheDataLayerImportsDatabaseSQL(t *testing.T) {
	root := repoRoot(t)
	walkGo(t, root, func(path, content string) {
		relPath := rel(root, path)
		if strings.HasPrefix(relPath, "internal/arch/") {
			// This file names the package it is banning.
			return
		}
		if strings.HasSuffix(path, "_test.go") {
			// Tests may reach the driver to build fixtures, but production code may not.
			return
		}
		if !strings.Contains(content, strconv.Quote("database/sql")) {
			return
		}
		if strings.HasPrefix(relPath, "internal/db/") {
			return
		}
		t.Errorf("%s imports database/sql; only internal/db/{dl,bal} may", relPath)
	})
}

func TestSchemaIsDeclaredInGoOnly(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "internal", "db"))
	if err != nil {
		t.Fatalf("read internal/db: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			t.Fatalf("found hand written SQL file %s; the schema lives in internal/db/dsm/complete_schema.go", entry.Name())
		}
	}
}

func walkGo(t *testing.T, root string, visit func(path, content string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == "client" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		visit(path, string(content))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func rel(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return value
}

// TestDaemonStartsTheReconcileLoop guards F-041: the central-network reconciler
// existed but was never started by the daemon, so plans were compiled and never
// delivered. Unit tests could not see it because they call the reconciler
// directly.
func TestDaemonStartsTheReconcileLoop(t *testing.T) {
	root := repoRoot(t)
	main := filepath.Join(root, "cmd", "easytier-webd", "main.go")
	content, err := os.ReadFile(main)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	body := string(content)
	if !strings.Contains(body, "enforcer.Run(") && !strings.Contains(body, "ReconcileAll(") {
		t.Fatal("cmd/easytier-webd/main.go never starts the central-network reconciler; device configs would never be pushed")
	}
}

// businessPackages enumerates every package that must go through internal/db/bal.
// It is derived from the tree rather than hard coded: a new package added later
// must not be able to escape the rule by being absent from a list.
func businessPackages(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	internalDir := filepath.Join(root, "internal")
	entries, err := os.ReadDir(internalDir)
	if err != nil {
		t.Fatalf("read internal: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "db" || name == "arch" {
			// The data layer owns the driver; arch names the forbidden packages.
			continue
		}
		dirs = append(dirs, filepath.Join("internal", name))
	}
	if _, err := os.Stat(filepath.Join(root, "cmd")); err == nil {
		dirs = append(dirs, "cmd")
	}
	if len(dirs) < 5 {
		t.Fatalf("only %d business packages found; the enumeration is broken", len(dirs))
	}
	return dirs
}

// TestBusinessCodeUsesTheProjectLogger guards log consistency: the console emits
// HiDNS style lines through internal/logging, so a stray stdlib log.Printf or
// fmt.Print would produce a second, unparseable format on the same stream.
func TestBusinessCodeUsesTheProjectLogger(t *testing.T) {
	root := repoRoot(t)
	walkGo(t, filepath.Join(root, "internal"), func(path, content string) {
		relPath := rel(root, path)
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		if strings.HasPrefix(relPath, "internal/logging/") {
			// The logger owns stdout.
			return
		}
		for _, line := range strings.Split(content, NL) {
			trimmed := strings.TrimSpace(line)
			if trimmed == strconv.Quote("log") || strings.HasPrefix(trimmed, "import "+strconv.Quote("log")) {
				t.Errorf("%s imports the stdlib logger; use internal/logging", relPath)
				break
			}
		}
		for _, call := range []string{"fmt.Print(", "fmt.Printf(", "fmt.Println("} {
			if strings.Contains(content, call) {
				t.Errorf("%s writes to stdout with %s; use internal/logging", relPath, call)
			}
		}
	})
}

// TestHandlersDoNotKnowDriverErrors keeps constraint translation in the data
// layer. A handler matching driver text is how a duplicate name became a 500 in
// r80: the check belongs next to the constraint, not next to the response writer.
func TestHandlersDoNotKnowDriverErrors(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{"sqlstate", "duplicate key value", "unique constraint failed"}
	walkGo(t, filepath.Join(root, "internal", "httpapi"), func(path, content string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		lowered := strings.ToLower(content)
		for _, needle := range forbidden {
			if strings.Contains(lowered, needle) {
				t.Errorf("%s matches driver text (%q); translate the error in internal/db/bal instead", rel(root, path), needle)
			}
		}
	})
}

// guardThenInsertReviewed lists the handlers allowed to read a guard value and
// then insert. Both are safe because the constraint or a lock enforces the rule;
// anything else has to be reviewed rather than added silently.
var guardThenInsertReviewed = map[string]string{
	"handleRegister":  "the unique username index rejects the loser and CreateUser maps it to 409",
	"handleAddMember": "AddMembersBounded takes the tenant row lock and counts inside the transaction",
}

// TestGuardThenInsertIsReviewed looks for the shape that produced F-137, F-140,
// F-141 and F-142: read an existence or count value, then insert based on it.
func TestGuardThenInsertIsReviewed(t *testing.T) {
	root := repoRoot(t)
	walkGo(t, filepath.Join(root, "internal", "httpapi"), func(path, content string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		for _, chunk := range strings.Split(content, "\nfunc ")[1:] {
			name := chunk
			if strings.HasPrefix(name, "(s *Server) ") {
				name = name[len("(s *Server) "):]
			}
			if index := strings.Index(name, "("); index > 0 {
				name = name[:index]
			}
			name = strings.TrimSpace(name)
			name = strings.TrimSpace(name)
			reads := strings.Contains(chunk, "Store.Exists") || strings.Contains(chunk, "Store.Count")
			writes := strings.Contains(chunk, "Store.Create") || strings.Contains(chunk, "Store.Add") || strings.Contains(chunk, "Store.Upsert")
			if !reads || !writes {
				continue
			}
			if _, ok := guardThenInsertReviewed[name]; ok {
				continue
			}
			t.Errorf("%s reads a guard value and then inserts; make the write atomic or add it to guardThenInsertReviewed with a reason", rel(root, path)+":"+name)
		}
	})
}

// TestLogsNeverCarrySecrets keeps credentials out of the log stream. String
// literals are stripped first, so a message that merely mentions the word
// ("wrong setup token") stays fine while a variable of that name does not.
func TestLogsNeverCarrySecrets(t *testing.T) {
	root := repoRoot(t)
	sinks := []string{
		".Info(", ".Warn(", ".Error(", ".Debug(",
		".Trace(", "log.Printf(", "log.Println(",
	}
	suspect := []string{"secret", "Secret", "password", "Password",
		"token", "Token", "hash", "Hash", "verifier"}
	for _, dir := range []string{"internal", "cmd"} {
		walkGo(t, filepath.Join(root, dir), func(path, content string) {
			if strings.HasSuffix(path, "_test.go") {
				return
			}
			for index, line := range strings.Split(content, NL) {
				code := stripStringLiterals(line)
				isSink := false
				for _, sink := range sinks {
					if strings.Contains(code, sink) {
						isSink = true
						break
					}
				}
				if !isSink {
					continue
				}
				for _, word := range suspect {
					for _, field := range identifierFields(code) {
						if field == word {
							t.Errorf("%s:%d logs %s; credentials never go to the log stream", rel(root, path), index+1, word)
						}
					}
				}
			}
		})
	}
}

const escapedQuoteRune = '\\'
const doubleQuoteRune = '"'

// stripStringLiterals removes double quoted text so identifiers inside messages
// are not mistaken for variables.
func stripStringLiterals(line string) string {
	var builder strings.Builder
	inString := false
	escaped := false
	for _, symbol := range line {
		switch {
		case inString && escaped:
			escaped = false
		case inString && symbol == escapedQuoteRune:
			escaped = true
		case symbol == doubleQuoteRune:
			inString = !inString
		case !inString:
			builder.WriteRune(symbol)
		}
	}
	return builder.String()
}

// identifierFields lists the identifiers on a line; dotted names contribute only
// their last segment, so both secret and cfg.Secret are reported.
func identifierFields(code string) []string {
	fields := []string{}
	current := strings.Builder{}
	flush := func() {
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}
	for _, symbol := range code {
		isLetter := (symbol >= 'a' && symbol <= 'z') || (symbol >= 'A' && symbol <= 'Z')
		isDigit := symbol >= '0' && symbol <= '9'
		if isLetter || isDigit || symbol == '_' {
			current.WriteRune(symbol)
			continue
		}
		flush()
	}
	flush()
	return fields
}

// TestTenantTablesAreAlwaysQueriedWithAUser keeps IDOR out of the data layer:
// every statement built against a tenant owned table must carry a user_id
// predicate. A window based scan was tried first and proved vacuous: removing one
// filter left another inside the window, so the statement span is what counts.
func TestTenantTablesAreAlwaysQueriedWithAUser(t *testing.T) {
	root := repoRoot(t)
	tenantTables := []string{
		"networks", "network_members", "devices",
		"network_credentials", "running_configs",
	}
	scanned := 0
	walkGo(t, filepath.Join(root, "internal", "db", "bal"), func(path, content string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		for _, opener := range []string{"query.From(", "query.Into(", "query.Table("} {
			for offset := 0; ; {
				at := strings.Index(content[offset:], opener)
				if at < 0 {
					break
				}
				at += offset
				tail := content[at:]
				end := strings.Index(tail, ".Build(")
				if end < 0 {
					offset = at + len(opener)
					continue
				}
				statement := tail[:end]
				offset = at + end
				for _, table := range tenantTables {
					if !strings.Contains(statement, "\""+table+"\"") {
						continue
					}
					scanned++
					if !strings.Contains(statement, "user_id") {
						t.Errorf("%s queries %s without a user_id predicate: %s", rel(root, path), table, firstLine(statement))
					}
				}
			}
		}
	})
	if scanned < 12 {
		t.Fatalf("only %d tenant statements found; the scan is not reading the store", scanned)
	}

	// Hand written SQL is not built through query.From, so it needs its own pass:
	// from the assignment to the call that executes it.
	rawStatements := 0
	walkGo(t, filepath.Join(root, "internal", "db", "bal"), func(path, content string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		lines := strings.Split(content, "\n")
		for index, line := range lines {
			if !strings.Contains(line, "statement :=") && !strings.Contains(line, "Statement :=") {
				continue
			}
			span := []string{line}
			for next := index + 1; next < len(lines) && next < index+20; next++ {
				span = append(span, lines[next])
				if strings.Contains(lines[next], ".Exec(") || strings.Contains(lines[next], ".QueryRow(") || strings.Contains(lines[next], ".Query(") || strings.Contains(lines[next], ".Build(") {
					break
				}
			}
			statement := strings.Join(span, "\n")
			for _, table := range tenantTables {
				if !strings.Contains(statement, "query.Quote(\""+table+"\")") {
					continue
				}
				rawStatements++
				if !strings.Contains(statement, "user_id") {
					t.Errorf("%s:%d builds a %s statement without user_id", rel(root, path), index+1, table)
				}
			}
		}
	})
	if rawStatements < 3 {
		t.Fatalf("only %d hand written tenant statements found; the scan is not reading the store", rawStatements)
	}
}

func firstLine(text string) string {
	line := text
	if at := strings.Index(line, "\n"); at >= 0 {
		line = line[:at]
	}
	return strings.TrimSpace(line)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestServerErrorsNeverEchoTheCause keeps 500 responses free of store text. A
// wrapped driver error names tables, constraints and SQLSTATE, which is exactly
// what r19 and r201 removed one call site at a time; this makes it a rule.
func TestServerErrorsNeverEchoTheCause(t *testing.T) {
	root := repoRoot(t)
	checked := 0
	walkGo(t, filepath.Join(root, "internal", "httpapi"), func(path, content string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		for index, line := range strings.Split(content, "\n") {
			if strings.Contains(line, "StatusInternalServerError") {
				checked++
			}
			// A status held in a variable hides the same leak from the rule above:
			// three call sites used a status variable and slipped through (review team, r140).
			if strings.Contains(line, "writeError(w, status") {
				t.Errorf("%s:%d passes a computed status to writeError; writeError must name the status literally", rel(root, path), index+1)
			}
			if strings.Contains(line, "StatusInternalServerError") && strings.Contains(line, "err.Error()") {
				t.Errorf("%s:%d echoes the error on a 500; log it instead", rel(root, path), index+1)
			}
		}
	})
	if checked < 10 {
		t.Fatalf("only %d server error sites found; the scan is not reading the handlers", checked)
	}
}
