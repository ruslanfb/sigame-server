// Package db opens the SQLite database, applies the embedded goose migrations
// and offers a small transaction helper. Every other package receives a ready
// *sql.DB from here and writes its own hand-written SQL.
//
// Concurrency model: the pool is limited to ONE connection
// (db.SetMaxOpenConns(1)). SQLite allows a single writer at a time, and with
// WAL mode readers do not block that writer, but a pool of several
// connections would still hit SQLITE_BUSY under concurrent writes and turn
// every transaction into a retry loop. One connection serialises all
// statements in-process, which is exactly what a single-node, self-hosted
// server needs, and it makes ":memory:" databases usable in tests (each
// connection would otherwise see its own private in-memory database).
//
// Consequence for callers: inside Tx use only the *sql.Tx; calling a *sql.DB
// method while a transaction is open would wait for the single connection
// and deadlock (until the context expires).
package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"
)

// DriverName is the database/sql driver registered by modernc.org/sqlite.
const DriverName = "sqlite"

// UnicodeLowerFunc is the name of a SQL scalar function registered by this
// package on every connection it opens: ulower(text) returns the Unicode
// lower-case form of its argument (SQLite's built-in lower() and the NOCASE
// collation only fold ASCII, which is useless for Cyrillic pack names).
// Repositories use it for case-insensitive LIKE searches and sorting.
const UnicodeLowerFunc = "ulower"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// pragma is a per-connection SQLite setting.
type pragma struct{ name, value string }

// pragmas are applied to every connection. journal_mode is persistent in the
// database file, the others are connection-scoped.
var pragmas = []pragma{
	{"journal_mode", "WAL"},   // concurrent readers, fewer fsyncs
	{"busy_timeout", "5000"},  // wait up to 5s on a lock instead of failing
	{"foreign_keys", "ON"},    // pack_media references packs/media
	{"synchronous", "NORMAL"}, // safe with WAL, much faster than FULL
	{"temp_store", "MEMORY"},  // temp tables/indices in RAM
}

func init() {
	// Registered once per process; the driver adds it to every new connection.
	sqlite.MustRegisterDeterministicScalarFunction(UnicodeLowerFunc, 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			switch v := args[0].(type) {
			case nil:
				return nil, nil
			case string:
				return strings.ToLower(v), nil
			case []byte:
				return strings.ToLower(string(v)), nil
			default:
				return strings.ToLower(fmt.Sprint(v)), nil
			}
		})
}

// Open opens (or creates) the SQLite database at path and applies the
// connection pragmas. path may be ":memory:" for tests. The returned pool is
// limited to a single connection (see the package comment).
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("db: empty database path")
	}
	sqlDB, err := sql.Open(DriverName, DSN(path))
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(0)

	// The DSN already carries the pragmas so that a connection recycled by
	// database/sql is configured identically; executing them here as well
	// verifies that the file is reachable and writable right away.
	for _, p := range pragmas {
		if _, err := sqlDB.ExecContext(ctx, "PRAGMA "+p.name+"="+p.value); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("db: open %q: pragma %s: %w", path, p.name, err)
		}
	}
	return sqlDB, nil
}

// DSN builds the modernc.org/sqlite data source name for path, embedding the
// pragmas as _pragma query parameters. Exposed for diagnostics and tests.
func DSN(path string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p.name+"("+p.value+")")
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + q.Encode()
}

// gooseMu serialises access to goose's package-level configuration
// (base FS, dialect, logger) so that parallel tests stay race-free.
var gooseMu sync.Mutex

// Migrate applies all pending embedded migrations. It is idempotent.
func Migrate(ctx context.Context, sqlDB *sql.DB) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("db: migrate: set dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("db: migrate: %w", err)
	}
	return nil
}

// Tx runs fn inside a transaction. The transaction is committed when fn
// returns nil and rolled back when it returns an error or panics (the panic
// is re-raised after the rollback). fn must use only tx, never the *sql.DB.
func Tx(ctx context.Context, sqlDB *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}
