package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// Database wraps a SQLite connection with write serialization.
type Database struct {
	db *sql.DB
	mu sync.Mutex // serializes writes (mirrors Rust's Mutex<Connection>)
}

// Open creates or opens a SQLite database at the given path.
func Open(path string) (*Database, error) {
	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Single write connection + WAL for concurrent reads.
	db.SetMaxOpenConns(1)

	d := &Database{db: db}

	if err := d.runMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return d, nil
}

// Close closes the database connection.
func (d *Database) Close() error {
	return d.db.Close()
}

// withLock serializes write operations.
func (d *Database) withLock(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fn()
}

// queryRow is a convenience for single-row queries.
func (d *Database) queryRow(query string, args ...any) *sql.Row {
	return d.db.QueryRow(query, args...)
}

// query is a convenience for multi-row queries.
func (d *Database) query(query string, args ...any) (*sql.Rows, error) {
	return d.db.Query(query, args...)
}

// exec executes a write statement under the write lock.
func (d *Database) exec(query string, args ...any) (sql.Result, error) {
	var result sql.Result
	err := d.withLock(func() error {
		var e error
		result, e = d.db.Exec(query, args...)
		return e
	})
	return result, err
}

// execTx runs multiple statements in a transaction under the write lock.
func (d *Database) execTx(fn func(tx *sql.Tx) error) error {
	return d.withLock(func() error {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	})
}
