/*
DbTool Interface — MicroCoreOS Go-First DB Abstraction
======================================================

This file defines the DbTool interface. Plugins import ONLY this interface.
Swap the backend (SQLite → PostgreSQL → CockroachDB) by registering a different
Tool with name "db" — zero plugin changes required.

PUBLIC CONTRACT (what plugins use):

	rows, err  := db.Query("SELECT * FROM users WHERE age > ?", 18)
	row,  err  := db.QueryOne("SELECT * FROM users WHERE id = ?", 5)
	id,   err  := db.Exec("INSERT INTO users (name) VALUES (?)", "Ana")
	count, err := db.Exec("UPDATE users SET active = ? WHERE age < ?", false, 18)

	tx, err := db.Begin()
	defer tx.Rollback()       // no-op if already committed
	id, err = tx.Exec(...)
	err = tx.Commit()

	ok := db.Ping()

PLACEHOLDERS: Use standard '?' placeholders (database/sql convention).

Note: PostgreSQL uses $1, $2... — if you add a PostgresDbTool, you use

	native $N there. Plugins targeting both DBs should use a thin query
	helper or a migration strategy. This is intentional and honest.
*/
package dbtool

import (
	"encoding/json"
	"fmt"
)

// Row is a single result row keyed by column name.
type Row = map[string]any

// ScanOne maps a single Row into a typed struct T using its json tags.
// Returns the zero value of T (not an error) if row is nil.
//
// Usage:
//
//	row, err := db.QueryOne("SELECT id, username FROM users WHERE id = ?", id)
//	user, err := dbtool.ScanOne[models.User](row)
func ScanOne[T any](row Row) (T, error) {
	var zero T
	if row == nil {
		return zero, nil
	}
	b, err := json.Marshal(row)
	if err != nil {
		return zero, fmt.Errorf("dbtool.ScanOne: marshal row: %w", err)
	}
	var result T
	if err := json.Unmarshal(b, &result); err != nil {
		return zero, fmt.Errorf("dbtool.ScanOne: unmarshal into %T: %w", result, err)
	}
	return result, nil
}

// Scan maps a slice of Rows into a typed slice []T using json tags.
//
// Usage:
//
//	rows, err := db.Query("SELECT id, username FROM users")
//	users, err := dbtool.Scan[models.User](rows)
func Scan[T any](rows []Row) ([]T, error) {
	result := make([]T, 0, len(rows))
	for _, row := range rows {
		item, err := ScanOne[T](row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

// Tx represents an active database transaction.
type Tx interface {
	// Query executes a SELECT within the transaction. Returns all rows.
	Query(sql string, args ...any) ([]Row, error)
	// QueryOne executes a SELECT within the transaction. Returns first row or nil.
	QueryOne(sql string, args ...any) (Row, error)
	// Exec executes INSERT/UPDATE/DELETE within the transaction.
	// Returns the last inserted ID for INSERTs, or affected rows for others.
	Exec(sql string, args ...any) (int64, error)
	// Commit commits the transaction.
	Commit() error
	// Rollback rolls back the transaction. Safe to call after Commit (no-op).
	Rollback() error
}

// DbTool is the interface plugins use for database access.
// Resolve in Inject() using:
//
//	p.db, err = core.GetTool[dbtool.DbTool](c, "db")
type DbTool interface {
	// Query executes a SELECT. Returns all matching rows.
	Query(sql string, args ...any) ([]Row, error)
	// QueryOne executes a SELECT. Returns the first row, or nil if no result.
	QueryOne(sql string, args ...any) (Row, error)
	// Exec executes INSERT/UPDATE/DELETE.
	// For INSERT: returns last-inserted ID.
	// For UPDATE/DELETE: returns affected row count.
	Exec(sql string, args ...any) (int64, error)
	// Begin starts an explicit transaction.
	Begin() (Tx, error)
	// Ping verifies the database connection is alive.
	Ping() error
}
