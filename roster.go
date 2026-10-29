package main

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type rosterRow struct {
	ID       string
	Name     string
	Category string
	Active   bool
}

func (a *app) listPeople() ([]rosterRow, error) {
	rows, err := a.db.Query(
		`SELECT id, name, COALESCE(category,''), active FROM people ORDER BY active DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rosterRow
	for rows.Next() {
		var p rosterRow
		var active int
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &active); err != nil {
			return nil, err
		}
		p.Active = active != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// upsertPerson inserts or updates by case-insensitive id, reporting whether a
// row already existed. An update also reactivates, so re-uploading a roster
// restores anyone previously deactivated.
func (a *app) upsertPerson(id, name, category string) (existed bool, err error) {
	var found string
	err = a.db.QueryRow(`SELECT id FROM people WHERE id = ? COLLATE NOCASE`, id).Scan(&found)
	switch {
	case err == nil:
		_, err = a.db.Exec(
			`UPDATE people SET name=?, category=?, active=1 WHERE id=? COLLATE NOCASE`, name, category, id)
		return true, err
	case errors.Is(err, sql.ErrNoRows):
		_, err = a.db.Exec(
			`INSERT INTO people(id,name,category,active,created_at) VALUES(?,?,?,1,?)`,
			id, name, category, time.Now().UTC().Format(time.RFC3339))
		return false, err
	default:
		return false, err
	}
}

func (a *app) setActive(id string, active bool) error {
	_, err := a.db.Exec(`UPDATE people SET active=? WHERE id=?`, boolToInt(active), id)
	return err
}

// deletePersonPermanent erases a person and their event history in one
// transaction — the right-to-be-forgotten path, distinct from deactivation.
func (a *app) deletePersonPermanent(id string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM events WHERE person_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM people WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type importSummary struct {
	Added       int
	Updated     int
	Skipped     int
	Deactivated int
	Errors      []string
}

// importCSV upserts rows of "id,name,category" (an optional header row is
// detected and skipped). Malformed rows are reported and skipped rather than
// aborting the whole import.
func (a *app) importCSV(r io.Reader, deactivateMissing bool) (importSummary, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	var sum importSummary
	seen := map[string]bool{}
	line := 0
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return sum, err
		}
		line++
		if line == 1 && len(rec) > 0 && strings.EqualFold(strings.TrimSpace(rec[0]), "id") {
			continue
		}
		id, name, category := field(rec, 0), field(rec, 1), field(rec, 2)
		if id == "" || name == "" {
			sum.Skipped++
			sum.Errors = append(sum.Errors, fmt.Sprintf("line %d: missing id or name", line))
			continue
		}
		existed, err := a.upsertPerson(id, name, category)
		if err != nil {
			return sum, err
		}
		if existed {
			sum.Updated++
		} else {
			sum.Added++
		}
		seen[strings.ToLower(id)] = true
	}
	if deactivateMissing {
		n, err := a.deactivateNotIn(seen)
		if err != nil {
			return sum, err
		}
		sum.Deactivated = n
	}
	return sum, nil
}

func (a *app) deactivateNotIn(seen map[string]bool) (int, error) {
	rows, err := a.db.Query(`SELECT id FROM people WHERE active = 1`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		if !seen[strings.ToLower(id)] {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range stale {
		if err := a.setActive(id, false); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}

func (a *app) handleRoster(w http.ResponseWriter, r *http.Request) {
	people, err := a.listPeople()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v := a.adminBase(r, "roster")
	v.Data = struct{ People []rosterRow }{people}
	a.render(w, "roster.html", v)
}

func (a *app) handleRosterSave(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if id == "" || name == "" {
		http.Redirect(w, r, "/admin/roster?err=ID+and+name+are+required", http.StatusSeeOther)
		return
	}
	if _, err := a.upsertPerson(id, name, strings.TrimSpace(r.FormValue("category"))); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/roster?flash=Saved+"+id, http.StatusSeeOther)
}

func (a *app) handleRosterDelete(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := a.setActive(r.FormValue("id"), false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/roster?flash=Deactivated", http.StatusSeeOther)
}

func (a *app) handleRosterPurge(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := a.deletePersonPermanent(r.FormValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/roster?flash=Permanently+deleted", http.StatusSeeOther)
}

func (a *app) handleRosterUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "upload too large", http.StatusBadRequest)
		return
	}
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, "/admin/roster?err=No+file+selected", http.StatusSeeOther)
		return
	}
	defer file.Close()
	sum, err := a.importCSV(file, r.FormValue("deactivate_missing") == "on")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	msg := fmt.Sprintf("Added %d, updated %d, skipped %d, deactivated %d",
		sum.Added, sum.Updated, sum.Skipped, sum.Deactivated)
	http.Redirect(w, r, "/admin/roster?flash="+url.QueryEscape(msg), http.StatusSeeOther)
}

func field(rec []string, i int) string {
	if i < len(rec) {
		return strings.TrimSpace(rec[i])
	}
	return ""
}
