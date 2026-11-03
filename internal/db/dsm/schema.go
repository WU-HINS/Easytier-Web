// Package schema declares the control-plane data model in Go and derives DDL,
// introspection and alignment from that single declaration.
//
// Nothing else in the tree is allowed to know SQL: repositories consume this
// package, the aligner applies it, and business code never sees a table.
package dsm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// Dialect is re-exported so the schema declarations read naturally.
type Dialect = dal.Dialect

const (
	SQLite   = dal.SQLite
	Postgres = dal.Postgres
)

// Kind is a logical column type mapped per dialect.
type Kind string

const (
	Text      Kind = "text"
	Int       Kind = "int"
	BigInt    Kind = "bigint"
	Bool      Kind = "bool"
	Timestamp Kind = "timestamp"
	Serial    Kind = "serial"
	Float     Kind = "float"
)

// Column is one declared column.
type Column struct {
	Name       string
	Kind       Kind
	NotNull    bool
	PrimaryKey bool
	// Default is a SQL literal (e.g. "''", "0", "false"); empty means none.
	Default string
}

// Index is a (possibly partial, possibly unique) index declaration.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
	// Where is a raw predicate; only used by partial indexes.
	Where string
}

// Reference is a foreign key declaration.
type Reference struct {
	Columns  []string
	Table    string
	RefCols  []string
	OnDelete string
}

// Table is one declared table.
type Table struct {
	Name       string
	Columns    []Column
	PrimaryKey []string
	Indexes    []Index
	Checks     []string
	References []Reference
}

// Column looks a column up by name.
func (t Table) Column(name string) (Column, bool) {
	for _, column := range t.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return Column{}, false
}

// HasColumnSource reports whether a serial primary key drives the key.
func (t Table) HasSerialKey() bool {
	for _, column := range t.Columns {
		if column.Kind == Serial {
			return true
		}
	}
	return false
}

// FTSS never true; placeholder to keep vet quiet about unused helpers.
func sqlType(d Dialect, k Kind) string {
	switch d {
	case SQLite:
		switch k {
		case Bool:
			return "INTEGER"
		case Int, BigInt, Serial:
			return "INTEGER"
		case Float:
			return "REAL"
		default:
			return "TEXT"
		}
	default:
		switch k {
		case Bool:
			return "BOOLEAN"
		case Int:
			return "INTEGER"
		case BigInt, Serial:
			return "BIGINT"
		case Float:
			return "DOUBLE PRECISION"
		case Timestamp:
			return "TIMESTAMPTZ"
		default:
			return "TEXT"
		}
	}
}

// CreateTable renders the DDL for one table (no indexes).
func (t Table) CreateTable(d Dialect) string {
	defs := make([]string, 0, len(t.Columns)+len(t.References)+len(t.Checks))
	for _, column := range t.Columns {
		definition := column.Name + " " + sqlType(d, column.Kind)
		if column.Kind == Serial && d == SQLite {
			// SQLite spells an auto key inline and allows only one.
			defs = append(defs, column.Name+" INTEGER PRIMARY KEY AUTOINCREMENT")
			continue
		}
		if column.Kind == Serial && d == Postgres {
			definition = column.Name + " BIGSERIAL"
		}
		if column.PrimaryKey && t.HasSerialKey() {
			definition += " PRIMARY KEY"
		}
		if column.NotNull && !column.PrimaryKey {
			definition += " NOT NULL"
		}
		if column.Default != "" {
			definition += " DEFAULT " + column.Default
		}
		defs = append(defs, definition)
	}
	keyColumns := append([]string(nil), t.PrimaryKey...)
	if len(keyColumns) == 0 {
		for _, column := range t.Columns {
			if column.PrimaryKey {
				keyColumns = append(keyColumns, column.Name)
			}
		}
	}
	if len(keyColumns) > 0 && !t.HasSerialKey() {
		defs = append(defs, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(keyColumns, ", ")))
	}
	for _, check := range t.Checks {
		defs = append(defs, "CHECK ("+check+")")
	}
	for _, reference := range t.References {
		clause := fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(reference.Columns, ", "), reference.Table, strings.Join(reference.RefCols, ", "))
		if reference.OnDelete != "" {
			clause += " ON DELETE " + reference.OnDelete
		}
		defs = append(defs, clause)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n)", t.Name, strings.Join(defs, ",\n  "))
}

// IndexDDL renders one index statement, which keeps change records label-able.
func (t Table) IndexDDL(d Dialect, index Index) string {
	unique := ""
	if index.Unique {
		unique = "UNIQUE "
	}
	statement := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s(%s)",
		unique, index.Name, t.Name, strings.Join(index.Columns, ", "))
	if index.Where != "" {
		statement += " WHERE " + index.Where
	}
	return statement
}

// CreateIndexes renders the index DDL for a table.
func (t Table) CreateIndexes(d Dialect) []string {
	out := make([]string, 0, len(t.Indexes))
	for _, index := range t.Indexes {
		unique := ""
		if index.Unique {
			unique = "UNIQUE "
		}
		statement := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s(%s)",
			unique, index.Name, t.Name, strings.Join(index.Columns, ", "))
		if index.Where != "" {
			statement += " WHERE " + index.Where
		}
		out = append(out, statement)
	}
	return out
}

// Export returns the declaration in dependency order: a table is emitted after
// every table it references. Names break ties so generated DDL diffs stay
// stable across runs.
func Export() []Table {
	tables := All()
	byName := make(map[string]Table, len(tables))
	for _, table := range tables {
		byName[table.Name] = table
	}
	emitted := make(map[string]bool, len(tables))
	visiting := make(map[string]bool, len(tables))
	out := make([]Table, 0, len(tables))

	var visit func(name string)
	visit = func(name string) {
		if emitted[name] || visiting[name] {
			return
		}
		table, ok := byName[name]
		if !ok {
			return
		}
		visiting[name] = true
		for _, reference := range table.References {
			visit(reference.Table)
		}
		visiting[name] = false
		emitted[name] = true
		out = append(out, table)
	}

	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		visit(name)
	}
	return out
}
