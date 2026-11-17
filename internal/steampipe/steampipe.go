// Package steampipe reads Steampipe tables, whichever platform they hold.
// It knows the tables' shape (one row per resource, jsonb columns, a region
// and account column) but not what any table means; a platform's adapter
// says which tables and columns to read and turns rows into records.
//
// Every query names its columns. Table names come from the adapter or from
// discovery, are checked against tableName, and are the only text written
// into SQL; every value is a query argument.
package steampipe

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Row is one result row, keyed by column. Every column is read as text; nil
// is SQL NULL.
type Row map[string]*string

// Source runs a query and returns its rows.
type Source interface {
	Query(ctx context.Context, sql string, args ...any) ([]Row, error)
}

// Table is one table to read.
type Table struct {
	Name string
	// IDColumn is matched against the IDs a read asks for.
	IDColumn string
	// Columns are selected, in addition to the scope's region column.
	Columns []string
	// FoldIDCase matches IDs case-insensitively, for platforms whose IDs
	// are, such as Azure's.
	FoldIDCase bool
}

// Scope selects one account's rows in some regions.
type Scope struct {
	// AccountColumn and Account select the account, such as AWS's
	// account_id or Azure's subscription_id.
	AccountColumn, Account string
	// RegionColumn holds each row's region.
	RegionColumn string
	Regions      []string
	// DeletedColumn, when set, marks rows a sync has soft-deleted, such as
	// _deleted_at; rows with it set are left out.
	DeletedColumn string
}

// tableName and columnName guard the names written into SQL.
var (
	tableName  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	columnName = regexp.MustCompile(`^_?[a-z][a-z0-9_]*$`)
)

// Read returns the table's rows in scope, limited to ids when any are given,
// ordered by ID.
func Read(ctx context.Context, src Source, t Table, scope Scope, ids []string) ([]Row, error) {
	sql, args, err := t.sql(scope, ids)
	if err != nil {
		return nil, err
	}
	rows, err := src.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", t.Name, err)
	}
	return rows, nil
}

// sql returns the table's query and arguments.
func (t Table) sql(scope Scope, ids []string) (string, []any, error) {
	names := slices.Concat(
		[]string{t.Name, t.IDColumn, scope.AccountColumn, scope.RegionColumn}, t.Columns,
	)
	if scope.DeletedColumn != "" {
		names = append(names, scope.DeletedColumn)
	}
	if !tableName.MatchString(t.Name) {
		return "", nil, fmt.Errorf("unsafe table name %q", t.Name)
	}
	for _, n := range names[1:] {
		if !columnName.MatchString(n) {
			return "", nil, fmt.Errorf("%s: unsafe column name %q", t.Name, n)
		}
	}

	cols := make([]string, 0, len(t.Columns)+1)
	for _, c := range slices.Concat([]string{scope.RegionColumn}, t.Columns) {
		cols = append(cols, fmt.Sprintf("%s::text AS %s", c, c))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT %s FROM %s WHERE ", strings.Join(cols, ", "), t.Name)
	if scope.DeletedColumn != "" {
		fmt.Fprintf(&b, "%s IS NULL AND ", scope.DeletedColumn)
	}
	fmt.Fprintf(&b, "%s = $1 AND %s = ANY($2)", scope.AccountColumn, scope.RegionColumn)
	args := []any{scope.Account, scope.Regions}
	if len(ids) > 0 && t.FoldIDCase {
		fmt.Fprintf(&b, " AND lower(%s) = ANY($3)", t.IDColumn)
		lower := make([]string, len(ids))
		for i, id := range ids {
			lower[i] = strings.ToLower(id)
		}
		args = append(args, lower)
	} else if len(ids) > 0 {
		fmt.Fprintf(&b, " AND %s = ANY($3)", t.IDColumn)
		args = append(args, ids)
	}
	fmt.Fprintf(&b, " ORDER BY %s", t.IDColumn)
	return b.String(), args, nil
}

// Text returns a column's value, or "" for NULL or a column not selected.
func Text(row Row, column string) string {
	if v := row[column]; v != nil {
		return *v
	}
	return ""
}

// Decode unmarshals a JSON column into v. NULL leaves v unchanged.
func Decode(row Row, column string, v any) error {
	s := Text(row, column)
	if s == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return fmt.Errorf("decode %s: %w", column, err)
	}
	return nil
}

// JSONIDs reads a jsonb list of IDs: either plain strings, or objects that
// hold the ID under the first of keys present, such as RDS's
// {"VpcSecurityGroupId": ...}.
func JSONIDs(row Row, column string, keys []string) ([]string, error) {
	var items []json.RawMessage
	if err := Decode(row, column, &items); err != nil {
		return nil, err
	}
	var ids []string
	for _, raw := range items {
		var id string
		if json.Unmarshal(raw, &id) == nil {
			ids = append(ids, id)
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("decode %s: %w", column, err)
		}
		for _, k := range keys {
			if v, ok := obj[k].(string); ok && v != "" {
				ids = append(ids, v)
				break
			}
		}
	}
	return ids, nil
}

// Discovery describes the tables Discover looks for.
type Discovery struct {
	// Prefix is the table name prefix, such as "aws_".
	Prefix string
	// Required columns must all be present for a table to be returned.
	Required []string
	// Optional columns are reported per table when present.
	Optional []string
}

// Discovered is one table Discover found, with the optional columns it has.
type Discovered struct {
	Table   string
	Columns map[string]bool
}

// discoverSQL finds the tables with a prefix that have every required
// column, and lists which optional columns each has.
const discoverSQL = `SELECT table_name::text AS table_name,
	coalesce(string_agg(column_name::text, ',') FILTER (WHERE column_name = ANY($2)), '') AS columns
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name LIKE $1
GROUP BY table_name
HAVING count(column_name) FILTER (WHERE column_name = ANY($3)) = cardinality($3::text[])
ORDER BY table_name`

// Discover returns the tables d describes, in name order. A table name that
// fails the safety check is left out, so it never reaches SQL.
func Discover(ctx context.Context, src Source, d Discovery) ([]Discovered, error) {
	like := strings.ReplaceAll(d.Prefix, "_", `\_`) + "%"
	rows, err := src.Query(ctx, discoverSQL, like, d.Optional, d.Required)
	if err != nil {
		return nil, fmt.Errorf("discover Steampipe tables: %w", err)
	}
	var out []Discovered
	for _, row := range rows {
		name := Text(row, "table_name")
		if !strings.HasPrefix(name, d.Prefix) || !tableName.MatchString(name) {
			continue
		}
		cols := map[string]bool{}
		if s := Text(row, "columns"); s != "" {
			for _, c := range strings.Split(s, ",") {
				cols[c] = true
			}
		}
		out = append(out, Discovered{Table: name, Columns: cols})
	}
	return out, nil
}
