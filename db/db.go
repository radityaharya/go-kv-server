package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kv-server/models"

	"github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
	statements map[string]*sql.Stmt
	mu         sync.RWMutex
}

type DBError int

const (
	ErrLocked DBError = iota
)

func InitDB(path string) (*DB, error) {
	// Create parent directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetMaxIdleConns(40)
	sqlDB.SetConnMaxLifetime(10 * time.Minute)

	optimizations := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=OFF",
		"PRAGMA busy_timeout=30000",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA mmap_size=3000000000",
		"PRAGMA page_size=32768",
		"PRAGMA cache_size=8000000",
		"PRAGMA journal_size_limit=134217728",
		"PRAGMA wal_autocheckpoint=2000",
		"PRAGMA locking_mode=NORMAL",
		"PRAGMA threads=4",
		"PRAGMA secure_delete=OFF",
	}

	for _, opt := range optimizations {
		if _, err := sqlDB.Exec(opt); err != nil {
			return nil, err
		}
	}

	db := &DB{
		DB:         sqlDB,
		statements: make(map[string]*sql.Stmt),
	}

	// Create tables first
	if err := db.CreateTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Prepare common statements after tables exist
	statements := map[string]string{
		"setValue":    "INSERT OR REPLACE INTO key_values(namespace, key, value) VALUES(?, ?, ?)",
		"getValue":    "SELECT value FROM key_values WHERE namespace = ? AND key = ?",
		"deleteValue": "DELETE FROM key_values WHERE namespace = ? AND key = ?",
	}

	for name, query := range statements {
		stmt, err := db.Prepare(query)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare statement %s: %w", name, err)
		}
		db.statements[name] = stmt
	}

	db.startBackgroundCheckpoint(10 * time.Second)

	return db, nil
}

func (db *DB) retryExec(stmt *sql.Stmt, args ...any) error {
	var err error
	for i := 0; i < 3; i++ {
		_, err = stmt.Exec(args...)
		if err != nil && IsLockError(err) {
			time.Sleep(time.Duration(i*50) * time.Millisecond)
			continue
		}
		break
	}
	return err
}

func (db *DB) retryQueryRow(stmt *sql.Stmt, namespace, key string) (string, error) {
	var value string
	var err error
	for i := 0; i < 3; i++ {
		err = stmt.QueryRow(namespace, key).Scan(&value)
		if err != nil && IsLockError(err) {
			time.Sleep(time.Duration(i*50) * time.Millisecond)
			continue
		}
		break
	}
	return value, err
}

func (db *DB) SetValue(namespace, key, value string) error {
	db.mu.RLock()
	stmt := db.statements["setValue"]
	db.mu.RUnlock()
	return db.retryExec(stmt, namespace, key, value)
}

func (db *DB) BatchSetValues(namespace string, kvPairs map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Disable synchronous writes during batch operation
	if _, err := tx.Exec("PRAGMA synchronous=OFF"); err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT OR REPLACE INTO key_values(namespace, key, value) VALUES(?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	batchSize := 5000 // Increased batch size
	batch := make([]interface{}, 0, batchSize*3)

	for k, v := range kvPairs {
		batch = append(batch, namespace, k, v)
		if len(batch) >= batchSize*3 {
			if _, err := stmt.Exec(batch...); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		if _, err := stmt.Exec(batch...); err != nil {
			return err
		}
	}

	// Re-enable synchronous writes before commit
	if _, err := tx.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) CreateTables() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS namespaces (
			name TEXT PRIMARY KEY
		)`,
		`CREATE TABLE IF NOT EXISTS key_values (
			namespace TEXT,
			key TEXT,
			value TEXT,
			PRIMARY KEY (namespace, key),
			FOREIGN KEY (namespace) REFERENCES namespaces(name)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_namespace_key ON key_values(namespace, key)`,
		`CREATE INDEX IF NOT EXISTS idx_namespace ON key_values(namespace)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) CreateNamespace(name string) error {
	_, err := db.Exec("INSERT INTO namespaces(name) VALUES(?)", name)
	return err
}

func (db *DB) ListNamespaces() ([]string, error) {
	rows, err := db.Query("SELECT name FROM namespaces")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var namespaces []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		namespaces = append(namespaces, name)
	}
	return namespaces, nil
}

func (db *DB) GetValue(namespace, key string) (string, error) {
	db.mu.RLock()
	stmt := db.statements["getValue"]
	db.mu.RUnlock()
	return db.retryQueryRow(stmt, namespace, key)
}

// IsLockError checks if the error is a database lock error
func IsLockError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, sqlite3.ErrLocked) ||
		errors.Is(err, sqlite3.ErrBusy) ||
		strings.Contains(err.Error(), "database is locked") ||
		strings.Contains(err.Error(), "busy")
}

