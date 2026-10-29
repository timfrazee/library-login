package main

import (
	"encoding/csv"
	"net/http"
	"sort"
	"strings"
	"time"
)

type occupant struct {
	Name     string
	Category string
	Since    string
}

// dayBoundsUTC returns the UTC [start, end) RFC3339 strings bracketing the local
// calendar day containing ref. Events are stored in UTC, so range filters must
// translate local day boundaries into UTC.
func dayBoundsUTC(loc *time.Location, ref time.Time) (string, string) {
	y, m, d := ref.In(loc).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, loc)
	return start.UTC().Format(time.RFC3339), start.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
}

func (a *app) currentlyIn(loc *time.Location) ([]occupant, error) {
	rows, err := a.db.Query(`
SELECT p.name, COALESCE(p.category,''), e.at FROM events e
JOIN (SELECT person_id, MAX(id) AS mid FROM events GROUP BY person_id) last ON e.id = last.mid
JOIN people p ON p.id = e.person_id
WHERE e.direction = 'in'
ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []occupant
	for rows.Next() {
		var o occupant
		var atStr string
		if err := rows.Scan(&o.Name, &o.Category, &atStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, atStr); err == nil {
			o.Since = t.In(loc).Format("Mon 3:04 PM")
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (a *app) countEventsBetween(startUTC, endUTC string) (int, error) {
	var n int
	err := a.db.QueryRow(`SELECT COUNT(*) FROM events WHERE at >= ? AND at < ?`, startUTC, endUTC).Scan(&n)
	return n, err
}

type reportFilter struct {
	From     string // YYYY-MM-DD local, inclusive
	To       string // YYYY-MM-DD local, inclusive
	PersonID string
	Category string
	Period   string
}

func parseReportFilter(r *http.Request) reportFilter {
	return reportFilter{
		From:     strings.TrimSpace(r.URL.Query().Get("from")),
		To:       strings.TrimSpace(r.URL.Query().Get("to")),
		PersonID: strings.TrimSpace(r.URL.Query().Get("person")),
		Category: strings.TrimSpace(r.URL.Query().Get("category")),
		Period:   strings.TrimSpace(r.URL.Query().Get("period")),
	}
}

func (f reportFilter) where(loc *time.Location) (string, []any) {
	var clauses []string
	var args []any
	if d, err := time.ParseInLocation("2006-01-02", f.From, loc); err == nil {
		clauses = append(clauses, "e.at >= ?")
		args = append(args, d.UTC().Format(time.RFC3339))
	}
	if d, err := time.ParseInLocation("2006-01-02", f.To, loc); err == nil {
		clauses = append(clauses, "e.at < ?")
		args = append(args, d.AddDate(0, 0, 1).UTC().Format(time.RFC3339))
	}
	if f.PersonID != "" {
		clauses = append(clauses, "e.person_id = ? COLLATE NOCASE")
		args = append(args, f.PersonID)
	}
	if f.Category != "" {
		clauses = append(clauses, "p.category = ?")
		args = append(args, f.Category)
	}
	if f.Period != "" {
		clauses = append(clauses, "COALESCE(e.period_name,'') = ?")
		args = append(args, f.Period)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

type eventRow struct {
	At        string
	Direction string
	PersonID  string
	Name      string
	Category  string
	Period    string
	atRaw     time.Time
}

func (a *app) queryEvents(f reportFilter, loc *time.Location, limit int) ([]eventRow, error) {
	where, args := f.where(loc)
	q := `SELECT e.at, e.direction, e.person_id, COALESCE(p.name,'(deleted)'),
	             COALESCE(p.category,''), COALESCE(e.period_name,'')
	      FROM events e LEFT JOIN people p ON p.id = e.person_id` + where + ` ORDER BY e.id DESC`
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := a.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		var atStr string
		if err := rows.Scan(&atStr, &e.Direction, &e.PersonID, &e.Name, &e.Category, &e.Period); err != nil {
			return nil, err
		}
		e.atRaw, _ = time.Parse(time.RFC3339, atStr)
		e.At = e.atRaw.In(loc).Format("2006-01-02 3:04 PM")
		out = append(out, e)
	}
	return out, rows.Err()
}

type countKV struct {
	Key   string
	Count int
}

type reportSummary struct {
	TotalEvents  int
	UniquePeople int
	ByDay        []countKV
	ByPeriod     []countKV
	ByCategory   []countKV
}

func (a *app) summarize(f reportFilter, loc *time.Location) (reportSummary, error) {
	rows, err := a.queryEvents(f, loc, 0)
	if err != nil {
		return reportSummary{}, err
	}
	day, period, category := map[string]int{}, map[string]int{}, map[string]int{}
	people := map[string]bool{}
	for _, e := range rows {
		day[e.atRaw.In(loc).Format("2006-01-02")]++
		period[orDash(e.Period)]++
		category[orDash(e.Category)]++
		people[strings.ToLower(e.PersonID)] = true
	}
	return reportSummary{
		TotalEvents:  len(rows),
		UniquePeople: len(people),
		ByDay:        sortedKV(day),
		ByPeriod:     sortedKV(period),
		ByCategory:   sortedKV(category),
	}, nil
}

func (a *app) handleReports(w http.ResponseWriter, r *http.Request) {
	loc := a.location()
	f := parseReportFilter(r)
	events, err := a.queryEvents(f, loc, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	summary, err := a.summarize(f, loc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v := a.adminBase(r, "reports")
	v.Data = struct {
		Filter  reportFilter
		Events  []eventRow
		Summary reportSummary
		Capped  bool
		Query   string
	}{f, events, summary, len(events) == 500, r.URL.RawQuery}
	a.render(w, "reports.html", v)
}

func (a *app) handleReportsExport(w http.ResponseWriter, r *http.Request) {
	loc := a.location()
	events, err := a.queryEvents(parseReportFilter(r), loc, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="library-login-events.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "direction", "id", "name", "category", "period"})
	for _, e := range events {
		_ = cw.Write([]string{e.At, e.Direction, e.PersonID, e.Name, e.Category, e.Period})
	}
	cw.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func sortedKV(m map[string]int) []countKV {
	out := make([]countKV, 0, len(m))
	for k, v := range m {
		out = append(out, countKV{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
