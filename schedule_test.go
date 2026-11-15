package main

import (
	"strings"
	"testing"
)

// periodID adds a period and returns its assigned id.
func periodID(t *testing.T, a *app, name string, days []int, start, end string) int64 {
	t.Helper()
	if err := a.addPeriod(name, days, start, end); err != nil {
		t.Fatalf("addPeriod %s: %v", name, err)
	}
	periods, err := a.listPeriods()
	if err != nil {
		t.Fatalf("listPeriods: %v", err)
	}
	for _, p := range periods {
		if p.Name == name {
			return p.ID
		}
	}
	t.Fatalf("period %s not found after add", name)
	return 0
}

func TestUpdatePeriod(t *testing.T) {
	a := testApp(t)
	id := periodID(t, a, "Morning", []int{1}, "09:00", "10:00")
	if err := a.updatePeriod(id, "First", []int{1, 2}, "08:30", "09:30"); err != nil {
		t.Fatalf("updatePeriod: %v", err)
	}
	periods, _ := a.listPeriods()
	p := periods[0]
	if p.Name != "First" || p.Start != "08:30" || p.End != "09:30" || p.DayLabel != "Mon, Tue" {
		t.Errorf("update not applied: %+v", p)
	}
}

func TestUpdatePeriodSelfOverlapAllowed(t *testing.T) {
	a := testApp(t)
	id := periodID(t, a, "Morning", []int{1}, "09:00", "10:00")
	if err := a.updatePeriod(id, "Morning", []int{1}, "09:00", "10:30"); err != nil {
		t.Errorf("a period must not overlap itself: %v", err)
	}
}

func TestUpdatePeriodOverlapRejected(t *testing.T) {
	a := testApp(t)
	periodID(t, a, "First", []int{1}, "09:00", "10:00")
	id := periodID(t, a, "Second", []int{1}, "10:00", "11:00")
	err := a.updatePeriod(id, "Second", []int{1}, "09:30", "11:00")
	if err == nil || !strings.Contains(err.Error(), "First") {
		t.Errorf("expected overlap with First, got %v", err)
	}
}

func TestUpdatePeriodValidation(t *testing.T) {
	a := testApp(t)
	id := periodID(t, a, "Morning", []int{1}, "09:00", "10:00")
	cases := map[string]struct {
		name  string
		days  []int
		start string
		end   string
	}{
		"empty name":  {"", []int{1}, "09:00", "10:00"},
		"no weekdays": {"Morning", nil, "09:00", "10:00"},
		"bad clock":   {"Morning", []int{1}, "nope", "10:00"},
		"end<=start":  {"Morning", []int{1}, "10:00", "09:00"},
	}
	for label, c := range cases {
		if err := a.updatePeriod(id, c.name, c.days, c.start, c.end); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
}

func TestScheduleTemplateRenders(t *testing.T) {
	tpl := themeTemplates(t)
	out := render(t, tpl, "schedule.html", adminView{
		ThemeColor: "sky",
		Data: struct {
			Periods []periodRow
			Days    []dayOption
		}{
			Periods: []periodRow{{
				ID: 7, Name: "Morning", DayLabel: "Mon", Start: "09:00", End: "10:00",
				Selected: map[int]bool{1: true},
			}},
			Days: dayOptions(),
		},
	})
	if !strings.Contains(out, `data-id="7"`) || !strings.Contains(out, "Morning") {
		t.Error("period row did not render")
	}
	if !strings.Contains(out, `data-csrf=`) {
		t.Error("table should expose CSRF for the row script")
	}
	if !strings.Contains(out, `value="1" checked`) {
		t.Error("selected weekday should render checked")
	}
}