func (db *DB) GetAllValues(namespace string) (map[string]string, error) {
	rows, err := db.Query(
		"SELECT key, value FROM key_values WHERE namespace = ?",
		namespace,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func (db *DB) DeleteValue(namespace, key string) error {
	db.mu.RLock()
	stmt := db.statements["deleteValue"]
	db.mu.RUnlock()

	_, err := stmt.Exec(namespace, key)
	return err
}

func (db *DB) DeleteNamespace(name string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// First delete all keys in the namespace
	_, err = tx.Exec("DELETE FROM key_values WHERE namespace = ?", name)
	if err != nil {
		return err
	}

	// Then delete the namespace itself
	_, err = tx.Exec("DELETE FROM namespaces WHERE name = ?", name)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) NamespaceExists(name string) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM namespaces WHERE name = ?)", name).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (db *DB) CountNamespaces() (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM namespaces").Scan(&count)
	return count, err
}

func (db *DB) CountKeyValues() (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM key_values").Scan(&count)
	return count, err
}

func (db *DB) GetAllValuesPaginated(namespace string, limit, offset int) ([]models.KeyValue, error) {
	if limit <= 0 {
		limit = 10 // default limit
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := db.Query(
		"SELECT key, value FROM key_values WHERE namespace = ? ORDER BY key LIMIT ? OFFSET ?",
		namespace, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var kvs []models.KeyValue
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		kvs = append(kvs, models.KeyValue{Key: k, Value: v})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return kvs, nil
}

func (db *DB) CountValuesInNamespace(namespace string) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM key_values WHERE namespace = ?", namespace).Scan(&count)
	return count, err
}

func (db *DB) BatchDeleteValues(namespace string, keys []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("DELETE FROM key_values WHERE namespace = ? AND key = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, k := range keys {
		if _, err := stmt.Exec(namespace, k); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type WriteBatch struct {
	data      map[string]map[string]string
	mu        sync.Mutex
	db        *DB
	batchSize int
}

func NewWriteBatch(db *DB, batchSize int) *WriteBatch {
	if batchSize <= 0 {
		batchSize = 5000 // Increased default batch size
	}
	wb := &WriteBatch{
		data:      make(map[string]map[string]string),
		db:        db,
		batchSize: batchSize,
	}
	return wb
}

func (wb *WriteBatch) Add(namespace, key, value string) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	if wb.data[namespace] == nil {
		wb.data[namespace] = make(map[string]string)
	}
	wb.data[namespace][key] = value

	// Flush if batch size reached
	if len(wb.data[namespace]) >= wb.batchSize {
		wb.Flush(namespace)
	}
}

func (wb *WriteBatch) Flush(namespace string) error {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	if len(wb.data[namespace]) == 0 {
		return nil
	}

	err := wb.db.BatchSetValues(namespace, wb.data[namespace])
	if err == nil {
		delete(wb.data, namespace)
	}
	return err
}

func (db *DB) startBackgroundCheckpoint(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			_, err := db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
			if err != nil && !IsLockError(err) {
				fmt.Printf("Background checkpoint error: %v\n", err)
			}
		}
	}()
}
