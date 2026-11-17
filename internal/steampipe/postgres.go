package steampipe

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is a Steampipe connection.
type DB struct {
	conn conn
}

// conn is the part of *pgx.Conn that DB uses. Tests swap in a fake so they
// exercise row reading without a database.
type conn interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Close(ctx context.Context) error
}

// Connect opens a Steampipe connection from the standard libpq environment
// variables (PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD, PGSSLMODE) and
// ~/.pgpass, so credentials never sit in the diagram config.
func Connect(ctx context.Context) (*DB, error) {
	conn, err := pgx.Connect(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("connect to Steampipe (set the PG* environment variables): %w", err)
	}
	return &DB{conn: conn}, nil
}

// Close closes the connection.
func (db *DB) Close(ctx context.Context) error {
	return db.conn.Close(ctx)
}

// Query runs sql and reads every column as text.
func (db *DB) Query(ctx context.Context, sql string, args ...any) ([]Row, error) {
	rows, err := db.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	var out []Row
	for rows.Next() {
		vals := make([]*string, len(fields))
		ptrs := make([]any, len(fields))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(Row, len(fields))
		for i, f := range fields {
			row[f.Name] = vals[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
