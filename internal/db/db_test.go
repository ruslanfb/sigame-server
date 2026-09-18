package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/db"
)

func openMigrated(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(ctx, sqlDB))
	return sqlDB
}

func tableNames(t *testing.T, sqlDB *sql.DB) map[string]bool {
	t.Helper()
	rows, err := sqlDB.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type IN ('table','index') AND name NOT LIKE 'sqlite_%'`)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		out[name] = true
	}
	require.NoError(t, rows.Err())
	return out
}

func TestOpen_EmptyPath(t *testing.T) {
	_, err := db.Open(context.Background(), "")
	require.Error(t, err)
}

func TestOpen_Pragmas(t *testing.T) {
	sqlDB := openMigrated(t)
	ctx := context.Background()

	var fk int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk))
	require.Equal(t, 1, fk, "foreign_keys must be ON")

	var busy int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy))
	require.Equal(t, 5000, busy)

	var sync int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync))
	require.Equal(t, 1, sync, "synchronous=NORMAL is 1")

	var lowered string
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT ulower(?)", "Своя ИГРА Abc").Scan(&lowered))
	require.Equal(t, "своя игра abc", lowered)
}

func TestOpen_FileWAL(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/test.db"
	sqlDB, err := db.Open(ctx, path)
	require.NoError(t, err)
	defer sqlDB.Close()

	var mode string
	require.NoError(t, sqlDB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	require.Equal(t, "wal", mode)
	require.NoError(t, db.Migrate(ctx, sqlDB))
	require.True(t, tableNames(t, sqlDB)["packs"])
}

func TestMigrate_CreatesTablesAndIsIdempotent(t *testing.T) {
	sqlDB := openMigrated(t)
	names := tableNames(t, sqlDB)
	for _, want := range []string{
		"packs", "packs_name", "packs_updated",
		"media", "pack_media", "pack_media_media",
		"rooms", "game_results", "settings", "goose_db_version",
	} {
		require.Truef(t, names[want], "missing table/index %q", want)
	}

	// Second run must be a no-op.
	require.NoError(t, db.Migrate(context.Background(), sqlDB))
	var version int64
	require.NoError(t, sqlDB.QueryRowContext(context.Background(),
		`SELECT MAX(version_id) FROM goose_db_version`).Scan(&version))
	require.Equal(t, int64(1), version)
}

func TestMigrate_ForeignKeysCascade(t *testing.T) {
	sqlDB := openMigrated(t)
	ctx := context.Background()
	_, err := sqlDB.ExecContext(ctx, `INSERT INTO packs(id,name,doc,created_at,updated_at) VALUES('p1','n','{}',1,1)`)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `INSERT INTO media(id,kind,mime,ext,size,created_at) VALUES('m1','image','image/png','png',1,1)`)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `INSERT INTO pack_media(pack_id,media_id) VALUES('p1','m1')`)
	require.NoError(t, err)

	// Unknown media id is rejected by the foreign key.
	_, err = sqlDB.ExecContext(ctx, `INSERT INTO pack_media(pack_id,media_id) VALUES('p1','nope')`)
	require.Error(t, err)

	// Deleting the pack cascades to pack_media.
	_, err = sqlDB.ExecContext(ctx, `DELETE FROM packs WHERE id='p1'`)
	require.NoError(t, err)
	var n int
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pack_media`).Scan(&n))
	require.Equal(t, 0, n)
}

func TestTx_CommitRollbackPanic(t *testing.T) {
	sqlDB := openMigrated(t)
	ctx := context.Background()

	count := func() int {
		var n int
		require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings`).Scan(&n))
		return n
	}

	require.NoError(t, db.Tx(ctx, sqlDB, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('a','1')`)
		return err
	}))
	require.Equal(t, 1, count())

	sentinel := errors.New("boom")
	err := db.Tx(ctx, sqlDB, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('b','2')`); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, count(), "rolled back on error")

	require.PanicsWithValue(t, "panic inside tx", func() {
		_ = db.Tx(ctx, sqlDB, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('c','3')`); err != nil {
				return err
			}
			panic("panic inside tx")
		})
	})
	require.Equal(t, 1, count(), "rolled back on panic")

	// The connection is usable after the rollback/panic.
	require.NoError(t, db.Tx(ctx, sqlDB, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('d','4')`)
		return err
	}))
	require.Equal(t, 2, count())
}
