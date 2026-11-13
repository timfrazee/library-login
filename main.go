package main

import (
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

//go:embed assets
var assetsFS embed.FS

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

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
	loadDotEnv(".env")

	dataDir := flag.String("data", "", "data directory (overrides env; default: per-user folder)")
	port := flag.Int("port", defaultPort, "preferred HTTP port")
	openUI := flag.Bool("open", false, "open a browser window on startup")
	installLauncher := flag.Bool("install-launcher", false, "register a desktop launcher entry (Linux/ChromeOS)")
	host := flag.String("host", "", "bind address, e.g. 0.0.0.0 to serve the LAN (default: auto)")
	lan := flag.Bool("lan", false, "serve on all interfaces (shortcut for -host 0.0.0.0)")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })
	doubleClickLaunch = isDoubleClick()

	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		fatalf("resolve data dir: %v", err)
	}
	db, err := openDB(filepath.Join(dir, dbFileName))
	if err != nil {
		fatalf("open database: %v", err)
	}
	defer db.Close()

	tpl, err := template.ParseFS(assetsFS, "assets/*.html", "assets/admin/*.html")
	if err != nil {
		fatalf("parse templates: %v", err)
	}
	a := &app{db: db, tpl: tpl, dataDir: dir}

	if *installLauncher {
		if exe, err := os.Executable(); err == nil {
			installDesktopLauncher(exe)
		}
	}

	plat := detectPlatform()
	bindHost := plat.bindHost
	override := hostFromConfig(*host, *lan)
	if doubleClickLaunch && !explicitMode(setFlags) {
		switch windowsLaunchMenu(dir) {
		case launchNetwork:
			override = "0.0.0.0"
		case launchLocal:
			override = "127.0.0.1"
		}
	}
	if override != "" {
		bindHost = override
	}
	ln, boundPort, err := listen(bindHost, *port)
	if err != nil {
		fatalf("listen: %v", err)
	}

	go a.autoCheckoutLoop()

	url := fmt.Sprintf("http://%s:%d/", plat.openHost, boundPort)
	fmt.Printf("Library Login %s\n", version)
	fmt.Printf("Data folder: %s\n", dir)
	if servesExternally(override) {
		urls := lanURLs(boundPort)
		for _, u := range urls {
			fmt.Printf("On your network:  %s\n", u)
		}
		switch {
		case len(urls) == 1:
			fmt.Println("  Open that address in the browser on your other devices.")
		case len(urls) > 1:
			fmt.Println("  Open one of these in the browser on your other devices -- try the first one.")
		}
	}
	fmt.Printf("Leave this running. The form is at %s\n", url)
	if *openUI {
		_ = openBrowser(url)
	}

	if err := http.Serve(ln, a.routes()); err != nil {
		fatalf("serve: %v", err)
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

func resolveDataDir(flagData string) (string, error) {
	dir := dataDirFromConfig(flagData)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
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

// servesExternally reports whether the bind override exposes the app beyond
// loopback, which is when the reachable LAN address is worth printing.
func servesExternally(override string) bool {
	return override != "" && override != "127.0.0.1" && override != "localhost"
}

// lanURLs lists the http URLs other devices can use to reach this host. It
// prefers the single address on the default-route interface, which excludes VM,
// VPN, and host-only interfaces that merely carry private addresses. Only when
// that cannot be determined (e.g. an offline host) does it fall back to scanning
// every interface.
func lanURLs(port int) []string {
	if ip := primaryLANIP(); ip != nil {
		return []string{fmt.Sprintf("http://%s:%d/", ip, port)}
	}
	return enumeratedLANURLs(port)
}

// primaryLANIP returns the IPv4 source address the OS would use to reach the
// network. The UDP socket performs no I/O; dialing only resolves the route and
// binds the local address, so this works offline as long as a route exists.
func primaryLANIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil
	}
	defer conn.Close()
	ua, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	ip := ua.IP.To4()
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return nil
	}
	return ip
}

func enumeratedLANURLs(port int) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var private, other []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 ||
			iface.Flags&net.FlagLoopback != 0 ||
			iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Equal(ip.Mask(ipnet.Mask)) {
				continue
			}
			if ip.IsPrivate() {
				private = append(private, ip)
			} else {
				other = append(other, ip)
			}
		}
	}
	ips := private
	if len(ips) == 0 {
		ips = other
	}
	sort.SliceStable(ips, func(i, j int) bool { return lanRank(ips[i]) < lanRank(ips[j]) })
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, fmt.Sprintf("http://%s:%d/", ip, port))
	}
	return out
}

func lanRank(ip net.IP) int {
	switch {
	case ip[0] == 192 && ip[1] == 168:
		return 0
	case ip[0] == 10:
		return 1
	default:
		return 2
	}
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
