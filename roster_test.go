package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLikePattern(t *testing.T) {
	cases := map[string]string{
		"al":  "%al%",
		"50%": `%50\%%`,
		"a_b": `%a\_b%`,
		`x\y`: `%x\\y%`,
	}
	for in, want := range cases {
		if got := likePattern(in); got != want {
			t.Errorf("likePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePage(t *testing.T) {
	for in, want := range map[string]int{"": 1, "0": 1, "-3": 1, "x": 1, "2": 2, "17": 17} {
		if got := parsePage(in); got != want {
			t.Errorf("parsePage(%q) = %d, want %d", in, got, want)
		}
	}
}

func testApp(t *testing.T) *app {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &app{db: db}
}

func TestRosterSearchAndPaging(t *testing.T) {
	a := testApp(t)
	people := []struct{ id, name, cat string }{
		{"1", "Alice", "staff"}, {"2", "Bob", "student"},
		{"3", "Carol", "student"}, {"4", "Dave", "staff"},
	}
	for _, p := range people {
		if _, err := a.upsertPerson(p.id, p.name, p.cat); err != nil {
			t.Fatal(err)
		}
	}

	if n, _ := a.countPeople("", false); n != 4 {
		t.Errorf("countPeople(all) = %d, want 4", n)
	}
	if n, _ := a.countPeople("student", false); n != 2 {
		t.Errorf("countPeople(student) = %d, want 2", n)
	}

	page, err := a.listPeoplePage("", false, 2, 0)
	if err != nil || len(page) != 2 {
		t.Fatalf("page len = %d (err %v), want 2", len(page), err)
	}
	if page[0].Name != "Alice" || page[1].Name != "Bob" {
		t.Errorf("unexpected page order: %s, %s", page[0].Name, page[1].Name)
	}
	page2, _ := a.listPeoplePage("", false, 2, 2)
	if len(page2) != 2 || page2[0].Name != "Carol" {
		t.Errorf("second page wrong: %+v", page2)
	}
}

func TestInactiveExcludedByDefault(t *testing.T) {
	a := testApp(t)
	for _, p := range []struct{ id, name string }{{"1", "Alice"}, {"2", "Bob"}} {
		if _, err := a.upsertPerson(p.id, p.name, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.setActive("2", false); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.countPeople("", false); n != 1 {
		t.Errorf("default count = %d, want 1 (inactive excluded)", n)
	}
	if n, _ := a.countPeople("", true); n != 2 {
		t.Errorf("include-inactive count = %d, want 2", n)
	}
	page, _ := a.listPeoplePage("", false, 10, 0)
	if len(page) != 1 || page[0].Name != "Alice" {
		t.Errorf("default page should list only active: %+v", page)
	}
}

func TestUpdatePersonPreservesActive(t *testing.T) {
	a := testApp(t)
	if _, err := a.upsertPerson("1", "Alice", "staff"); err != nil {
		t.Fatal(err)
	}
	if err := a.setActive("1", false); err != nil {
		t.Fatal(err)
	}
	if err := a.updatePerson("1", "Alicia", "faculty"); err != nil {
		t.Fatal(err)
	}
	page, _ := a.listPeoplePage("", true, 10, 0)
	if len(page) != 1 {
		t.Fatalf("want 1 person, got %d", len(page))
	}
	p := page[0]
	if p.Name != "Alicia" || p.Category != "faculty" {
		t.Errorf("update not applied: %+v", p)
	}
	if p.Active {
		t.Error("updatePerson must not reactivate a deactivated person")
	}
}

func TestRosterTemplateRenders(t *testing.T) {
	tpl := themeTemplates(t)
	out := render(t, tpl, "roster.html", adminView{
		ThemeColor: "sky",
		Data: rosterData{
			People:    []rosterRow{{ID: "1", Name: "Alice", Category: "staff", Active: true}},
			Query:     "al",
			Total:     1,
			Page:      1,
			PageCount: 1,
		},
	})
	if !strings.Contains(out, `data-id="1"`) || !strings.Contains(out, "Alice") {
		t.Error("roster row did not render")
	}
	if !strings.Contains(out, `data-csrf=`) {
		t.Error("table should expose CSRF for the row script")
	}
}
