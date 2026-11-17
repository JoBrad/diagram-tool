package steampipe

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeConn returns canned rows, or err, from Query.
type fakeConn struct {
	rows   *fakeRows
	err    error
	sql    string
	args   []any
	closed bool
}

func (f *fakeConn) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.sql, f.args = sql, args
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeConn) Close(context.Context) error {
	f.closed = true
	return nil
}

// fakeRows serves values row by row, as text or nil for NULL. The embedded
// interface stands in for the methods Query does not call.
type fakeRows struct {
	pgx.Rows
	columns []string
	values  [][]*string
	scanErr error
	err     error

	next   int
	closed bool
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	fields := make([]pgconn.FieldDescription, len(r.columns))
	for i, c := range r.columns {
		fields[i].Name = c
	}
	return fields
}

func (r *fakeRows) Next() bool {
	r.next++
	return r.next <= len(r.values)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	vals := r.values[r.next-1]
	if len(dest) != len(vals) {
		return fmt.Errorf("scan %d values into %d destinations", len(vals), len(dest))
	}
	for i, v := range vals {
		*dest[i].(**string) = v
	}
	return nil
}

func (r *fakeRows) Err() error { return r.err }

func (r *fakeRows) Close() { r.closed = true }

func ptr(s string) *string { return &s }

func TestDBQuery(t *testing.T) {
	rows := &fakeRows{
		columns: []string{"id", "name"},
		values:  [][]*string{{ptr("a"), ptr("first")}, {ptr("b"), nil}},
	}
	c := &fakeConn{rows: rows}
	db := &DB{conn: c}

	got, err := db.Query(context.Background(), "select id, name from t where id = $1", "a")
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{row("id", "a", "name", "first"), row("id", "b", "name", "NULL")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if c.sql != "select id, name from t where id = $1" || !reflect.DeepEqual(c.args, []any{"a"}) {
		t.Errorf("query = %q %v, want the SQL and arguments passed through", c.sql, c.args)
	}
	if !rows.closed {
		t.Error("Query left the result rows open")
	}

	if err := db.Close(context.Background()); err != nil || !c.closed {
		t.Errorf("Close = %v, closed = %v; want the connection closed", err, c.closed)
	}
}

func TestDBQueryErrors(t *testing.T) {
	boom := errors.New("boom")
	one := [][]*string{{ptr("a")}}
	tests := []struct {
		name string
		conn *fakeConn
	}{
		{"query", &fakeConn{err: boom}},
		{"scan", &fakeConn{rows: &fakeRows{columns: []string{"id"}, values: one, scanErr: boom}}},
		{"rows", &fakeConn{rows: &fakeRows{columns: []string{"id"}, values: one, err: boom}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &DB{conn: tt.conn}
			if _, err := db.Query(context.Background(), "select id from t"); !errors.Is(err, boom) {
				t.Errorf("err = %v, want %v", err, boom)
			}
			if tt.conn.rows != nil && !tt.conn.rows.closed {
				t.Error("Query left the result rows open after an error")
			}
		})
	}
}
