// PostgreSQL Tool — MicroCoreOS Standard DB Implementation
//
// Self-contained package: interface + types + implementation in one place.
// PostgreSQL is the reference standard. Plugins import this package and get everything.
//
// PLACEHOLDERS: $1, $2, $3... (Postgres native)
//
// INSERT — always use RETURNING to get the inserted ID:
//
//	id, err := db.Insert("INSERT INTO users (name) VALUES ($1) RETURNING id", name)
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
//	row,  err := db.QueryOne("SELECT id, name FROM users WHERE id = $1", id)
//	user, err := postgresdbtool.ScanOne[User](row)   // nil row → zero value, no error
//	rows, err := db.Query("SELECT id, name FROM users")
//	users,err := postgresdbtool.Scan[User](rows)
//
// TRANSACTIONS:
//
//	tx, err := db.Begin()
//	defer tx.Rollback()
//	id, err = tx.Insert("INSERT INTO orders (...) VALUES ($1) RETURNING id", ...)
//	err = tx.Commit()
//
// CONFIGURATION (env vars):
//
//	POSTGRES_DSN            — required. postgresql://user:pass@localhost:5432/dbname
//	POSTGRES_TOOL_NAME      — Container key (default: "db")
//	POSTGRES_MIGRATIONS_DIR — root dir for migrations (default: "domains")
//	POSTGRES_MAX_CONNS      — pool size (default: 10)
package postgresdbtool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"microcoreos-go/core"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// Row is a single result row keyed by column name.
// Values are natively typed by pgx: int32/int64, string, bool, time.Time, etc.
type Row = map[string]any

// Tx represents an active Postgres transaction.
// Use $N placeholders. Call Rollback in a defer — it is a no-op after Commit.
type Tx interface {
	Query(sql string, args ...any) ([]Row, error)
	QueryOne(sql string, args ...any) (Row, error)
	// Insert executes an INSERT with RETURNING. Returns the first returned column as int64.
	Insert(sql string, args ...any) (int64, error)
	// Exec executes UPDATE or DELETE. Returns affected row count.
	Exec(sql string, args ...any) (int64, error)
	Commit() error
	Rollback() error
}

// PostgresTool is the interface plugins use for Postgres database access.
// Resolve in Inject() using:
//
//	p.db, err = core.GetTool[postgresdbtool.PostgresTool](c, "db")
type PostgresTool interface {
	Query(sql string, args ...any) ([]Row, error)
	QueryOne(sql string, args ...any) (Row, error)
	// Insert executes an INSERT with RETURNING. Returns the first returned column as int64.
	// SQL must include RETURNING <id_col>: "INSERT INTO t (col) VALUES ($1) RETURNING id"
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
//	user, err := postgresdbtool.ScanOne[User](row)
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
//	users, err := postgresdbtool.Scan[User](rows)
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

// assignValue sets fv from val, handling the type coercions pgx produces.
// pgx returns native Go types: int16/int32/int64, float32/float64, string, bool, time.Time, []byte.
func assignValue(fv reflect.Value, val any) error {
	rv := reflect.ValueOf(val)

	// Direct assignment when types match exactly.
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
		v, ok := val.(bool)
		if !ok {
			return fmt.Errorf("cannot assign %T to bool", val)
		}
		fv.SetBool(v)

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
		// Last resort: reflect conversion (handles time.Time → time.Time, [16]byte, etc.)
		if rv.Type().ConvertibleTo(fv.Type()) {
			fv.Set(rv.Convert(fv.Type()))
			return nil
		}
		return fmt.Errorf("cannot assign %T to %s", val, fv.Type())
	}
	return nil
}

// ─── Implementation ───────────────────────────────────────────────────────────

// pgxTx wraps pgx.Tx implementing the Tx interface.
type pgxTx struct {
	tx   pgx.Tx
	done bool
}

func (t *pgxTx) Query(query string, args ...any) ([]Row, error) {
	rows, err := t.tx.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	return scanPgRows(rows)
}

func (t *pgxTx) QueryOne(query string, args ...any) (Row, error) {
	rows, err := t.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (t *pgxTx) Insert(query string, args ...any) (int64, error) {
	var id int64
	err := t.tx.QueryRow(context.Background(), query, args...).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres tx Insert: %w (SQL must include RETURNING <id_col>)", err)
	}
	return id, nil
}

