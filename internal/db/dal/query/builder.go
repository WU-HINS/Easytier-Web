// Package query compiles the abstract query forms used by BAL into dialect SQL.
package query

import (
	"fmt"
	"strings"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// Cond is one WHERE predicate.
type Cond struct {
	Column string
	Op     string
	Value  any
}

// Order is one ORDER BY term.
type Order struct {
	Column string
	Desc   bool
}

// Select builds a SELECT statement.
type Select struct {
	table   string
	columns []string
	where   []Cond
	order   []Order
	limit   int
}

// From starts a SELECT.
func From(table string) *Select { return &Select{table: table} }

// Columns restricts the projection.
func (s *Select) Columns(columns ...string) *Select {
	s.columns = append(s.columns, columns...)
	return s
}

// Where adds an AND predicate.
func (s *Select) Where(column, op string, value any) *Select {
	s.where = append(s.where, Cond{Column: column, Op: op, Value: value})
	return s
}

// OrderBy adds an ORDER BY term.
func (s *Select) OrderBy(column string, desc bool) *Select {
	s.order = append(s.order, Order{Column: column, Desc: desc})
	return s
}

// Limit caps the row count.
func (s *Select) Limit(n int) *Select {
	s.limit = n
	return s
}

// Build renders SQL and arguments.
func (s *Select) Build(d dal.Dialect) (string, []any) {
	columns := "*"
	if len(s.columns) > 0 {
		quoted := make([]string, 0, len(s.columns))
		for _, column := range s.columns {
			quoted = append(quoted, Quote(column))
		}
		columns = strings.Join(quoted, ", ")
	}
	var (
		builder strings.Builder
		args    []any
	)
	builder.WriteString("SELECT " + columns + " FROM " + Quote(s.table))
	clauses := make([]string, 0, len(s.where))
	for _, cond := range s.where {
		clauses = append(clauses, Quote(cond.Column)+" "+cond.Op+" "+Placeholder(d, len(args)+1))
		args = append(args, cond.Value)
	}
	if len(clauses) > 0 {
		builder.WriteString(" WHERE " + strings.Join(clauses, " AND "))
	}
	if len(s.order) > 0 {
		terms := make([]string, 0, len(s.order))
		for _, order := range s.order {
			term := Quote(order.Column)
			if order.Desc {
				term += " DESC"
			}
			terms = append(terms, term)
		}
		builder.WriteString(" ORDER BY " + strings.Join(terms, ", "))
	}
	if s.limit > 0 {
		builder.WriteString(fmt.Sprintf(" LIMIT %d", s.limit))
	}
	return builder.String(), args
}

// Insert builds an INSERT with optional upsert.
type Insert struct {
	table      string
	columns    []string
	values     []any
	conflict   []string
	updateCols []string
}

// Into starts an INSERT.
func Into(table string) *Insert { return &Insert{table: table} }

// Columns sets the column list together with its values.
func (i *Insert) Columns(columns ...string) *Insert {
	i.columns = append(i.columns, columns...)
	return i
}

// Values sets the row values, in column order.
func (i *Insert) Values(values ...any) *Insert {
	i.values = append(i.values, values...)
	return i
}

// OnConflict selects the conflict target and the columns to refresh.
func (i *Insert) OnConflict(target []string, update []string) *Insert {
	i.conflict = target
	i.updateCols = update
	return i
}

// Build renders SQL and arguments.
func (i *Insert) Build(d dal.Dialect) (string, []any) {
	quoted := make([]string, 0, len(i.columns))
	placeholders := make([]string, 0, len(i.columns))
	for index := range i.columns {
		quoted = append(quoted, Quote(i.columns[index]))
		placeholders = append(placeholders, Placeholder(d, index+1))
	}
	statement := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		Quote(i.table), strings.Join(quoted, ", "), strings.Join(placeholders, ", "))
	if len(i.conflict) > 0 {
		target := make([]string, 0, len(i.conflict))
		for _, column := range i.conflict {
			target = append(target, Quote(column))
		}
		if len(i.updateCols) == 0 {
			statement += " ON CONFLICT (" + strings.Join(target, ", ") + ") DO NOTHING"
		} else {
			assignments := make([]string, 0, len(i.updateCols))
			for _, column := range i.updateCols {
				assignments = append(assignments, fmt.Sprintf("%s = excluded.%s", Quote(column), Quote(column)))
			}
			statement += " ON CONFLICT (" + strings.Join(target, ", ") + ") DO UPDATE SET " + strings.Join(assignments, ", ")
		}
	}
	return statement, append([]any(nil), i.values...)
}

// Update builds an UPDATE statement.
type Update struct {
	table string
	set   []string
	args  []any
	where []Cond
}

// Table starts an UPDATE.
func Table(name string) *Update { return &Update{table: name} }

// Set assigns a column.
func (u *Update) Set(column string, value any) *Update {
	u.set = append(u.set, column)
	u.args = append(u.args, value)
	return u
}

// Where adds an AND predicate.
func (u *Update) Where(column, op string, value any) *Update {
	u.where = append(u.where, Cond{Column: column, Op: op, Value: value})
	return u
}

// Build renders SQL and arguments.
func (u *Update) Build(d dal.Dialect) (string, []any) {
	args := append([]any(nil), u.args...)
	assignments := make([]string, 0, len(u.set))
	for index, column := range u.set {
		assignments = append(assignments, fmt.Sprintf("%s = %s", Quote(column), Placeholder(d, index+1)))
	}
	statement := fmt.Sprintf("UPDATE %s SET %s", Quote(u.table), strings.Join(assignments, ", "))
	clauses := make([]string, 0, len(u.where))
	for _, cond := range u.where {
		clauses = append(clauses, Quote(cond.Column)+" "+cond.Op+" "+Placeholder(d, len(args)+1))
		args = append(args, cond.Value)
	}
	if len(clauses) > 0 {
		statement += " WHERE " + strings.Join(clauses, " AND ")
	}
	return statement, args
}

// Delete builds a DELETE statement.
type Delete struct {
	table string
	where []Cond
}

// From starts a DELETE.
func From_(table string) *Delete { return &Delete{table: table} }

// DeleteFrom starts a DELETE (alias kept explicit for readability).
func DeleteFrom(table string) *Delete { return &Delete{table: table} }

// Where adds an AND predicate.
func (d *Delete) Where(column, op string, value any) *Delete {
	d.where = append(d.where, Cond{Column: column, Op: op, Value: value})
	return d
}

// Build renders SQL and arguments.
func (d *Delete) Build(dialect dal.Dialect) (string, []any) {
	args := make([]any, 0, len(d.where))
	clauses := make([]string, 0, len(d.where))
	for _, cond := range d.where {
		clauses = append(clauses, Quote(cond.Column)+" "+cond.Op+" "+Placeholder(dialect, len(args)+1))
		args = append(args, cond.Value)
	}
	statement := "DELETE FROM " + Quote(d.table)
	if len(clauses) > 0 {
		statement += " WHERE " + strings.Join(clauses, " AND ")
	}
	return statement, args
}
