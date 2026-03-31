# Review: Schema Migration via Silently-Ignored ALTER TABLE Is Fragile

## Problem

[internal/database/db.go lines 125–127](../internal/database/db.go#L125) handles the schema migration for the `lcn` column by issuing an `ALTER TABLE` and discarding both the result and any error:

```go
// Migration: add lcn column to existing databases that predate this column.
// SQLite returns an error if the column already exists; we intentionally ignore it.
_, _ = db.Exec(`ALTER TABLE channels ADD COLUMN lcn INTEGER`)
```

The approach relies on SQLite's error for a duplicate column as the signal that the migration has already been applied.

## Why It Is an Issue

### 1. Errors are indistinguishable

SQLite returns the same error type for "column already exists" as it does for "disk full", "database is locked", or "I/O error". By discarding the error entirely, a genuine failure to add the column is silently swallowed. The application starts up, looks healthy, and then misbehaves because `lcn` values are never stored.

### 2. It does not scale

This approach works for one migration. With two or three migrations, the `Open` function becomes a list of silent `_, _ = db.Exec(...)` calls, each relying on side effects to detect whether they've already run. There is no way to know which migrations have been applied, roll one back, or apply them selectively.

### 3. The pattern is not how database migrations are done

The standard practice — in every language, ORM, and database toolkit — is to maintain a **version table** that records which migrations have been applied. On startup, the application checks which migrations are pending and runs only those.

### 4. It conflates schema initialisation with migration

The `schema` constant creates tables with `CREATE TABLE IF NOT EXISTS`, which is idempotent. The `ALTER TABLE` below it is not idempotent, and the workaround for that (swallowing the error) creates the problems above. Ideally, the schema and migration logic would be separated cleanly.

## What Should Be Done Instead

Maintain a migration version table and apply migrations in order:

```go
const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version   INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);`

var migrations = []struct {
    version int
    sql     string
}{
    {1, `ALTER TABLE channels ADD COLUMN lcn INTEGER`},
    // Future migrations are appended here, never modified.
}

func applyMigrations(db *sql.DB) error {
    if _, err := db.Exec(createMigrationsTable); err != nil {
        return fmt.Errorf("creating migrations table: %w", err)
    }
    for _, m := range migrations {
        var count int
        if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&count); err != nil {
            return fmt.Errorf("checking migration %d: %w", m.version, err)
        }
        if count > 0 {
            continue // already applied
        }
        if _, err := db.Exec(m.sql); err != nil {
            return fmt.Errorf("applying migration %d: %w", m.version, err)
        }
        if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
            m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
            return fmt.Errorf("recording migration %d: %w", m.version, err)
        }
    }
    return nil
}
```

This:
- Reports genuine errors rather than silently ignoring them.
- Is idempotent — safe to call on every startup.
- Scales to any number of future migrations.
- Makes it trivially auditable which migrations have been applied and when.

For a project of this scope, an embedded migration library such as [`golang-migrate/migrate`](https://github.com/golang-migrate/migrate) is likely overkill. The simple version table above is sufficient and adds no external dependencies.

## References

- [SQLite `ALTER TABLE` documentation](https://www.sqlite.org/lang_altertable.html) — errors if the column already exists
- [`golang-migrate/migrate`](https://github.com/golang-migrate/migrate) — popular Go migration library (reference for the pattern, not necessarily a recommendation to adopt)
- [Flyway migration principles](https://documentation.red-gate.com/fd/why-database-migrations-184127574.html) — version table approach
- [Go Wiki: "CodeReviewComments — Errors"](https://github.com/golang/go/wiki/CodeReviewComments#errors) — "Do not discard errors using `_` variables."