func (t *pgxTx) Exec(query string, args ...any) (int64, error) {
	tag, err := t.tx.Exec(context.Background(), query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (t *pgxTx) Commit() error {
	t.done = true
	return t.tx.Commit(context.Background())
}

func (t *pgxTx) Rollback() error {
	if t.done {
		return nil
	}
	t.done = true
	return t.tx.Rollback(context.Background())
}

// PostgresDB implements PostgresTool using pgx/v5 connection pool.
type PostgresDB struct {
	core.BaseToolDefaults
	name          string
	dsn           string
	migrationsDir string
	maxConns      int32
	pool          *pgxpool.Pool
}

func init() {
	core.RegisterTool(func() core.Tool { return newPostgresDB() })
}

func newPostgresDB() *PostgresDB {
	name := os.Getenv("POSTGRES_TOOL_NAME")
	if name == "" {
		name = "db"
	}
	migrationsDir := os.Getenv("POSTGRES_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "domains"
	}
	maxConns := int32(10)
	if v := os.Getenv("POSTGRES_MAX_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConns = int32(n)
		}
	}
	return &PostgresDB{
		name:          name,
		dsn:           os.Getenv("POSTGRES_DSN"),
		migrationsDir: migrationsDir,
		maxConns:      maxConns,
	}
}

func (p *PostgresDB) Name() string { return p.name }

func (p *PostgresDB) Setup() error {
	if p.dsn == "" {
		return fmt.Errorf("postgres: POSTGRES_DSN is required")
	}
	cfg, err := pgxpool.ParseConfig(p.dsn)
	if err != nil {
		return fmt.Errorf("postgres: invalid DSN: %w", err)
	}
	cfg.MaxConns = p.maxConns

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("postgres: failed to create pool: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return fmt.Errorf("postgres: ping failed: %w", err)
	}
	p.pool = pool

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS _migrations_history (
			id         BIGSERIAL PRIMARY KEY,
			domain     TEXT NOT NULL,
			filename   TEXT NOT NULL,
			applied_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(domain, filename)
		)`)
	if err != nil {
		return fmt.Errorf("postgres: create migrations table: %w", err)
	}

	fmt.Printf("[PostgresDB] Ready (pool max_conns=%d).\n", p.maxConns)
	return nil
}

func (p *PostgresDB) GetInterfaceDescription() string {
	return `PostgreSQL Tool (` + p.name + `): Standard DB — pgx/v5 connection pool.
Write Postgres SQL directly. $N placeholders. Migrations auto-run on boot.

── PLACEHOLDERS ──────────────────────────────────────────────────────────────
  Use $1, $2, $3... for all args (Postgres native).

── QUERIES ───────────────────────────────────────────────────────────────────
  Query(sql, args...)    → ([]Row, error)   SELECT multiple rows.
  QueryOne(sql, args...) → (Row, error)     SELECT one row; nil if not found.
  Exec(sql, args...)     → (int64, error)   UPDATE/DELETE → rows affected.
  Ping()                 → error

── INSERT ────────────────────────────────────────────────────────────────────
  Insert(sql, args...)   → (int64, error)   Requires RETURNING <id_col>.
    id, err := db.Insert(
        "INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id",
        name, email,
    )

── SCAN (reflection-based, no JSON round-trip) ───────────────────────────────
  type User struct {
      ID    int64  ` + "`" + `json:"id"` + "`" + `
      Name  string ` + "`" + `json:"name"` + "`" + `
  }
  row,   err := db.QueryOne("SELECT id, name FROM users WHERE id = $1", id)
  user,  err := postgresdbtool.ScanOne[User](row)
  rows,  err := db.Query("SELECT id, name FROM users")
  users, err := postgresdbtool.Scan[User](rows)

── TRANSACTIONS ──────────────────────────────────────────────────────────────
  tx, err := db.Begin()
  defer tx.Rollback()
  id,  err = tx.Insert("INSERT INTO orders (...) VALUES ($1) RETURNING id", ...)
  err = tx.Commit()`
}

func (p *PostgresDB) OnBootComplete(_ *core.Container) error {
	fmt.Printf("[PostgresDB:%s] Checking migrations in %q...\n", p.name, p.migrationsDir)
	if _, err := os.Stat(p.migrationsDir); os.IsNotExist(err) {
		return nil
	}

	type migration struct{ domain, filename, path string }
	var migrations []migration

	entries, err := os.ReadDir(p.migrationsDir)
	if err != nil {
		return fmt.Errorf("postgres migrations: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		migDir := filepath.Join(p.migrationsDir, entry.Name(), "migrations")
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
		row, err := p.QueryOne(
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

		tx, err := p.Begin()
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
			"INSERT INTO _migrations_history (domain, filename) VALUES ($1, $2) RETURNING id",
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

func (p *PostgresDB) Shutdown() error {
	if p.pool != nil {
		p.pool.Close()
		fmt.Println("[PostgresDB] Connection pool closed.")
	}
	return nil
}

func (p *PostgresDB) Query(query string, args ...any) ([]Row, error) {
	rows, err := p.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	return scanPgRows(rows)
}

func (p *PostgresDB) QueryOne(query string, args ...any) (Row, error) {
	rows, err := p.Query(query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (p *PostgresDB) Insert(query string, args ...any) (int64, error) {
	var id int64
	err := p.pool.QueryRow(context.Background(), query, args...).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres Insert: %w (SQL must include RETURNING <id_col>)", err)
	}
	return id, nil
}

func (p *PostgresDB) Exec(query string, args ...any) (int64, error) {
	tag, err := p.pool.Exec(context.Background(), query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (p *PostgresDB) Begin() (Tx, error) {
	tx, err := p.pool.Begin(context.Background())
	if err != nil {
		return nil, err
	}
	return &pgxTx{tx: tx}, nil
}

func (p *PostgresDB) Ping() error {
	return p.pool.Ping(context.Background())
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// scanPgRows converts pgx.Rows into []Row using rows.Values().
// pgx returns natively typed values — no JSON round-trip.
func scanPgRows(rows pgx.Rows) ([]Row, error) {
	defer rows.Close()
	cols := rows.FieldDescriptions()
	var result []Row
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(Row, len(cols))
		for i, col := range cols {
			row[string(col.Name)] = vals[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
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
