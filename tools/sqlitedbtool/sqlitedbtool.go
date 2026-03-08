// SQLite Tool — MicroCoreOS DB Implementation
//
// Self-contained package: interface + types + implementation in one place.
// Uses modernc.org/sqlite (pure Go, no CGO).
//
// PLACEHOLDERS: $1, $2, $3... (same as PostgresTool — normalized to ? internally)
//
// INSERT — uses LastInsertId(), no RETURNING needed:
//
//	id, err := db.Insert("INSERT INTO users (name) VALUES ($1)", name)
//
// QUERIES:
//
//	rows, err := db.Query("SELECT id, name FROM users WHERE active = $1", true)
//	row,  err := db.QueryOne("SELECT * FROM users WHERE id = $1", 5)
//	n,    err := db.Exec("UPDATE users SET active = $1 WHERE id = $2", false, 5)
//
// TYPED SCAN (no JSON round-trip, reflection-based):
//
//	type User struct {
//	    ID    int64  `json:"id"`
//	    Name  string `json:"name"`
//	}
//	row,  err := db.QueryOne("SELECT id, name FROM users WHERE id = ?", id)
//	user, err := sqlitedbtool.ScanOne[User](row)   // nil row → zero value, no error
//	rows, err := db.Query("SELECT id, name FROM users")
//	users,err := sqlitedbtool.Scan[User](rows)
//
// TRANSACTIONS:
//
//	tx, err := db.Begin()
//	defer tx.Rollback()
//	id, err = tx.Insert("INSERT INTO orders (...) VALUES ($1)", ...)
//	err = tx.Commit()
//
// CONFIGURATION (env vars):
//
//	SQLITE_DB_PATH       — file path (default: "database.db"). Use ":memory:" for in-memory.
//	SQLITE_TOOL_NAME     — Container key (default: "db")
//	SQLITE_MIGRATIONS_DIR — root dir for migrations (default: "domains")
package sqlitedbtool

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"microcoreos-go/core"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// Row is a single result row keyed by column name.
// Values are natively typed by the SQLite driver: int64, float64, string, []byte.
type Row = map[string]any

// Tx represents an active SQLite transaction.
// Use ? placeholders. Call Rollback in a defer — it is a no-op after Commit.
type Tx interface {
	Query(sql string, args ...any) ([]Row, error)
	QueryOne(sql string, args ...any) (Row, error)
	// Insert executes an INSERT. Returns the last inserted row ID.
	Insert(sql string, args ...any) (int64, error)
	// Exec executes UPDATE or DELETE. Returns affected row count.
	Exec(sql string, args ...any) (int64, error)
	Commit() error
	Rollback() error
}

