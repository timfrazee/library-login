package main

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

// openDB opens (creating if absent) the SQLite database, enables WAL so the
// kiosk and an admin reading reports don't block each other, and migrates.
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS people (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  category   TEXT,
  active     INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  id          INTEGER PRIMARY KEY,
  person_id   TEXT NOT NULL,
  direction   TEXT NOT NULL CHECK (direction IN ('in','out')),
  at          TEXT NOT NULL,
  period_id   INTEGER,
  period_name TEXT,
  auto        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS events_person_idx ON events (person_id, id);
CREATE INDEX IF NOT EXISTS events_at_idx ON events (at);

CREATE TABLE IF NOT EXISTS periods (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  weekdays   TEXT NOT NULL,
  start_time TEXT NOT NULL,
  end_time   TEXT NOT NULL,
  active     INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`)
	return err
}

func getConfig(db *sql.DB, key string) (string, bool, error) {
	var v string
	err := db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func setConfig(db *sql.DB, key, value string) error {
	_, err := db.Exec(
		`INSERT INTO config (key, value) VALUES (?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func configOr(db *sql.DB, key, def string) string {
	v, ok, err := getConfig(db, key)
	if err != nil || !ok || v == "" {
		return def
	}
	return v
}

// loadLocation returns the admin-configured timezone. It defaults to UTC rather
// than the host clock because Crostini containers commonly default to UTC, which
// would silently skew every logged timestamp.
func loadLocation(db *sql.DB) *time.Location {
	loc, err := time.LoadLocation(configOr(db, cfgTimezone, "UTC"))
	if err != nil {
		return time.UTC
	}
	return loc
}
