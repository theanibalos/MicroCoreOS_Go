// SQLite Tool — Go-First implementation for MicroCoreOS
//
// Implements DbTool using Go's standard database/sql + modernc.org/sqlite (pure Go, no CGO).
//
// Setup:
//   - Runs migrations from domains/<domain>/migrations/*.sql on OnBootComplete.
//   - Each migration runs in its own transaction (auto-commit/rollback).
//   - Migration history is tracked in _migrations_history table.
//
// Swap to PostgreSQL: create tools/postgresdbtool implementing DbTool with name "db".
// Plugins require ZERO changes.
package drivers

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"microcoreos-go/core"
	"microcoreos-go/tools/dbtool"

	_ "modernc.org/sqlite"
)

// ─── sqliteTx ────────────────────────────────────────────────────────────────

type sqliteTx struct {
	tx         *sql.Tx
	rolledBack bool
}

func (t *sqliteTx) Query(query string, args ...any) ([]dbtool.Row, error) {
	rows, err := t.tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return scanRows(rows)
}

func (t *sqliteTx) QueryOne(query string, args ...any) (dbtool.Row, error) {
	rows, err := t.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (t *sqliteTx) Exec(query string, args ...any) (int64, error) {
	res, err := t.tx.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(query)), "INSERT") {
		return res.LastInsertId()
	}
	return res.RowsAffected()
}

func (t *sqliteTx) Commit() error {
	return t.tx.Commit()
}

func (t *sqliteTx) Rollback() error {
	if t.rolledBack {
		return nil
	}
	t.rolledBack = true
	return t.tx.Rollback()
}

// ─── SqliteTool ──────────────────────────────────────────────────────────────

// SqliteTool implements DbTool using SQLite via database/sql.
//
// Configuration (env vars):
//   - SQLITE_TOOL_NAME      — tool name registered in the Container (default: "db").
//     Change this to run multiple SQLite instances side-by-side (e.g. "db", "cache_db").
//   - SQLITE_DB_PATH        — file path for the SQLite database (default: "database.db").
//   - SQLITE_MIGRATIONS_DIR — root directory scanned for migrations (default: "domains").
//     Each subdirectory of this root is treated as a domain; migrations live in
//     <dir>/<domain>/migrations/*.sql.
type SqliteTool struct {
	core.BaseToolDefaults
	name          string
	path          string
	migrationsDir string
	db            *sql.DB
}

func init() {
	core.RegisterTool(func() core.Tool { return NewSqliteTool() })
}

// NewSqliteTool creates a SqliteTool. Configuration comes from env vars.
func NewSqliteTool() *SqliteTool {
	name := os.Getenv("SQLITE_TOOL_NAME")
	if name == "" {
		name = "db"
	}
	path := os.Getenv("SQLITE_DB_PATH")
	if path == "" {
		path = "database.db"
	}
	migrationsDir := os.Getenv("SQLITE_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "domains"
	}
	return &SqliteTool{name: name, path: path, migrationsDir: migrationsDir}
}

func (s *SqliteTool) Name() string { return s.name }

func (s *SqliteTool) Setup() error {
	fmt.Printf("[SqliteTool] Opening %s...\n", s.path)
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return fmt.Errorf("sqlite: cannot open %q: %w", s.path, err)
	}
	// SQLite performs best with a single writer connection.
	db.SetMaxOpenConns(1)

	// Enable WAL mode and foreign keys.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("sqlite: WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("sqlite: foreign keys: %w", err)
	}

	// Migration history table.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations_history (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		domain     TEXT NOT NULL,
		filename   TEXT NOT NULL,
		applied_at TEXT DEFAULT (datetime('now')),
		UNIQUE(domain, filename)
	)`); err != nil {
		return fmt.Errorf("sqlite: create migrations table: %w", err)
	}

	s.db = db
	fmt.Println("[SqliteTool] Ready (WAL mode, FK enabled).")
	return nil
}

func (s *SqliteTool) GetInterfaceDescription() string {
	return `SQLite DB Tool (` + s.name + `): Relational persistence via database/sql.
