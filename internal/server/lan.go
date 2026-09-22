package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/Dealwatch/Tabularium117/internal/lan"
)

// The LAN surface (KONZEPT.md section 6, task T7.1-T7.3).
const (
	// lanPath is the one write endpoint in the whole project. It is
	// reachable from the loopback listener only.
	lanPath   = "/api/v1/lan"
	lanQRPath = "/api/v1/lan/qr.png"
	// tokenCookie is a session cookie: no Expires, so it is gone when the
	// phone's browser is closed, and the token itself is gone when Tabularium 117
	// restarts.
	tokenCookie = "tabularium_token"
	// tokenParam is how the token arrives the first time, from the QR code.
	tokenParam = "token"
	// qrPixels is the QR image's edge length. 256 px is large enough for a
	// phone camera at arm's length and small enough to sit in the panel
	// without scaling.
	qrPixels = 256
	// maxToggleBody bounds the only body this server ever reads.
	maxToggleBody = 1 << 10
)

// --- which listener did this request arrive on? ---

// side distinguishes the two listeners. It is decided per connection, in
// ConnContext, and can therefore not be influenced by anything the client
// sends: a Host or X-Forwarded-For header is a claim, the accepting listener
// is a fact.
type side int

const (
	// sideLoopback is 127.0.0.1: the PC Tabularium 117 runs on, and the only side
	// that may change settings.
	sideLoopback side = iota
	// sideLAN is the second listener, the one phones reach.
	sideLAN
)

// sideKey is the context key carrying the side.
type sideKey struct{}

// requestSide reports which listener r arrived on.
//
// The zero value is the loopback, because Handler() - the handler this type
// hands out and that httptest drives - is the trusted one. The network is
// reached exclusively through lanMode, which always wraps the same handler in
// guardLAN and marks its connections; there is no other way to bind a second
// listener.
func requestSide(r *http.Request) side {
	if s, ok := r.Context().Value(sideKey{}).(side); ok {
		return s
	}
	return sideLoopback
}

// isLocal reports whether the request came from this PC over the loopback
// listener. It is what /api/v1/status reports as lan.local, and what the LAN
// switch, the QR code and the URL with the token are gated on.
func isLocal(r *http.Request) bool { return requestSide(r) == sideLoopback }

// --- the second listener ---

// bindError is a LAN listener that could not be bound - almost always because
// something else already holds the port on that address. Its message names
// the address and the port, because that is what the user has to free up; the
// toggle endpoint answers it, like every other refusal to switch on, with a
// 409 and the reason.
type bindError struct {
	addr lan.Address
	port int
	err  error
}

func (e *bindError) Error() string {
	return fmt.Sprintf("cannot listen on %s:%d: %v; is another program using that port?",
		e.addr.IP, e.port, e.err)
}

func (e *bindError) Unwrap() error { return e.err }

// lanState is the LAN listener's state as the API reports it.
type lanState struct {
	Enabled bool
	// Available is whether LAN mode could be switched on at all: a private
	// address exists (or is already in use) and the server knows its port.
	Available bool
	Addr      lan.Address
	Port      int
	// Reason explains an Available of false, in English, for the UI to show
	// next to the disabled switch.
	Reason string
}

// lanMode owns the second listener: the one on a private IPv4 address that
// phones in the same network reach (KONZEPT.md section 6).
//
// It is off until something switches it on - the --lan flag at start or the
// toggle endpoint at runtime - and switching it off closes the listener and
// every connection on it.
type lanMode struct {
	log *slog.Logger
	// handler is the guarded handler: client filter and token check in front
	// of the same site the loopback listener serves.
	handler       http.Handler
	interfaces    func() ([]lan.Interface, error)
	wantIP        string
	allowLoopback bool

	// opMu serialises enable and disable, so that two toggles cannot
	// interleave around the bind and leave the listener in a state nobody
	// asked for. mu guards the fields below and is held only briefly.
	opMu sync.Mutex

	mu      sync.Mutex
	port    int
	enabled bool
	addr    lan.Address
	ln      net.Listener
	srv     *http.Server
	closed  bool
}

// setPort tells the LAN listener which port to use: the same one the
// loopback listener bound, which is only known after binding when --port 0
// picked it.
func (m *lanMode) setPort(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.port = port
}

