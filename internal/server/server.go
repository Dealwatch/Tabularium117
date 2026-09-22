package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/catalog"
	"github.com/Dealwatch/Tabularium117/internal/lan"
	"github.com/Dealwatch/Tabularium117/internal/state"
	"github.com/Dealwatch/Tabularium117/internal/store"
	"github.com/Dealwatch/Tabularium117/web"
)

const (
	// DefaultHeartbeat is how often an idle event stream sends a comment line
	// so that proxies and sleeping phones keep the connection open.
	DefaultHeartbeat = 15 * time.Second
	// shutdownTimeout bounds the graceful shutdown. Requests are short and
	// the streams are closed first, so this is a backstop, not a budget.
	shutdownTimeout = 5 * time.Second
	// readHeaderTimeout keeps a half-open connection from occupying a
	// goroutine for ever. There is deliberately no write timeout: an event
	// stream is a response that never ends.
	readHeaderTimeout = 10 * time.Second
)

// AlertSource is the live rule engine, as the server needs it: the set of
// alerts that are open right now. *alerts.Engine implements it.
//
// It is an interface so that the server does not own the engine's lifetime
// and tests can hand in a fixed set of alerts.
type AlertSource interface {
	Active() []alerts.Alert
}

// Options configures a Server. Only State is required.
type Options struct {
	// State is the live view the API reads. Required.
	State *state.State
	// Store is the history database. nil means the run keeps no history;
	// the history endpoints then answer 503 instead of pretending.
	Store *store.Store
	// Alerts is the live rule engine. nil means no alerts are evaluated in
	// this run, and the active list is empty rather than an error: the UI is
	// built against the endpoint either way.
	Alerts AlertSource
	// Catalog resolves GUIDs to names. nil uses catalog.Default().
	Catalog *catalog.Catalog
	// Log receives serving problems. nil discards them.
	Log *slog.Logger
	// Version is reported by /api/v1/status.
	Version string
	// Heartbeat is the event stream's keep-alive interval. Zero or less uses
	// DefaultHeartbeat.
	Heartbeat time.Duration
	// Now is the clock used for history ranges. nil uses time.Now.
	Now func() time.Time
	// OnReady, when set, is called with the bound address once ListenAndServe
	// has the listener but before it starts serving. It is how the command
	// learns the real port (--port 0) and when it may open a browser.
	OnReady func(net.Addr)
	// LAN configures the second listener. Its zero value is "auto-select a
	// private address when someone switches LAN mode on".
	LAN LANOptions
}

// LANOptions configures LAN mode (KONZEPT.md section 6). Every field has a
// safe zero value; the two function fields exist so that tests do not depend
// on the interfaces of the machine they run on.
type LANOptions struct {
	// IP pins the address to bind (--lan-ip). Empty means "pick the best
	// private address". It must be an RFC 1918 address this PC really has,
	// or, with AllowLoopback, a loopback address.
	IP string
	// AllowLoopback accepts a 127.0.0.0/8 address for IP and as a client
	// address. It is the documented test aid (README.md): loopback is
	// reachable from this machine only, so it exercises the LAN code path
	// without opening anything to the network.
	AllowLoopback bool
	// Interfaces lists this machine's network interfaces. nil uses
	// lan.SystemInterfaces.
	Interfaces func() ([]lan.Interface, error)
	// ClientAllowed decides which peer address the LAN listener serves. nil
	// uses lan.IsPrivate (plus the loopback when AllowLoopback is set),
	// which is the rule KONZEPT.md section 6 requires.
	ClientAllowed func(netip.Addr) bool
}

// Server serves the embedded UI, the read-only REST API and the SSE stream.
//
// It has two faces. The loopback listener is the PC Tabularium 117 runs on: it is
// trusted, needs no credential, and is the only side that may switch LAN
// mode. The optional LAN listener is a second listener on one private IPv4
// address, behind the RFC 1918 client filter and the access token
// (KONZEPT.md section 6). /api/v1/lan is the only endpoint that writes
// anything, and it writes nothing but that switch.
type Server struct {
	state     *state.State
	store     *store.Store
	alerts    AlertSource
	catalog   *catalog.Catalog
	log       *slog.Logger
	version   string
	heartbeat time.Duration
	now       func() time.Time
	onReady   func(net.Addr)

	// site is the routed handler; handler is site behind the host check and
	// is what Handler() returns.
	site    http.Handler
	handler http.Handler
	hub     *hub

	// token is the LAN access token: one per process start, never logged
	// (KONZEPT.md section 6).
	token string
	// isClientAllowed decides which peer the LAN listener serves.
	isClientAllowed func(netip.Addr) bool
	lan             *lanMode

	mu   sync.Mutex
	addr net.Addr
}

