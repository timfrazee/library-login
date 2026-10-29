package main

import (
	"database/sql"
	"errors"
)

type person struct {
	ID       string
	Name     string
	Category string
	Active   bool
}

// findPerson matches case-insensitively so scanner/keyboard case differences
// don't produce false "not recognized" results. Returns nil when no row matches.
func findPerson(db *sql.DB, id string) (*person, error) {
	var p person
	var active int
	var category sql.NullString
	err := db.QueryRow(
		`SELECT id, name, category, active FROM people WHERE id = ? COLLATE NOCASE LIMIT 1`,
		id).Scan(&p.ID, &p.Name, &category, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Category = category.String
	p.Active = active != 0
	return &p, nil
}