// candidate resolves the address LAN mode would bind: the one --lan-ip
// names, or the best private address this PC has.
func (m *lanMode) candidate() (lan.Address, error) {
	ifaces, err := m.interfaces()
	if err != nil {
		return lan.Address{}, err
	}
	if m.wantIP != "" {
		return lan.Resolve(m.wantIP, ifaces, m.allowLoopback)
	}
	addr, ok := lan.Pick(ifaces)
	if !ok {
		return lan.Address{}, errors.New("this PC has no private network address " +
			"(10.x.x.x, 172.16-31.x.x or 192.168.x.x); connect it to your home network first")
	}
	return addr, nil
}

// bound reports the address and port the LAN listener is serving on, or
// false when it is not serving. It is what the host check compares against.
func (m *lanMode) bound() (netip.Addr, int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || !m.addr.IP.IsValid() {
		return netip.Addr{}, 0, false
	}
	return m.addr.IP, m.port, true
}

// isEnabled is the cheap question: is the second listener open? It is what
// the status answers with, on every request and on every broadcast, so it
// must not go and ask the operating system for its interfaces the way state()
// does.
func (m *lanMode) isEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// state reports what the API answers with. It enumerates the machine's
// interfaces when LAN mode is off, to say whether it could be switched on, so
// it belongs on the LAN endpoint and not in a hot path.
func (m *lanMode) state() lanState {
	m.mu.Lock()
	enabled, addr, port, closed := m.enabled, m.addr, m.port, m.closed
	m.mu.Unlock()

	st := lanState{Enabled: enabled, Addr: addr, Port: port}
	if enabled {
		st.Available = true
		return st
	}
	if closed {
		st.Reason = "the server is shutting down"
		return st
	}
	// What the machine has comes first: "no private address" is the reason a
	// user can act on, and it is true whether or not the listener is up yet.
	candidate, err := m.candidate()
	if err != nil {
		st.Reason = err.Error()
		return st
	}
	st.Addr = candidate
	if port == 0 {
		st.Reason = "the server is not listening yet"
		return st
	}
	st.Available = true
	return st
}

// enable binds the LAN address and starts serving on it. Binding happens
// before anything is reported as enabled, so a caller that gets no error
// knows the address is really open.
func (m *lanMode) enable() (lanState, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return m.state(), errors.New("the server is shutting down")
	}
	if m.enabled {
		m.mu.Unlock()
		return m.state(), nil
	}
	port := m.port
	m.mu.Unlock()

	// The address first: "this PC has no such address" is about the machine
	// and is true whether or not the listener is up, and it is the answer
	// the user can act on.
	addr, err := m.candidate()
	if err != nil {
		return m.state(), err
	}
	if port == 0 {
		return m.state(), errors.New("the server is not listening yet")
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(addr.IP.String(), strconv.Itoa(port)))
	if err != nil {
		return m.state(), &bindError{addr: addr, port: port, err: err}
	}
	srv := &http.Server{
		Handler:           m.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(m.log.Handler(), slog.LevelDebug),
		// This is the whole basis of the "which listener was it" decision:
		// every connection accepted here is marked, once, at accept time.
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, sideKey{}, sideLAN)
		},
	}

	m.mu.Lock()
	if m.closed || m.enabled {
		// Somebody won the race in between; drop what was just opened.
		m.mu.Unlock()
		ln.Close()
		return m.state(), nil
	}
	m.enabled, m.addr, m.ln, m.srv = true, addr, ln, srv
	m.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	// The token is never logged: the log often ends up in a bug report, and
	// the UI is the only place that hands it out (KONZEPT.md section 6).
	m.log.Info(fmt.Sprintf("LAN mode enabled on http://%s/ (token in the UI)",
		net.JoinHostPort(addr.IP.String(), strconv.Itoa(port))),
		"interface", addr.Interface)
	return m.state(), nil
}

// disable closes the LAN listener and every connection on it - including the
// event streams, which would otherwise keep a phone updated after the switch
// was turned off.
func (m *lanMode) disable() lanState {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	srv := m.srv
	m.enabled, m.addr, m.ln, m.srv = false, lan.Address{}, nil, nil
	m.mu.Unlock()

	if srv != nil {
		// Close, not Shutdown: a phone must lose access now, not when its
		// event stream happens to end.
		srv.Close()
		m.log.Info("LAN mode disabled")
	}
	return m.state()
}

// closeAll ends LAN mode for good, as part of the server's shutdown. The flag
// is set before the listener is closed, so a toggle that is halfway through
// enabling finds it and the close that follows takes that listener with it.
func (m *lanMode) closeAll() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.disable()
}