// New builds a Server from opts. It panics when State is missing: a server
// without the live view has nothing to serve, and that is a wiring mistake,
// not a runtime condition.
func New(opts Options) *Server {
	if opts.State == nil {
		panic("server: Options.State is required")
	}
	s := &Server{
		state:     opts.State,
		store:     opts.Store,
		alerts:    opts.Alerts,
		catalog:   opts.Catalog,
		log:       opts.Log,
		version:   opts.Version,
		heartbeat: opts.Heartbeat,
		now:       opts.Now,
		onReady:   opts.OnReady,
	}
	if s.catalog == nil {
		s.catalog = catalog.Default()
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.version == "" {
		s.version = "dev"
	}
	if s.heartbeat <= 0 {
		s.heartbeat = DefaultHeartbeat
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.hub = newHub(s.log)
	// site is the server without the host check; handler is what anything
	// outside this package gets, and it always carries the check.
	s.site = s.routes()
	s.handler = s.checkHost(s.site)

	// One token per process start. It exists even when LAN mode is never
	// switched on, so that switching it on is a listener away and nothing
	// has to be generated while a user is waiting.
	s.token = lan.NewToken()
	s.isClientAllowed = opts.LAN.ClientAllowed
	if s.isClientAllowed == nil {
		allowLoopback := opts.LAN.AllowLoopback
		s.isClientAllowed = func(addr netip.Addr) bool {
			return lan.IsPrivate(addr) || (allowLoopback && addr.IsLoopback())
		}
	}
	interfaces := opts.LAN.Interfaces
	if interfaces == nil {
		interfaces = lan.SystemInterfaces
	}
	s.lan = &lanMode{
		log: s.log,
		// The LAN listener never serves the bare handler: the client filter
		// and the token check are wired in here, once, and lanMode is the
		// only thing that binds a second listener.
		// The host check comes first here too: a request for the wrong name
		// is refused before the token check can set a cookie or redirect.
		handler:       s.checkHost(s.guardLAN(s.site)),
		interfaces:    interfaces,
		wantIP:        opts.LAN.IP,
		allowLoopback: opts.LAN.AllowLoopback,
	}
	return s
}

// EnableLAN switches LAN mode on, exactly as the toggle endpoint does. It is
// how --lan is honoured at start, and it reports why it could not.
func (s *Server) EnableLAN() error {
	_, err := s.lan.enable()
	if err == nil {
		s.PublishStatus()
	}
	return err
}

// LANEnabled reports whether the LAN listener is currently open.
func (s *Server) LANEnabled() bool { return s.lan.isEnabled() }

// Handler returns the whole site: UI, API and event stream. It is what
// ListenAndServe serves and what tests drive through httptest.
func (s *Server) Handler() http.Handler { return s.handler }

// Addr reports the bound address, or nil before ListenAndServe has a
// listener.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// ListenAndServe binds addr and serves until ctx is done, then shuts down
// gracefully and returns nil. Binding happens before serving, so a caller
// that waits for Options.OnReady knows the port is really open.
//
// A failure to bind is returned as-is; everything else that ends the server
// on purpose - a cancelled context, a clean shutdown - is not an error.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server: listen on %s: %w", addr, err)
	}
	s.mu.Lock()
	s.addr = ln.Addr()
	s.mu.Unlock()
	// The LAN listener uses the same port on a different address, which is
	// only known now when --port 0 picked it.
	s.lan.setPort(s.loopbackPort())
	if s.onReady != nil {
		s.onReady(ln.Addr())
	}
	return s.serve(ctx, ln)
}

