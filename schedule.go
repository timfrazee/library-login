package main

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var weekdayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

type periodRow struct {
	ID         int64
	Name       string
	DayLabel   string
	Start      string
	End        string
	Selected   map[int]bool
	weekdays   []int
	startClock string
	endClock   string
}

func (a *app) listPeriods() ([]periodRow, error) {
	rows, err := a.db.Query(
		`SELECT id, name, weekdays, start_time, end_time FROM periods WHERE active = 1 ORDER BY start_time, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []periodRow
	for rows.Next() {
		var p periodRow
		var wd string
		if err := rows.Scan(&p.ID, &p.Name, &wd, &p.startClock, &p.endClock); err != nil {
			return nil, err
		}
		p.weekdays = parseWeekdays(wd)
		p.DayLabel = weekdayLabel(p.weekdays)
		p.Selected = intSet(p.weekdays)
		p.Start, p.End = p.startClock, p.endClock
		out = append(out, p)
	}
	return out, rows.Err()
}

// validatePeriod checks a period's fields and rejects any overlap on a shared
// weekday with an existing period so resolvePeriod never faces ambiguity. The
// period whose id is excludeID is skipped, letting an edit keep its own slot.
func (a *app) validatePeriod(name string, weekdays []int, start, end string, excludeID int64) error {
	if name == "" {
		return errors.New("Name is required")
	}
	if len(weekdays) == 0 {
		return errors.New("Select at least one weekday")
	}
	if !validClock(start) || !validClock(end) || start >= end {
		return errors.New("Start must be a valid time before end")
	}
	existing, err := a.listPeriods()
	if err != nil {
		return err
	}
	wset := intSet(weekdays)
	for _, ex := range existing {
		if ex.ID == excludeID {
			continue
		}
		if sharesWeekday(wset, ex.weekdays) && start < ex.endClock && ex.startClock < end {
			return errors.New("Overlaps \"" + ex.Name + "\"")
		}
	}
	return nil
}

func (a *app) addPeriod(name string, weekdays []int, start, end string) error {
	if err := a.validatePeriod(name, weekdays, start, end, 0); err != nil {
		return err
	}
	_, err := a.db.Exec(
		`INSERT INTO periods(name, weekdays, start_time, end_time, active) VALUES(?,?,?,?,1)`,
		name, joinWeekdays(weekdays), start, end)
	return err
}

// updatePeriod validates and saves edits to an existing period, excluding that
// period from the overlap check so an unchanged slot is not flagged.
func (a *app) updatePeriod(id int64, name string, weekdays []int, start, end string) error {
	if err := a.validatePeriod(name, weekdays, start, end, id); err != nil {
		return err
	}
	_, err := a.db.Exec(
		`UPDATE periods SET name=?, weekdays=?, start_time=?, end_time=? WHERE id=?`,
		name, joinWeekdays(weekdays), start, end, id)
	return err
}

func (a *app) handleSchedule(w http.ResponseWriter, r *http.Request) {
	periods, err := a.listPeriods()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v := a.adminBase(r, "schedule")
	v.Data = struct {
		Periods []periodRow
		Days    []dayOption
	}{periods, dayOptions()}
	a.render(w, "schedule.html", v)
}

func (a *app) handleScheduleAdd(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	err := a.addPeriod(strings.TrimSpace(r.FormValue("name")), formWeekdays(r),
		r.FormValue("start"), r.FormValue("end"))
	if err != nil {
		http.Redirect(w, r, "/admin/schedule?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/schedule?flash=Period+added", http.StatusSeeOther)
}

// handleScheduleUpdate saves inline edits to one period, responding with the
// recomputed display fields as JSON rather than a redirect.
func (a *app) handleScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid period")
		return
	}
	weekdays := formWeekdays(r)
	name := strings.TrimSpace(r.FormValue("name"))
	start, end := r.FormValue("start"), r.FormValue("end")
	if err := a.updatePeriod(id, name, weekdays, start, end); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]string{
		"name":  name,
		"days":  weekdayLabel(weekdays),
		"start": start,
		"end":   end,
	})
}

func formWeekdays(r *http.Request) []int {
	var weekdays []int
	for _, s := range r.Form["weekdays"] {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 6 {
			weekdays = append(weekdays, n)
		}
	}
	return weekdays
}

func (a *app) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	if !a.checkCSRF(r) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if _, err := a.db.Exec(`DELETE FROM periods WHERE id = ?`, r.FormValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/schedule?flash=Period+removed", http.StatusSeeOther)
}

type dayOption struct {
	Value int
	Label string
}

func dayOptions() []dayOption {
	out := make([]dayOption, 7)
	for i, n := range weekdayNames {
		out[i] = dayOption{i, n}
	}
	return out
}

func parseWeekdays(csv string) []int {
	var out []int
	for _, s := range strings.Split(csv, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func joinWeekdays(w []int) string {
	parts := make([]string, len(w))
	for i, n := range w {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func weekdayLabel(w []int) string {
	parts := make([]string, 0, len(w))
	for _, n := range w {
		if n >= 0 && n <= 6 {
			parts = append(parts, weekdayNames[n])
		}
	}
	return strings.Join(parts, ", ")
}

func intSet(w []int) map[int]bool {
	m := make(map[int]bool, len(w))
	for _, n := range w {
		m[n] = true
	}
	return m
}

func sharesWeekday(set map[int]bool, days []int) bool {
	for _, d := range days {
		if set[d] {
			return true
		}
	}
	return false
}

func validClock(s string) bool {
	_, err := time.Parse("15:04", s)
	return err == nil
}
