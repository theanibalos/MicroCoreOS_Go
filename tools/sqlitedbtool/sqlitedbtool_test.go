package sqlitedbtool

import (
	"reflect"
	"testing"
)

type testUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Active   bool   `json:"active"`
}

func TestScanOne(t *testing.T) {
	t.Run("direct type match", func(t *testing.T) {
		row := Row{"id": int64(1), "username": "alice", "email": "alice@example.com", "active": true}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := testUser{ID: 1, Username: "alice", Email: "alice@example.com", Active: true}
		if !reflect.DeepEqual(user, expected) {
			t.Errorf("expected %+v, got %+v", expected, user)
		}
	})

	t.Run("sqlite bool as int64 (0/1)", func(t *testing.T) {
		// SQLite has no native bool — drivers may return int64 0/1
		row := Row{"id": int64(1), "active": int64(1)}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !user.Active {
			t.Errorf("expected Active=true (from int64 1), got false")
		}
	})

	t.Run("nil row returns zero value, no error", func(t *testing.T) {
		user, err := ScanOne[testUser](nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(user, testUser{}) {
			t.Errorf("expected zero value, got %+v", user)
		}
	})

	t.Run("nil column value skipped", func(t *testing.T) {
		row := Row{"id": int64(1), "username": nil}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != 1 || user.Username != "" {
			t.Errorf("unexpected: %+v", user)
		}
	})

	t.Run("partial row — missing columns stay zero", func(t *testing.T) {
		row := Row{"username": "partial"}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Username != "partial" || user.ID != 0 {
			t.Errorf("unexpected: %+v", user)
		}
	})

	t.Run("unknown column ignored", func(t *testing.T) {
		row := Row{"id": int64(1), "ghost_column": "ignored"}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != 1 {
			t.Errorf("expected ID=1, got %d", user.ID)
		}
	})

	t.Run("type mismatch returns error", func(t *testing.T) {
		row := Row{"id": "not-a-number"}
		_, err := ScanOne[testUser](row)
		if err == nil {
			t.Error("expected error for string→int64, got nil")
		}
	})

	t.Run("float64 coerced to int64 field", func(t *testing.T) {
		// Some SQLite queries return float64 for numeric expressions
		row := Row{"id": float64(42)}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != 42 {
			t.Errorf("expected ID=42, got %d", user.ID)
		}
	})

	t.Run("[]byte coerced to string field", func(t *testing.T) {
		row := Row{"username": []byte("bytename")}
		user, err := ScanOne[testUser](row)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Username != "bytename" {
			t.Errorf("expected username=bytename, got %q", user.Username)
		}
	})
}

func TestScan(t *testing.T) {
	t.Run("slice mapping", func(t *testing.T) {
		rows := []Row{
			{"id": int64(1), "username": "alice"},
			{"id": int64(2), "username": "bob"},
		}
		users, err := Scan[testUser](rows)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(users) != 2 || users[0].Username != "alice" || users[1].Username != "bob" {
			t.Errorf("unexpected result: %+v", users)
		}
	})

	t.Run("empty slice returns empty slice", func(t *testing.T) {
		users, err := Scan[testUser]([]Row{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(users) != 0 {
			t.Errorf("expected empty, got %d items", len(users))
		}
	})

	t.Run("error propagates from invalid row", func(t *testing.T) {
		rows := []Row{
			{"id": int64(1)},
			{"id": "invalid"},
		}
		_, err := Scan[testUser](rows)
		if err == nil {
			t.Error("expected error from invalid row, got nil")
		}
	})
}

func TestNormalizePlaceholders(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"SELECT * FROM t WHERE id = $1", "SELECT * FROM t WHERE id = ?"},
		{"INSERT INTO t (a, b) VALUES ($1, $2)", "INSERT INTO t (a, b) VALUES (?, ?)"},
		{"WHERE a = $1 AND b = $2 AND c = $10", "WHERE a = ? AND b = ? AND c = ?"},
		{"SELECT '$1' AS literal", "SELECT '$1' AS literal"}, // inside string — untouched
		{"SELECT * FROM t", "SELECT * FROM t"},               // no placeholders
	}
	for _, c := range cases {
		got := normalizePlaceholders(c.in)
		if got != c.out {
			t.Errorf("normalizePlaceholders(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestSplitSQL(t *testing.T) {
	t.Run("multiple statements", func(t *testing.T) {
		sql := "CREATE TABLE a (id INTEGER); CREATE TABLE b (id INTEGER);"
		stmts := splitSQL(sql)
		if len(stmts) != 2 {
			t.Errorf("expected 2 statements, got %d: %v", len(stmts), stmts)
		}
	})

	t.Run("ignores line comments", func(t *testing.T) {
		sql := "-- comment\nCREATE TABLE a (id INTEGER);"
		stmts := splitSQL(sql)
		if len(stmts) != 1 {
			t.Errorf("expected 1 statement, got %d", len(stmts))
		}
	})

	t.Run("preserves quoted semicolons", func(t *testing.T) {
		sql := "INSERT INTO t (v) VALUES ('a;b');"
		stmts := splitSQL(sql)
		if len(stmts) != 1 {
			t.Errorf("expected 1 statement, got %d: %v", len(stmts), stmts)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		stmts := splitSQL("")
		if len(stmts) != 0 {
			t.Errorf("expected 0 statements, got %d", len(stmts))
		}
	})
}

func TestIntegration(t *testing.T) {
	db := &SqliteDB{
		name:          "test",
		dbPath:        ":memory:",
		migrationsDir: "testdata",
	}
	if err := db.Setup(); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer db.Shutdown() //nolint:errcheck

	t.Run("create table and insert", func(t *testing.T) {
		_, err := db.Exec("CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)")
		if err != nil {
			t.Fatalf("create table: %v", err)
		}
		id, err := db.Insert("INSERT INTO users (name) VALUES ($1)", "alice")
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		if id != 1 {
			t.Errorf("expected id=1, got %d", id)
		}
	})

	t.Run("query one", func(t *testing.T) {
		row, err := db.QueryOne("SELECT id, name FROM users WHERE name = $1", "alice")
		if err != nil {
			t.Fatalf("queryOne: %v", err)
		}
		if row == nil {
			t.Fatal("expected row, got nil")
		}
		type u struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		user, err := ScanOne[u](row)
		if err != nil {
			t.Fatalf("ScanOne: %v", err)
		}
		if user.ID != 1 || user.Name != "alice" {
			t.Errorf("unexpected: %+v", user)
		}
	})

	t.Run("query nil when not found", func(t *testing.T) {
		row, err := db.QueryOne("SELECT id FROM users WHERE name = $1", "nobody")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if row != nil {
			t.Errorf("expected nil, got %v", row)
		}
	})

	t.Run("exec returns rows affected", func(t *testing.T) {
		n, err := db.Exec("UPDATE users SET name = $1 WHERE name = $2", "alice2", "alice")
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if n != 1 {
			t.Errorf("expected 1 row affected, got %d", n)
		}
	})

	t.Run("transaction commit", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback() //nolint:errcheck

		id, err := tx.Insert("INSERT INTO users (name) VALUES ($1)", "bob")
		if err != nil {
			t.Fatalf("tx insert: %v", err)
		}
		if id < 1 {
			t.Errorf("expected id >= 1, got %d", id)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}

		rows, err := db.Query("SELECT id, name FROM users")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(rows) != 2 {
			t.Errorf("expected 2 rows after commit, got %d", len(rows))
		}
	})

	t.Run("transaction rollback", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if _, err := tx.Insert("INSERT INTO users (name) VALUES ($1)", "rollback_user"); err != nil {
			t.Fatalf("tx insert: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}

		row, err := db.QueryOne("SELECT id FROM users WHERE name = $1", "rollback_user")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if row != nil {
			t.Error("expected rolled-back row to not exist")
		}
	})
}
