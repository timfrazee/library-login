package main

import (
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"
)

const (
	statusIn        = "in"
	statusOut       = "out"
	statusDebounced = "debounced"
	statusUnknown   = "unknown"
	statusInactive  = "inactive"
)

type scanResult struct {
	Status    string
	Name      string
	Direction string
	At        time.Time
}

// recordScan resolves an ID to a person, toggles their in/out state against
// their last event, and records the event stamped with the current period.
func (a *app) recordScan(rawID string, now time.Time) (scanResult, error) {
	id := strings.TrimSpace(rawID)
	if id == "" {
		return scanResult{Status: statusUnknown}, nil
	}
	p, err := findPerson(a.db, id)
	if err != nil {
		return scanResult{}, err
	}
	if p == nil {
		return scanResult{Status: statusUnknown}, nil
	}
	if !p.Active {
		return scanResult{Status: statusInactive, Name: p.Name}, nil
	}

	lastAt, lastDir, hasLast, err := lastEvent(a.db, p.ID)
	if err != nil {
		return scanResult{}, err
	}
	if hasLast && now.Sub(lastAt) < a.debounce() {
		return scanResult{Status: statusDebounced, Name: p.Name, Direction: lastDir, At: lastAt}, nil
	}

	dir := statusIn
	if hasLast && lastDir == statusIn {
		dir = statusOut
	}

	pid, pname, err := resolvePeriod(a.db, now, a.location())
	if err != nil {
		return scanResult{}, err
	}
	if err := insertEvent(a.db, p.ID, dir, now.UTC(), pid, pname, false); err != nil {
		return scanResult{}, err
	}
	return scanResult{Status: dir, Name: p.Name, Direction: dir, At: now}, nil
}

func lastEvent(db *sql.DB, personID string) (at time.Time, direction string, ok bool, err error) {
	var atStr string
	err = db.QueryRow(
		`SELECT at, direction FROM events WHERE person_id = ? ORDER BY id DESC LIMIT 1`,
		personID).Scan(&atStr, &direction)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, "", false, nil
	}
	if err != nil {
		return time.Time{}, "", false, err
	}
	at, err = time.Parse(time.RFC3339, atStr)
	return at, direction, true, err
}

func insertEvent(db *sql.DB, personID, direction string, atUTC time.Time, periodID sql.NullInt64, periodName sql.NullString, auto bool) error {
	_, err := db.Exec(
		`INSERT INTO events (person_id, direction, at, period_id, period_name, auto)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		personID, direction, atUTC.Format(time.RFC3339), periodID, periodName, boolToInt(auto))
	return err
}

// autoCheckoutLoop logs out anyone still "in" at the configured daily time,
// keeping historical occupancy honest without manual cleanup. It fires at most
// once per calendar day in the configured timezone.
func (a *app) autoCheckoutLoop() {
	var lastRun string
	for range time.Tick(time.Minute) {
		if configOr(a.db, cfgAutoCheckoutEnabled, "0") != "1" {
			continue
		}
		now := time.Now().In(a.location())
		today := now.Format("2006-01-02")
		if lastRun == today || now.Format("15:04") != configOr(a.db, cfgAutoCheckoutTime, "") {
			continue
		}
		if err := a.autoCheckout(now.UTC()); err != nil {
			log.Printf("auto-checkout: %v", err)
			continue
		}
		lastRun = today
	}
}

func (a *app) autoCheckout(nowUTC time.Time) error {
	rows, err := a.db.Query(`
SELECT e.person_id FROM events e
JOIN (SELECT person_id, MAX(id) AS mid FROM events GROUP BY person_id) last
  ON e.id = last.mid
WHERE e.direction = 'in'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := insertEvent(a.db, id, statusOut, nowUTC, sql.NullInt64{}, sql.NullString{}, true); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
