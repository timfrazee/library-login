package main

import (
	"net/http"
	"time"
)

// adminView is the common template context for every admin page. Page-specific
// data hangs off Data.
type adminView struct {
	SiteName string
	Nav      string
	CSRF     string
	Flash    string
	FlashErr string
	Data     any
}

func (a *app) registerAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/setup", a.handleSetupForm)
	mux.HandleFunc("POST /admin/setup", a.handleSetupSubmit)
	mux.HandleFunc("GET /admin/login", a.handleLoginForm)
	mux.HandleFunc("POST /admin/login", a.handleLoginSubmit)
	mux.HandleFunc("POST /admin/logout", a.requireAuth(a.handleLogout))

	mux.HandleFunc("GET /admin/{$}", a.requireAuth(a.handleDashboard))

	mux.HandleFunc("GET /admin/roster", a.requireAuth(a.handleRoster))
	mux.HandleFunc("POST /admin/roster/save", a.requireAuth(a.handleRosterSave))
	mux.HandleFunc("POST /admin/roster/delete", a.requireAuth(a.handleRosterDelete))
	mux.HandleFunc("POST /admin/roster/purge", a.requireAuth(a.handleRosterPurge))
	mux.HandleFunc("POST /admin/roster/upload", a.requireAuth(a.handleRosterUpload))

	mux.HandleFunc("GET /admin/schedule", a.requireAuth(a.handleSchedule))
	mux.HandleFunc("POST /admin/schedule/add", a.requireAuth(a.handleScheduleAdd))
	mux.HandleFunc("POST /admin/schedule/delete", a.requireAuth(a.handleScheduleDelete))

	mux.HandleFunc("GET /admin/reports", a.requireAuth(a.handleReports))
	mux.HandleFunc("GET /admin/reports/export.csv", a.requireAuth(a.handleReportsExport))

	mux.HandleFunc("GET /admin/settings", a.requireAuth(a.handleSettings))
	mux.HandleFunc("POST /admin/settings", a.requireAuth(a.handleSettingsSave))
	mux.HandleFunc("POST /admin/settings/password", a.requireAuth(a.handlePasswordChange))
	mux.HandleFunc("POST /admin/settings/logo", a.requireAuth(a.handleLogoUpload))
	mux.HandleFunc("POST /admin/settings/logo/delete", a.requireAuth(a.handleLogoDelete))
	mux.HandleFunc("GET /admin/settings/backup", a.requireAuth(a.handleBackup))
}

func (a *app) render(w http.ResponseWriter, name string, v adminView) {
	if err := a.tpl.ExecuteTemplate(w, name, v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *app) adminBase(r *http.Request, nav string) adminView {
	nonce, _ := a.authSession(r)
	return adminView{
		SiteName: a.siteName(),
		Nav:      nav,
		CSRF:     a.csrfToken(nonce),
		Flash:    r.URL.Query().Get("flash"),
		FlashErr: r.URL.Query().Get("err"),
	}
}

type dashboardData struct {
	Date       string
	InCount    int
	TodayScans int
	Occupants  []occupant
}

func (a *app) handleDashboard(w http.ResponseWriter, r *http.Request) {
	loc := a.location()
	now := time.Now()
	occ, err := a.currentlyIn(loc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	startUTC, endUTC := dayBoundsUTC(loc, now)
	scans, err := a.countEventsBetween(startUTC, endUTC)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	v := a.adminBase(r, "dashboard")
	v.Data = dashboardData{
		Date:       now.In(loc).Format("Monday, January 2, 2006"),
		InCount:    len(occ),
		TodayScans: scans,
		Occupants:  occ,
	}
	a.render(w, "dashboard.html", v)
}