Migrations in ` + s.migrationsDir + `/*/migrations/*.sql run automatically on boot.
Placeholders: use ? for all args (standard database/sql convention).

── QUERIES ─────────────────────────────────────────────────────────────────────
  Query(sql, args...)    → ([]Row, error)  SELECT multiple rows.
  QueryOne(sql, args...) → (Row, error)    SELECT first row; Row is nil if no result.
  Exec(sql, args...)     → (int64, error)  INSERT/UPDATE/DELETE.
                                           INSERT → last inserted ID.
                                           UPDATE/DELETE → affected row count.
  Ping()                 → error           Check connectivity.

── TYPED SCAN (PREFERRED) ──────────────────────────────────────────────────────
  Map rows into typed structs using their json tags. Eliminates type assertions.

  dbtool.ScanOne[T](row Row) (T, error)
    Maps a single Row into T. Returns zero value of T (not error) when row is nil.

  dbtool.Scan[T](rows []Row) ([]T, error)
    Maps a slice of Rows into []T.

  Example:
    // Model (json tags drive the column mapping):
    type User struct {
        ID        int64  ` + "`" + `json:"id"` + "`" + `
        Username  string ` + "`" + `json:"username"` + "`" + `
        CreatedAt string ` + "`" + `json:"created_at"` + "`" + `
    }

    row, err := p.db.QueryOne("SELECT id, username, created_at FROM users WHERE id = ?", id)
    user, err := dbtool.ScanOne[User](row)  // user.ID, user.Username — no type assertions

    rows, err := p.db.Query("SELECT id, username, created_at FROM users")
    users, err := dbtool.Scan[User](rows)   // []User

  For queries that need fields not in the shared model (e.g. password_hash),
  define a private struct in the plugin:
    type loginRow struct {
        ID           int64  ` + "`" + `json:"id"` + "`" + `
        PasswordHash string ` + "`" + `json:"password_hash"` + "`" + `
    }
    creds, err := dbtool.ScanOne[loginRow](row)

── TRANSACTIONS ────────────────────────────────────────────────────────────────
  tx, err := db.Begin()
  defer tx.Rollback()        // no-op if already committed
  id,  err  = tx.Exec(...)
  rows, err = tx.Query(...)
  err = tx.Commit()`
}

// OnBootComplete runs pending SQL migrations from <migrationsDir>/*/migrations/*.sql.
func (s *SqliteTool) OnBootComplete(_ *core.Container) error {
	fmt.Printf("[SqliteTool:%s] Checking for pending migrations in %q...\n", s.name, s.migrationsDir)
	if _, err := os.Stat(s.migrationsDir); os.IsNotExist(err) {
		return nil
	}

	// Collect migrations: domain → sorted list of .sql files.
	type migration struct {
		domain   string
		filename string
		path     string
	}
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
		// Skip already applied.
		row, err := s.QueryOne("SELECT 1 FROM _migrations_history WHERE domain = ? AND filename = ?", m.domain, m.filename)
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

		// Run each statement in its own transaction.
		tx, err := s.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range splitSQL(string(content)) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s/%s failed: %w", m.domain, m.filename, err)
			}
		}
		if _, err := tx.Exec("INSERT INTO _migrations_history (domain, filename) VALUES (?, ?)", m.domain, m.filename); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		fmt.Printf("  [Migration] ✅ Applied %s/%s\n", m.domain, m.filename)
	}
	return nil
}

func (s *SqliteTool) Shutdown() error {
	if s.db != nil {
		fmt.Println("[SqliteTool] Connection closed.")
		return s.db.Close()
	}
	return nil
}

// ─── DbTool implementation ────────────────────────────────────────────────────

func (s *SqliteTool) Query(query string, args ...any) ([]dbtool.Row, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	return scanRows(rows)
}

func (s *SqliteTool) QueryOne(query string, args ...any) (dbtool.Row, error) {
	rows, err := s.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *SqliteTool) Exec(query string, args ...any) (int64, error) {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(query)), "INSERT") {
		return res.LastInsertId()
	}
	return res.RowsAffected()
}

func (s *SqliteTool) Begin() (dbtool.Tx, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx}, nil
}

func (s *SqliteTool) Ping() error {
	return s.db.Ping()
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func scanRows(rows *sql.Rows) ([]dbtool.Row, error) {
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var result []dbtool.Row
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(dbtool.Row, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// splitSQL splits a SQL script into individual statements.
// Correctly handles:
//   - Single-quoted string literals ('hello; world')  — semicolons inside are not delimiters
//   - Line comments (-- comment)                      — stripped entirely
//   - Blank / whitespace-only statements              — skipped
//
// Limitation: does not handle $$ dollar-quoted strings (PostgreSQL-specific).
func splitSQL(content string) []string {
	var stmts []string
	var current strings.Builder

	inString := false
	inLineComment := false
	runes := []rune(content)
	n := len(runes)

	for i := 0; i < n; i++ {
		ch := runes[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				current.WriteRune(' ') // preserve whitespace in output
			}
			// skip all other chars inside a comment
			continue
		}

		if inString {
			current.WriteRune(ch)
			if ch == '\'' {
				// Check for escaped quote: '' is a literal single quote in SQL
				if i+1 < n && runes[i+1] == '\'' {
					current.WriteRune('\'')
					i++ // consume both
				} else {
					inString = false
				}
			}
			continue
		}

		// Not in string or comment
		switch {
		case ch == '-' && i+1 < n && runes[i+1] == '-':
			inLineComment = true
			i++ // skip second '-'
		case ch == '\'':
			inString = true
			current.WriteRune(ch)
		case ch == ';':
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	// Final statement without trailing semicolon
	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}
