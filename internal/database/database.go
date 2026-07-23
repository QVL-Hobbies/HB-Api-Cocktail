package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS cocktails (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	name         TEXT    NOT NULL,
	instructions TEXT    NOT NULL,
	glass        TEXT    NOT NULL,
	category     TEXT    NOT NULL,
	strength     TEXT    NOT NULL,
	alcoholic    INTEGER NOT NULL,
	season       TEXT    NOT NULL DEFAULT '',
	image_name   TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ingredients (
	id   INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT    NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS cocktail_ingredients (
	cocktail_id   INTEGER NOT NULL REFERENCES cocktails(id)   ON DELETE CASCADE,
	ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
	quantity      TEXT    NOT NULL DEFAULT '',
	unit          TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (cocktail_id, ingredient_id)
);

CREATE TABLE IF NOT EXISTS cocktail_tags (
	cocktail_id INTEGER NOT NULL REFERENCES cocktails(id) ON DELETE CASCADE,
	tag         TEXT    NOT NULL,
	PRIMARY KEY (cocktail_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_cocktails_category ON cocktails(category);
CREATE INDEX IF NOT EXISTS idx_cocktail_tags_tag  ON cocktail_tags(tag);
`

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("reach database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return db, nil
}
