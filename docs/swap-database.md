# Swapping the Database Tool

Each DB tool is a **self-contained package** with its own interface and SQL dialect.
Swapping is a deliberate, mechanical process — not automatic. The compiler will catch every missing step.

---

## SQLite → PostgreSQL

### 1. Environment (`.env`)

```diff
- SQLITE_DB_PATH=database.db
+ POSTGRES_DSN=postgres://user:pass@localhost:5432/mydb
```

Optional overrides (defaults shown):
```env
POSTGRES_TOOL_NAME=db
POSTGRES_MIGRATIONS_DIR=domains
POSTGRES_MAX_CONNS=10
```

### 2. Migration files (`domains/*/migrations/*.sql`)

| SQLite | PostgreSQL |
|--------|-----------|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` |
| `TEXT DEFAULT (datetime('now'))` | `TIMESTAMPTZ DEFAULT NOW()` |
| `REAL` | `NUMERIC` or `DOUBLE PRECISION` |
| `BLOB` | `BYTEA` |

Example:

```diff
- id INTEGER PRIMARY KEY AUTOINCREMENT,
- created_at TEXT DEFAULT (datetime('now'))
+ id BIGSERIAL PRIMARY KEY,
+ created_at TIMESTAMPTZ DEFAULT NOW()
```

### 3. Plugin imports

```diff
- "microcoreos-go/tools/sqlitedbtool"
+ "microcoreos-go/tools/postgresdbtool"
```

### 4. Struct fields and Inject()

```diff
- db sqlitedbtool.SqliteTool
+ db postgresdbtool.PostgresTool

- p.db, err = core.GetTool[sqlitedbtool.SqliteTool](c, "db")
+ p.db, err = core.GetTool[postgresdbtool.PostgresTool](c, "db")
```

### 5. SQL placeholders

```diff
- db.QueryOne("SELECT * FROM users WHERE id = ?", id)
+ db.QueryOne("SELECT * FROM users WHERE id = $1", id)

- db.Exec("UPDATE users SET name = ? WHERE id = ?", name, id)
+ db.Exec("UPDATE users SET name = $1 WHERE id = $2", name, id)
```

### 6. INSERT — add RETURNING

PostgreSQL requires `RETURNING` to get the inserted ID.

```diff
- id, err := db.Insert("INSERT INTO users (name) VALUES (?)", name)
+ id, err := db.Insert("INSERT INTO users (name) VALUES ($1) RETURNING id", name)
```

### 7. Scan helpers

```diff
- sqlitedbtool.ScanOne[User](row)
+ postgresdbtool.ScanOne[User](row)

- sqlitedbtool.Scan[User](rows)
+ postgresdbtool.Scan[User](rows)
```

### 8. Regenerate imports

```bash
go generate
go build ./...
```

---

## PostgreSQL → SQLite

The reverse of the above. Key differences:

- Remove `RETURNING id` from INSERT — `Insert()` uses `LastInsertId()` automatically
- Change `$1, $2...` placeholders to `?`
- Rewrite schema: `BIGSERIAL` → `INTEGER PRIMARY KEY AUTOINCREMENT`, etc.
- `bool` fields: SQLite has no native boolean; driver returns `int64` (0/1). `ScanOne` handles this automatically.

---

## Using two databases simultaneously

Register each tool under a different name via env vars:

```env
# Primary (Postgres)
POSTGRES_DSN=postgres://...
POSTGRES_TOOL_NAME=db

# Secondary (SQLite, e.g. for caching or local jobs)
SQLITE_DB_PATH=cache.db
SQLITE_TOOL_NAME=cache_db
```

In plugins that need both:

```go
p.db,    err = core.GetTool[postgresdbtool.PostgresTool](c, "db")
p.cache, err = core.GetTool[sqlitedbtool.SqliteTool](c, "cache_db")
```

---

## Checklist

- [ ] `.env` updated
- [ ] Migration SQL files rewritten
- [ ] Plugin imports changed
- [ ] Struct fields and `GetTool` calls updated
- [ ] SQL placeholders updated
- [ ] INSERT statements updated (add/remove RETURNING)
- [ ] Scan helper calls updated
- [ ] `go generate && go build ./...` passes
