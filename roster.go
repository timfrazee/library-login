package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const rosterPageSize = 50

type rosterRow struct {
	ID       string
	Name     string
	Category string
	Active   bool
}

// likePattern wraps a search term for a case-insensitive substring LIKE, escaping
// the LIKE metacharacters so a literal % or _ in the term matches itself.
func likePattern(q string) string {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	return "%" + esc + "%"
}

// peopleWhere builds the shared roster filter: active-only unless inactive are
// included, plus an optional term matching id, name, or category.
func peopleWhere(q string, includeInactive bool) (string, []any) {
	var conds []string
	var args []any
	if !includeInactive {
		conds = append(conds, "active = 1")
	}
	if q != "" {
		p := likePattern(q)
		conds = append(conds, `(id LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR category LIKE ? ESCAPE '\')`)
		args = append(args, p, p, p)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func (a *app) countPeople(q string, includeInactive bool) (int, error) {
	where, args := peopleWhere(q, includeInactive)
	var n int
	err := a.db.QueryRow(`SELECT COUNT(*) FROM people `+where, args...).Scan(&n)
	return n, err
}

// listPeoplePage returns one page of people. Inactive people sort after active
// ones when included.
func (a *app) listPeoplePage(q string, includeInactive bool, limit, offset int) ([]rosterRow, error) {
	where, args := peopleWhere(q, includeInactive)
	args = append(args, limit, offset)
	rows, err := a.db.Query(
		`SELECT id, name, COALESCE(category,''), active FROM people `+where+
			` ORDER BY active DESC, name COLLATE NOCASE LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rosterRow
	for rows.Next() {
		var pr rosterRow
		var active int
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Category, &active); err != nil {
			return nil, err
		}
		pr.Active = active != 0
		out = append(out, pr)
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

// updatePerson edits name and category for an existing person, leaving their
// active state untouched (unlike upsertPerson, which reactivates).
func (a *app) updatePerson(id, name, category string) error {
	_, err := a.db.Exec(`UPDATE people SET name=?, category=? WHERE id=? COLLATE NOCASE`, name, category, id)
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

type rosterData struct {
	People          []rosterRow
	Query           string
	IncludeInactive bool
	Total           int
	Page            int
	PageCount       int
	HasPrev         bool
	HasNext         bool
	PrevPage        int
	NextPage        int
}

func (a *app) handleRoster(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	includeInactive := r.URL.Query().Get("include_inactive") != ""
	total, err := a.countPeople(q, includeInactive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pageCount := (total + rosterPageSize - 1) / rosterPageSize
	if pageCount < 1 {
		pageCount = 1
	}
	page := parsePage(r.URL.Query().Get("page"))
	if page > pageCount {
		page = pageCount
	}
	people, err := a.listPeoplePage(q, includeInactive, rosterPageSize, (page-1)*rosterPageSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v := a.adminBase(r, "roster")
	v.Data = rosterData{
		People:          people,
		Query:           q,
		IncludeInactive: includeInactive,
		Total:           total,
		Page:            page,
		PageCount:       pageCount,
		HasPrev:         page > 1,
		HasNext:         page < pageCount,
		PrevPage:        page - 1,
		NextPage:        page + 1,
	}

	a.render(w, "roster.html", v)
}

func parsePage(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 1
	}
	return n
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

// handleRosterUpdate saves inline edits to one person's name and category,
// responding with JSON rather than a redirect.
func (a *app) handleRosterUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	category := strings.TrimSpace(r.FormValue("category"))
	if id == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "ID and name are required")
		return
	}
	if err := a.updatePerson(id, name, category); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not save")
		return
	}
	writeJSON(w, map[string]string{"name": name, "category": category})
}

func (a *app) handleRosterDeactivate(w http.ResponseWriter, r *http.Request) {
	a.setActiveJSON(w, r, false)
}

func (a *app) handleRosterReactivate(w http.ResponseWriter, r *http.Request) {
	a.setActiveJSON(w, r, true)
}

func (a *app) setActiveJSON(w http.ResponseWriter, r *http.Request, active bool) {
	if !a.checkCSRF(r) {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := a.setActive(r.FormValue("id"), active); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not update")
		return
	}
	writeJSON(w, map[string]bool{"active": active})
}

func (a *app) handleRosterPurge(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := a.deletePersonPermanent(r.FormValue("id")); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
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
