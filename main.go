package main

import (
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

//go:embed assets
var assetsFS embed.FS

const (
	defaultPort = 8765
	dbFileName  = "library-login.db"

	cfgAdminHash           = "admin_password_hash"
	cfgSessionSecret       = "session_secret"
	cfgSiteName            = "site_name"
	cfgTimezone            = "timezone"
	cfgDebounce            = "debounce_seconds"
	cfgAutoCheckoutEnabled = "auto_checkout_enabled"
	cfgAutoCheckoutTime    = "auto_checkout_time"
	cfgKioskHeading        = "kiosk_heading"
	cfgKioskInstructions   = "kiosk_instructions"
)

type app struct {
	db      *sql.DB
	tpl     *template.Template
	dataDir string

	mu        sync.Mutex
	failCount int
	lockUntil time.Time
}

func main() {
	dataDir := flag.String("data", "", "directory for the database file (default: next to the binary)")
	port := flag.Int("port", defaultPort, "preferred HTTP port")
	openUI := flag.Bool("open", true, "open a browser window on startup")
	flag.Parse()

	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}
	db, err := openDB(filepath.Join(dir, dbFileName))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	tpl, err := template.ParseFS(assetsFS, "assets/*.html", "assets/admin/*.html")
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}
	a := &app{db: db, tpl: tpl, dataDir: dir}

	plat := detectPlatform()
	ln, boundPort, err := listen(plat.bindHost, *port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go a.autoCheckoutLoop()

	url := fmt.Sprintf("http://%s:%d/", plat.openHost, boundPort)
	fmt.Printf("Leave this running. The form is at %s\n", url)
	if *openUI {
		_ = openBrowser(url)
	}

	if err := http.Serve(ln, a.routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	staticSub, _ := fs.Sub(assetsFS, "assets/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	mux.HandleFunc("GET /{$}", a.handleKiosk)
	mux.HandleFunc("GET /logo", a.handleLogo)
	mux.HandleFunc("POST /submit", a.handleSubmit)
	a.registerAdmin(mux)
	return mux
}

func resolveDataDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// listen prefers the configured port but falls back to an OS-assigned free port
// so a busy 8765 never stops the app from starting.
func listen(host string, port int) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		if ln, err = net.Listen("tcp", fmt.Sprintf("%s:0", host)); err != nil {
			return nil, 0, err
		}
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}

func (a *app) location() *time.Location { return loadLocation(a.db) }

func (a *app) siteName() string { return configOr(a.db, cfgSiteName, "Library Login") }

func (a *app) debounce() time.Duration {
	n, err := strconv.Atoi(configOr(a.db, cfgDebounce, "5"))
	if err != nil || n < 0 {
		n = 5
	}
	return time.Duration(n) * time.Second
}