// url is the address a phone opens, with the token. It is only ever sent to
// the loopback listener and never logged.
func (m *lanMode) url(token string) string {
	m.mu.Lock()
	enabled, addr, port := m.enabled, m.addr, m.port
	m.mu.Unlock()
	if !enabled {
		return ""
	}
	return fmt.Sprintf("http://%s/?%s=%s",
		net.JoinHostPort(addr.IP.String(), strconv.Itoa(port)), tokenParam, token)
}

// --- the guard in front of the LAN listener ---

// guardLAN is everything that stands between the network and the site: the
// RFC 1918 client filter and the access token (KONZEPT.md section 6). It is
// the handler the LAN listener serves, and it is the only handler that does.
func (s *Server) guardLAN(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The guard answers before readOnly does, so it sets the same
		// headers itself: a refusal must not be cached either, and a URL
		// carrying the token must never travel in a Referer.
		securityHeaders(w)
		if !s.clientAllowed(r) {
			// The address comes from the accepted connection, never from a
			// header: X-Forwarded-For is whatever the sender typed.
			s.log.Warn("refused a LAN request from outside the private network", "remote", r.RemoteAddr)
			writeError(w, http.StatusForbidden, "only clients in your own private network may use this address")
			return
		}
		if !s.checkToken(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientAllowed reports whether the peer may be served on the LAN listener.
func (s *Server) clientAllowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// No port means this is not a TCP peer as we know it; refusing is
		// the only safe reading.
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return s.isClientAllowed(addr.Unmap())
}

// checkToken enforces the access token on the LAN listener and reports
// whether the request may continue.
//
// The token arrives once, in the QR code's URL, and is exchanged for a
// session cookie straight away: a 303 to "/" takes it out of the address bar
// and out of the browser's history.
func (s *Server) checkToken(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/" {
		if presented := r.URL.Query().Get(tokenParam); presented != "" {
			if !lan.TokenEqual(presented, s.token) {
				s.denyToken(w, r)
				return false
			}
			http.SetCookie(w, &http.Cookie{
				Name:     tokenCookie,
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				// No Expires and no MaxAge: a session cookie, gone with the
				// browser, and worthless after Tabularium 117 restarts anyway.
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return false
		}
	}
	if cookie, err := r.Cookie(tokenCookie); err == nil && lan.TokenEqual(cookie.Value, s.token) {
		return true
	}
	s.denyToken(w, r)
	return false
}

// denyToken answers a request without a valid token. The API gets the usual
// JSON error - it is fetched by code - and everything else gets a page that
// says what to do, in both languages, with nothing in it that would help
// someone guess the token.
func (s *Server) denyToken(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusUnauthorized, "this address needs the access token from the QR code")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	io.WriteString(w, unauthorizedPage)
}

// unauthorizedPage is the 401 a phone sees without a valid token. It is
// deliberately tiny, static and free of any detail: no token, no address, no
// hint about what was wrong.
const unauthorizedPage = `<!DOCTYPE html>
<html lang="de">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Tabularium 117</title>
<style>
body { margin: 0; padding: 2rem 1rem; font-family: system-ui, sans-serif; line-height: 1.5;
       color-scheme: light dark; }
main { max-width: 32rem; margin: 0 auto; }
h1 { font-size: 1.25rem; }
p { margin: .75rem 0; }
</style>
</head>
<body>
<main>
<h1>Tabularium 117</h1>
<p lang="de">Kein gültiger Zugriffscode. Bitte den QR-Code in Tabularium 117 auf dem PC erneut scannen.</p>
<p lang="en">No valid access token. Please scan the QR code in Tabularium 117 on the PC again.</p>
</main>
</body>
</html>
`

// --- the endpoints ---

// lanStateDTO is what GET and POST /api/v1/lan answer with (KONZEPT.md
// section 5).
type lanStateDTO struct {
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	IP        string `json:"ip"`
	Interface string `json:"interface,omitempty"`
	Port      int    `json:"port"`
	// URL carries the token and is therefore only ever filled for the
	// loopback listener - the PC in front of the user.
	URL string `json:"url,omitempty"`
	// Reason explains why the switch cannot be used, when it cannot.
	Reason string `json:"reason,omitempty"`
}

// lanStateDTO renders the LAN state for r. Everything secret depends on the
// side the request came from, so the request is part of the rendering.
func (s *Server) lanStateDTO(r *http.Request) lanStateDTO {
	st := s.lan.state()
	out := lanStateDTO{
		Enabled:   st.Enabled,
		Available: st.Available,
		Port:      st.Port,
		Interface: st.Addr.Interface,
		Reason:    st.Reason,
	}
	if st.Addr.IP.IsValid() {
		out.IP = st.Addr.IP.String()
	}
	if st.Enabled && isLocal(r) {
		out.URL = s.lan.url(s.token)
	}
	return out
}

// handleLAN serves the LAN switch: GET reports the state, POST changes it.
//
// POST is the only write in the whole API. It is accepted on the loopback
// listener only - a phone with a valid token is still a phone and does not
// get to open the network wider (KONZEPT.md section 6).
func (s *Server) handleLAN(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, http.StatusOK, s.lanStateDTO(r))
	case http.MethodPost:
		s.handleLANToggle(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeError(w, http.StatusMethodNotAllowed, "use GET to read the LAN state or POST to change it")
	}
}