// serve runs the HTTP server on ln until ctx is done.
func (s *Server) serve(ctx context.Context, ln net.Listener) error {
	httpSrv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
	}

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("server: serve: %w", err)
	case <-ctx.Done():
	}

	// The event streams are live requests that never end by themselves, so
	// Shutdown would wait out its whole timeout for them. Ending them first
	// turns the shutdown into the clean case it should be.
	s.hub.closeAll()
	// Both listeners go: a phone must not keep a connection to a Tabularium 117
	// that is on its way out.
	s.lan.closeAll()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := httpSrv.Shutdown(shutdownCtx)
	<-errc
	if err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	return nil
}

// securityHeaders marks a response uncacheable and keeps the Referer header
// off every link the page carries. Both matter most on the LAN side, where a
// URL can contain the access token, so both are set on every response of
// both listeners rather than on the paths somebody remembered.
func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// Nothing here is meant to be framed, and since the UI now carries a
	// switch that opens the network, a page that could frame it could also
	// trick someone into flipping it. Both headers say the same thing; the
	// older one is understood by more browsers.
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
}

// staticContentTypes is what the embedded UI is served as, whatever machine
// the binary runs on.
//
// http.FileServerFS derives the type from mime.TypeByExtension, and on
// Windows that function reads the registry: ".js" comes back as
// "text/javascript" on one PC and as "application/javascript" on another
// (a GitHub runner), from the same binary and the same files. Both of those a
// browser accepts for an ES module - but the same registry key is also the
// one other installers write, and a machine where it says "text/plain"
// refuses every module in web/js and shows a blank page. That failure would
// reach exactly one user at a time and be unreproducible everywhere else.
//
// So the types are pinned here rather than asked for. Registering them
// overrides what the registry said, which is also why this is init() and not
// something a caller has to remember.
var staticContentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json",
	".png":  "image/png",
	".svg":  "image/svg+xml",
}

func init() { pinContentTypes() }

// pinContentTypes registers staticContentTypes, overriding whatever the
// machine's own table answered for those extensions. It is a function rather
// than the body of init so that a test can put a machine's wrong answer back
// and check that pinning wins (server_internal_test.go).
func pinContentTypes() {
	for ext, typ := range staticContentTypes {
		// The only error AddExtensionType returns is for an extension that
		// does not start with a dot, which this map cannot produce; the
		// tests check the result rather than trusting it.
		_ = mime.AddExtensionType(ext, typ)
	}
}

// routes builds the handler. The endpoints themselves are documented in
// KONZEPT.md section 5, which is their reference.
func (s *Server) routes() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("/api/v1/status", s.handleStatus)
	api.HandleFunc("/api/v1/islands", s.handleIslands)
	api.HandleFunc("/api/v1/islands/{id}/products", s.handleProducts)
	api.HandleFunc("/api/v1/islands/{id}/products/{guid}/history", s.handleHistory)
	api.HandleFunc("/api/v1/islands/{id}/efficiency", s.handleEfficiency)
	api.HandleFunc("/api/v1/alerts", s.handleAlerts)
	api.HandleFunc("/api/v1/events", s.handleEvents)
	api.HandleFunc(lanPath, s.handleLAN)
	api.HandleFunc(lanQRPath, s.handleLANQR)
	// Anything else below /api/v1 is a JSON 404, not the UI's 404 page.
	api.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})

	root := http.NewServeMux()
	root.Handle("/api/v1/", api)
	root.Handle("/", http.FileServerFS(web.FS))
	return readOnly(root)
}

// checkHost is the first thing every request meets, on both listeners and in
// front of everything else - the static files, the event stream, the LAN
// guard and its 401 page included.
//
// It is the defence against DNS rebinding, which is the standing threat to
// any service on localhost: a page on the internet whose name resolves to
// 127.0.0.1 after its TTL runs out becomes, to the browser, same-origin with
// Tabularium 117. The Origin check stops it from writing, but nothing would stop it
// from reading - and one of the things it could read is the LAN address with
// the access token. The name in the Host header is the one thing that still
// tells the two apart: a rebound page asks for "evil.example", never for
// "127.0.0.1".
//
// 421 is the honest answer: this server is not the one that name belongs to.
func (s *Server) checkHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.hostAllowed(r) {
			next.ServeHTTP(w, r)
			return
		}
		securityHeaders(w)
		s.log.Warn("refused a request for a host this server does not serve",
			"host", r.Host, "path", r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusMisdirectedRequest,
				"this is not the address Tabularium 117 serves; open it as http://127.0.0.1:<port>/")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMisdirectedRequest)
		io.WriteString(w, "Tabularium 117 does not serve this host name.\n")
	})
}

