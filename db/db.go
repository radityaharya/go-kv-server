package db

import (
	"database/sql"
	"os"

	"path/filepath"

	"kv-server/models"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func InitDB(path string) (*DB, error) {
	// Create parent directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &DB{db}, nil
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

func (db *DB) SetValue(namespace, key, value string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if namespace exists
	exists, err := db.NamespaceExists(namespace)
	if err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}

	_, err = tx.Exec(
		"INSERT OR REPLACE INTO key_values(namespace, key, value) VALUES(?, ?, ?)",
		namespace, key, value,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) GetValue(namespace, key string) (string, error) {
	var value string
	err := db.QueryRow(
		"SELECT value FROM key_values WHERE namespace = ? AND key = ?",
		namespace, key,
	).Scan(&value)
	return value, err
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
	_, err := db.Exec(
		"DELETE FROM key_values WHERE namespace = ? AND key = ?",
		namespace, key,
	)
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

// New helper method to count key-values in a namespace.
func (db *DB) CountValuesInNamespace(namespace string) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM key_values WHERE namespace = ?", namespace).Scan(&count)
	return count, err
}