// toggleRequest is the body of POST /api/v1/lan.
type toggleRequest struct {
	// A pointer so that a body without the field is a mistake rather than
	// "switch it off".
	Enabled *bool `json:"enabled"`
}

func (s *Server) handleLANToggle(w http.ResponseWriter, r *http.Request) {
	if !isLocal(r) {
		writeError(w, http.StatusForbidden,
			"LAN mode can only be switched from Tabularium 117 on the PC itself")
		return
	}
	if !s.originAllowed(r) {
		// Cross-site request forgery defence for a service on localhost: a
		// page on the internet must not be able to open the user's network,
		// and a browser attaches the Origin header to every POST it makes.
		s.log.Warn("refused a LAN toggle with a foreign origin", "origin", r.Header.Get("Origin"))
		writeError(w, http.StatusForbidden, "this request did not come from Tabularium 117's own page")
		return
	}
	if mediaType := contentType(r); mediaType != contentTypeJSONShort {
		writeError(w, http.StatusUnsupportedMediaType, "send application/json")
		return
	}

	var body toggleRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxToggleBody)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, `the body must be {"enabled": true} or {"enabled": false}`)
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, `the body must name "enabled" as true or false`)
		return
	}

	if !*body.Enabled {
		s.lan.disable()
		s.PublishStatus()
		writeJSON(w, http.StatusOK, s.lanStateDTO(r))
		return
	}

	if _, err := s.lan.enable(); err != nil {
		// 409 rather than 500 or 400: the request is fine, the machine is
		// not in a state to do it - the port is taken, or there is no
		// private address to bind. The reason is the answer, because the UI
		// shows it next to the switch.
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.PublishStatus()
	writeJSON(w, http.StatusOK, s.lanStateDTO(r))
}

// contentTypeJSONShort is the media type the toggle accepts, without
// parameters.
const contentTypeJSONShort = "application/json"

// contentType returns the request's media type in lower case, without any
// parameters ("application/json; charset=utf-8" -> "application/json").
func contentType(r *http.Request) string {
	value, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	return strings.ToLower(strings.TrimSpace(value))
}

// originAllowed reports whether a write may come from this Origin.
//
// A request without an Origin header is not from a browser page (curl, a
// script on the PC), which the loopback check has already vetted. A request
// with one must come from Tabularium 117's own loopback page.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host != "localhost" {
		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Unmap().IsLoopback() {
			return false
		}
	}
	// The port has to match the one this server is listening on: another
	// service on localhost is a different origin and must not reach in here.
	if port := s.loopbackPort(); port != 0 {
		return u.Port() == strconv.Itoa(port)
	}
	return true
}

// loopbackPort is the port the loopback listener bound, or 0 before it has.
func (s *Server) loopbackPort() int {
	addr := s.Addr()
	if addr == nil {
		return 0
	}
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return tcp.Port
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return n
}

// handleLANQR serves the QR code a phone scans: the LAN URL including the
// token, as a PNG.
//
// It is available on the loopback listener only and only while LAN mode is
// on - a QR code for an address nothing is listening on would be a puzzle,
// and one served to the LAN would hand the token to whoever asked.
func (s *Server) handleLANQR(w http.ResponseWriter, r *http.Request) {
	if !isLocal(r) || !s.lan.isEnabled() {
		writeError(w, http.StatusNotFound, "there is no QR code: LAN mode is off")
		return
	}
	png, err := qrcode.Encode(s.lan.url(s.token), qrcode.Medium, qrPixels)
	if err != nil {
		s.logError("cannot render the QR code", err)
		writeError(w, http.StatusInternalServerError, "cannot render the QR code")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Cache-Control: no-store is set for every response (readOnly); it
	// matters here, because this image is a secret.
	w.WriteHeader(http.StatusOK)
	w.Write(png)
}