// SqliteTool is the interface plugins use for SQLite database access.
// Resolve in Inject() using:
//
//	p.db, err = core.GetTool[sqlitedbtool.SqliteTool](c, "db")
type SqliteTool interface {
	Query(sql string, args ...any) ([]Row, error)
	QueryOne(sql string, args ...any) (Row, error)
	// Insert executes an INSERT. Returns the last inserted row ID.
	Insert(sql string, args ...any) (int64, error)
	// Exec executes UPDATE or DELETE. Returns affected row count.
	Exec(sql string, args ...any) (int64, error)
	Begin() (Tx, error)
	Ping() error
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

// ScanOne maps a single Row into a typed struct T using its json tags.
// Uses reflection — no JSON round-trip, no type corruption.
// Returns the zero value of T (not an error) if row is nil.
//
//	user, err := sqlitedbtool.ScanOne[User](row)
func ScanOne[T any](row Row) (T, error) {
	var result T
	if row == nil {
		return result, nil
	}

	rv := reflect.ValueOf(&result).Elem()
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		colName := strings.SplitN(tag, ",", 2)[0]
		if colName == "" {
			continue
		}
		val, ok := row[colName]
		if !ok || val == nil {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		if err := assignValue(fv, val); err != nil {
			return result, fmt.Errorf("ScanOne: field %q (column %q): %w", field.Name, colName, err)
		}
	}
	return result, nil
}

// Scan maps a slice of Rows into []T using json tags.
//
//	users, err := sqlitedbtool.Scan[User](rows)
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

// assignValue sets fv from val, handling the native types SQLite returns.
// SQLite driver returns: int64, float64, string, []byte, nil.
func assignValue(fv reflect.Value, val any) error {
	rv := reflect.ValueOf(val)

	if rv.Type().AssignableTo(fv.Type()) {
		fv.Set(rv)
		return nil
	}

	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := val.(type) {
		case int64:
			fv.SetInt(v)
		case int32:
			fv.SetInt(int64(v))
		case int16:
			fv.SetInt(int64(v))
		case int8:
			fv.SetInt(int64(v))
		case int:
			fv.SetInt(int64(v))
		case float64:
			fv.SetInt(int64(v))
		case float32:
			fv.SetInt(int64(v))
		default:
			return fmt.Errorf("cannot assign %T to %s", val, fv.Type())
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch v := val.(type) {
		case uint64:
			fv.SetUint(v)
		case uint32:
			fv.SetUint(uint64(v))
		case int64:
			fv.SetUint(uint64(v))
		case int32:
			fv.SetUint(uint64(v))
		default:
			return fmt.Errorf("cannot assign %T to %s", val, fv.Type())
		}

	case reflect.Float32, reflect.Float64:
		switch v := val.(type) {
		case float64:
			fv.SetFloat(v)
		case float32:
			fv.SetFloat(float64(v))
		case int64:
			fv.SetFloat(float64(v))
		case int32:
			fv.SetFloat(float64(v))
		default:
			return fmt.Errorf("cannot assign %T to %s", val, fv.Type())
		}

	case reflect.String:
		switch v := val.(type) {
		case string:
			fv.SetString(v)
		case []byte:
			fv.SetString(string(v))
		case fmt.Stringer:
			fv.SetString(v.String())
		default:
			return fmt.Errorf("cannot assign %T to string", val)
		}

	case reflect.Bool:
		switch v := val.(type) {
		case bool:
			fv.SetBool(v)
		case int64:
			fv.SetBool(v != 0)
		default:
			return fmt.Errorf("cannot assign %T to bool", val)
		}

	case reflect.Slice:
		if fv.Type() == reflect.TypeOf([]byte(nil)) {
			switch v := val.(type) {
			case []byte:
				fv.SetBytes(v)
			case string:
				fv.SetBytes([]byte(v))
			default:
				return fmt.Errorf("cannot assign %T to []byte", val)
			}
		} else {
			return fmt.Errorf("cannot assign %T to %s", val, fv.Type())
		}

	default:
		if rv.Type().ConvertibleTo(fv.Type()) {
			fv.Set(rv.Convert(fv.Type()))
			return nil
		}
		return fmt.Errorf("cannot assign %T to %s", val, fv.Type())
	}
	return nil
}

// ─── Implementation ───────────────────────────────────────────────────────────

// sqliteTx wraps database/sql.Tx implementing the Tx interface.
type sqliteTx struct {
	tx   *sql.Tx
	done bool
}

func (t *sqliteTx) Query(query string, args ...any) ([]Row, error) {
	rows, err := t.tx.Query(normalizePlaceholders(query), args...)
	if err != nil {
		return nil, err
	}
	return scanSqliteRows(rows)
}

func (t *sqliteTx) QueryOne(query string, args ...any) (Row, error) {
	rows, err := t.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (t *sqliteTx) Insert(query string, args ...any) (int64, error) {
	result, err := t.tx.Exec(normalizePlaceholders(query), args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (t *sqliteTx) Exec(query string, args ...any) (int64, error) {
	result, err := t.tx.Exec(normalizePlaceholders(query), args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (t *sqliteTx) Commit() error {
	t.done = true
	return t.tx.Commit()
}

func (t *sqliteTx) Rollback() error {
	if t.done {
		return nil
	}
	t.done = true
	return t.tx.Rollback()
}

// SqliteDB implements SqliteTool using database/sql + modernc.org/sqlite.
type SqliteDB struct {
	core.BaseToolDefaults
	name          string
	dbPath        string
	migrationsDir string
	db            *sql.DB
}

func init() {
	core.RegisterTool(func() core.Tool { return newSqliteDB() })
}

func newSqliteDB() *SqliteDB {
	name := os.Getenv("SQLITE_TOOL_NAME")
	if name == "" {
		name = "db"
	}
	dbPath := os.Getenv("SQLITE_DB_PATH")
	if dbPath == "" {
		dbPath = "database.db"
	}
	migrationsDir := os.Getenv("SQLITE_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "domains"
	}
	return &SqliteDB{
		name:          name,
		dbPath:        dbPath,
		migrationsDir: migrationsDir,
	}
}

func (s *SqliteDB) Name() string { return s.name }

func (s *SqliteDB) Setup() error {
	db, err := sql.Open("sqlite", s.dbPath)
	if err != nil {
		return fmt.Errorf("sqlite: failed to open %q: %w", s.dbPath, err)
	}

	// SQLite is not safe for concurrent writes with multiple connections.
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better read concurrency.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return fmt.Errorf("sqlite: failed to enable WAL mode: %w", err)
	}
	// Enable foreign key enforcement.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return fmt.Errorf("sqlite: failed to enable foreign keys: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("sqlite: ping failed: %w", err)
	}
	s.db = db

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS _migrations_history (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			domain      TEXT NOT NULL,
			filename    TEXT NOT NULL,
			applied_at  TEXT DEFAULT (datetime('now')),
			UNIQUE(domain, filename)
		)`)
	if err != nil {
		return fmt.Errorf("sqlite: create migrations table: %w", err)
	}

	fmt.Printf("[SqliteDB] Ready (path=%q, WAL enabled).\n", s.dbPath)
	return nil
}

func (s *SqliteDB) GetInterfaceDescription() string {
	return `SQLite Tool (` + s.name + `): Embedded DB — modernc.org/sqlite (pure Go, no CGO).
Write SQLite SQL directly. ? placeholders. Migrations auto-run on boot.

── PLACEHOLDERS ──────────────────────────────────────────────────────────────
  Use $1, $2, $3... — same as PostgresTool. Normalized to ? internally.

── QUERIES ───────────────────────────────────────────────────────────────────
  Query(sql, args...)    → ([]Row, error)   SELECT multiple rows.
  QueryOne(sql, args...) → (Row, error)     SELECT one row; nil if not found.
  Exec(sql, args...)     → (int64, error)   UPDATE/DELETE → rows affected.
  Ping()                 → error

── INSERT ────────────────────────────────────────────────────────────────────
  Insert(sql, args...)   → (int64, error)   Returns last inserted row ID.
    id, err := db.Insert(
        "INSERT INTO users (name, email) VALUES ($1, $2)",
        name, email,
    )

── SCAN (reflection-based, no JSON round-trip) ───────────────────────────────
  type User struct {
      ID    int64  ` + "`" + `json:"id"` + "`" + `
      Name  string ` + "`" + `json:"name"` + "`" + `
  }
  row,   err := db.QueryOne("SELECT id, name FROM users WHERE id = $1", id)
  user,  err := sqlitedbtool.ScanOne[User](row)
  rows,  err := db.Query("SELECT id, name FROM users")
  users, err := sqlitedbtool.Scan[User](rows)

── TRANSACTIONS ──────────────────────────────────────────────────────────────
  tx, err := db.Begin()
  defer tx.Rollback()
  id,  err = tx.Insert("INSERT INTO orders (...) VALUES ($1)", ...)
  err = tx.Commit()`
}

func (s *SqliteDB) OnBootComplete(_ *core.Container) error {
	fmt.Printf("[SqliteDB:%s] Checking migrations in %q...\n", s.name, s.migrationsDir)
	if _, err := os.Stat(s.migrationsDir); os.IsNotExist(err) {
		return nil
	}

	type migration struct{ domain, filename, path string }
	var migrations []migration

	entries, err := os.ReadDir(s.migrationsDir)
	if err != nil {
		return fmt.Errorf("sqlite migrations: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		migDir := filepath.Join(s.migrationsDir, entry.Name(), "migrations")
		files, err := os.ReadDir(migDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var sqlFiles []string
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
				sqlFiles = append(sqlFiles, f.Name())
			}
		}
		sort.Strings(sqlFiles)
		for _, fname := range sqlFiles {
			migrations = append(migrations, migration{
				domain:   entry.Name(),
				filename: fname,
				path:     filepath.Join(migDir, fname),
			})
		}
	}

	for _, m := range migrations {
		row, err := s.QueryOne(
			"SELECT 1 FROM _migrations_history WHERE domain = $1 AND filename = $2",
			m.domain, m.filename,
		)
		if err != nil {
			return err
		}
		if row != nil {
			continue
		}

		fmt.Printf("  [Migration] Applying %s/%s...\n", m.domain, m.filename)
		content, err := os.ReadFile(m.path)
		if err != nil {
			return err
		}

		tx, err := s.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range splitSQL(string(content)) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback() //nolint:errcheck
				return fmt.Errorf("migration %s/%s failed: %w", m.domain, m.filename, err)
			}
		}
		if _, err := tx.Insert(
			"INSERT INTO _migrations_history (domain, filename) VALUES ($1, $2)",
			m.domain, m.filename,
		); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		fmt.Printf("  [Migration] Applied %s/%s\n", m.domain, m.filename)
	}
	return nil
}

func (s *SqliteDB) Shutdown() error {
	if s.db != nil {
		fmt.Println("[SqliteDB] Closing database.")
		return s.db.Close()
	}
	return nil
}

func (s *SqliteDB) Query(query string, args ...any) ([]Row, error) {
	rows, err := s.db.Query(normalizePlaceholders(query), args...)
	if err != nil {
		return nil, err
	}
	return scanSqliteRows(rows)
}

func (s *SqliteDB) QueryOne(query string, args ...any) (Row, error) {
	rows, err := s.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *SqliteDB) Insert(query string, args ...any) (int64, error) {
	result, err := s.db.Exec(normalizePlaceholders(query), args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *SqliteDB) Exec(query string, args ...any) (int64, error) {
	result, err := s.db.Exec(normalizePlaceholders(query), args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SqliteDB) Begin() (Tx, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx}, nil
}

func (s *SqliteDB) Ping() error {
	return s.db.Ping()
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// scanSqliteRows converts database/sql Rows into []Row.
// SQLite driver returns natively typed values: int64, float64, string, []byte.
func scanSqliteRows(rows *sql.Rows) ([]Row, error) {
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var result []Row
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(Row, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// normalizePlaceholders converts Postgres-style $1, $2, $3... placeholders
// to SQLite-style ? so plugins can use the same SQL dialect as PostgresTool.
func normalizePlaceholders(query string) string {
	var result strings.Builder
	inString := false
	runes := []rune(query)
	n := len(runes)
	for i := 0; i < n; i++ {
		ch := runes[i]
		if inString {
			result.WriteRune(ch)
			if ch == '\'' {
				if i+1 < n && runes[i+1] == '\'' {
					result.WriteRune('\'')
					i++
				} else {
					inString = false
				}
			}
			continue
		}
		if ch == '\'' {
			inString = true
			result.WriteRune(ch)
			continue
		}
		if ch == '$' && i+1 < n && runes[i+1] >= '1' && runes[i+1] <= '9' {
			// Skip the digits after $
			i++
			for i+1 < n && runes[i+1] >= '0' && runes[i+1] <= '9' {
				i++
			}
			result.WriteRune('?')
			continue
		}
		result.WriteRune(ch)
	}
	return result.String()
}

// splitSQL splits a SQL script into individual statements, handling
// single-quoted strings and line comments correctly.
func splitSQL(content string) []string {
	var stmts []string
	var current strings.Builder
	inString, inLineComment := false, false
	runes := []rune(content)
	n := len(runes)

	for i := 0; i < n; i++ {
		ch := runes[i]
		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				current.WriteRune(' ')
			}
			continue
		}
		if inString {
			current.WriteRune(ch)
			if ch == '\'' {
				if i+1 < n && runes[i+1] == '\'' {
					current.WriteRune('\'')
					i++
				} else {
					inString = false
				}
			}
			continue
		}
		switch {
		case ch == '-' && i+1 < n && runes[i+1] == '-':
			inLineComment = true
			i++
		case ch == '\'':
			inString = true
			current.WriteRune(ch)
		case ch == ';':
			if stmt := strings.TrimSpace(current.String()); stmt != "" {
				stmts = append(stmts, stmt)
			}
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}