// hostAllowed reports whether r asked for a name this listener answers to.
func (s *Server) hostAllowed(r *http.Request) bool {
	if requestSide(r) == sideLAN {
		addr, port, ok := s.lan.bound()
		if !ok {
			// Nothing is listening on the LAN address, so nothing can have
			// arrived through it; this is a handler driven by a test.
			return true
		}
		host, hostPort, ok := splitHostPort(r.Host)
		if !ok || !portMatches(hostPort, port) {
			return false
		}
		asked, err := netip.ParseAddr(host)
		return err == nil && asked.Unmap() == addr
	}

	port := s.loopbackPort()
	if port == 0 {
		// The server was never bound - Handler() driven directly by a test.
		// There is no address to compare against and nothing to reach it.
		return true
	}
	host, hostPort, ok := splitHostPort(r.Host)
	if !ok || !portMatches(hostPort, port) {
		return false
	}
	// "localhost" is a name, not an address, and browsers do not lowercase it
	// for us.
	if strings.EqualFold(host, "localhost") {
		return true
	}
	asked, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	asked = asked.Unmap()
	if asked == loopbackV4 || asked == loopbackV6 {
		return true
	}
	// Whatever this server really bound, in case a caller chose another
	// loopback alias than the command does.
	if bound, ok := boundAddr(s.Addr()); ok && bound == asked {
		return true
	}
	return false
}

// loopbackV4 and loopbackV6 are the two addresses the PC's own browser uses.
var (
	loopbackV4 = netip.MustParseAddr("127.0.0.1")
	loopbackV6 = netip.MustParseAddr("::1")
)

// splitHostPort splits a Host header into its name and its port. The port may
// be absent ("example.com", "[::1]"), which HTTP reads as the scheme's
// default; the brackets of a literal IPv6 address are removed either way.
func splitHostPort(value string) (host, port string, ok bool) {
	if value == "" {
		return "", "", false
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		return host, port, host != ""
	}
	if inner, found := strings.CutPrefix(value, "["); found {
		if inner, found := strings.CutSuffix(inner, "]"); found {
			return inner, "", inner != ""
		}
		return "", "", false
	}
	// A second colon means this was not "host:port" but something malformed.
	if strings.Contains(value, ":") {
		return "", "", false
	}
	return value, "", true
}

// portMatches reports whether the port from a Host header is the one this
// server bound. An absent port means 80, the default for http.
func portMatches(hostPort string, bound int) bool {
	if hostPort == "" {
		return bound == 80
	}
	return hostPort == strconv.Itoa(bound)
}

// boundAddr pulls the IP out of a listener address.
func boundAddr(addr net.Addr) (netip.Addr, bool) {
	if addr == nil {
		return netip.Addr{}, false
	}
	if tcp, ok := addr.(*net.TCPAddr); ok {
		if ip, ok := netip.AddrFromSlice(tcp.IP); ok {
			return ip.Unmap(), true
		}
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// readOnly is the one place that enforces "no write path": whatever is
// registered below it, only GET and HEAD reach it - with the single, named
// exception of the LAN switch, which is a setting of Tabularium 117 itself and
// still changes nothing in the game or the history (KONZEPT.md section 6).
//
// It also marks every response uncacheable - the data is live, and the UI is
// replaced by the next version of the executable - and suppresses the
// Referer header, so that a URL carrying the access token cannot leak into
// anything a page happens to link to.
func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		switch {
		case r.Method == http.MethodGet || r.Method == http.MethodHead:
		case r.Method == http.MethodPost && r.URL.Path == lanPath:
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "this API is read-only; use GET")
			return
		}
		next.ServeHTTP(w, r)
	})
}
