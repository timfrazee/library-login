package main

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// resolvePeriod returns the active period whose weekday set and HH:MM window
// contain now (in loc); a null result means the scan fell outside any period.
// weekdays are stored as a comma-separated list of Go weekday numbers (0=Sunday).
func resolvePeriod(db *sql.DB, now time.Time, loc *time.Location) (sql.NullInt64, sql.NullString, error) {
	local := now.In(loc)
	weekday := int(local.Weekday())
	clock := local.Format("15:04")

	rows, err := db.Query(`SELECT id, name, weekdays, start_time, end_time FROM periods WHERE active = 1`)
	if err != nil {
		return sql.NullInt64{}, sql.NullString{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, weekdays, start, end string
		if err := rows.Scan(&id, &name, &weekdays, &start, &end); err != nil {
			return sql.NullInt64{}, sql.NullString{}, err
		}
		if weekdaysContain(weekdays, weekday) && start <= clock && clock < end {
			return sql.NullInt64{Int64: id, Valid: true}, sql.NullString{String: name, Valid: true}, nil
		}
	}
	return sql.NullInt64{}, sql.NullString{}, rows.Err()
}

func weekdaysContain(csv string, weekday int) bool {
	want := strconv.Itoa(weekday)
	for _, d := range strings.Split(csv, ",") {
		if strings.TrimSpace(d) == want {
			return true
		}
	}
	return false
}
