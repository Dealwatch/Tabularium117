package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/lan"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// lanRequest builds a request as it would arrive on the LAN listener: marked
// with the side the connection was accepted on, and from remote.
//
// Marking the context is exactly what the LAN listener's ConnContext does, so
// this exercises the real guard rather than a copy of it - and it lets the
// test pretend to be 8.8.8.8 without leaving the machine.
func lanRequest(method, target, remote string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remote
	return req.WithContext(context.WithValue(req.Context(), sideKey{}, sideLAN))
}

// lanTestServer is a server whose LAN side can be driven directly, without a
// second listener.
func lanTestServer(t *testing.T) *Server {
	t.Helper()
	return New(Options{
		State: state.New(),
		LAN:   LANOptions{IP: "192.168.1.5", Interfaces: fakeInterfaces},
	})
}

// fakeInterfaces is a machine with one private address, so that no test
// depends on the network of the machine it runs on.
func fakeInterfaces() ([]lan.Interface, error) {
	return []lan.Interface{
		{Name: "lo", Up: true, Loopback: true, Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{Name: "wlan0", Up: true, Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.5")}},
	}, nil
}

// serveLAN runs one request through the guarded handler the LAN listener
// serves.
func serveLAN(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.lan.handler.ServeHTTP(rec, req)
	return rec
}

// Only RFC 1918 clients are served on the LAN listener, and the decision is
// made on the accepted connection's address - never on a header, which is
// whatever the sender typed (KONZEPT.md section 6).
func TestLANClientFilter(t *testing.T) {
	s := lanTestServer(t)
	cookie := &http.Cookie{Name: tokenCookie, Value: s.token}

	allowed := []string{"10.0.0.5:4444", "172.16.0.1:4444", "172.20.1.1:4444",
		"172.31.255.254:4444", "192.168.1.10:4444", "[::ffff:10.1.2.3]:4444"}
	for _, remote := range allowed {
		req := lanRequest(http.MethodGet, "/api/v1/status", remote)
		req.AddCookie(cookie)
		if rec := serveLAN(s, req); rec.Code != http.StatusOK {
			t.Errorf("a request from %s = %d, want 200", remote, rec.Code)
		}
	}

	refused := []string{"8.8.8.8:4444", "172.32.0.1:4444", "172.15.255.255:4444",
		"169.254.1.1:4444", "127.0.0.1:4444", "[2001:db8::1]:4444", "not-an-address"}
	for _, remote := range refused {
		// Even with a perfectly valid token: the filter comes first.
		req := lanRequest(http.MethodGet, "/api/v1/status", remote)
		req.AddCookie(cookie)
		rec := serveLAN(s, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("a request from %s = %d, want 403", remote, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, `"error"`) {
			t.Errorf("a refused request from %s answered %q, want a JSON error", remote, body)
		}
	}

	// A forged header must change nothing.
	req := lanRequest(http.MethodGet, "/api/v1/status", "8.8.8.8:4444")
	req.AddCookie(cookie)
	req.Header.Set("X-Forwarded-For", "192.168.1.10")
	req.Header.Set("X-Real-IP", "192.168.1.10")
	if rec := serveLAN(s, req); rec.Code != http.StatusForbidden {
		t.Errorf("X-Forwarded-For got a public client in: %d", rec.Code)
	}
}

// Without the token nothing on the LAN listener answers: the API with JSON,
// everything else with a page that says what to do and nothing else.
func TestLANTokenIsRequired(t *testing.T) {
	s := lanTestServer(t)

	for _, path := range []string{"/api/v1/status", "/api/v1/islands", "/api/v1/events",
		"/api/v1/lan", "/api/v1/lan/qr.png"} {
		rec := serveLAN(s, lanRequest(http.MethodGet, path, "192.168.1.10:4444"))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != contentTypeJSON {
			t.Errorf("GET %s without a token: content type = %q, want JSON", path, ct)
		}
	}

	for _, path := range []string{"/", "/js/app.js", "/css/app.css"} {
		rec := serveLAN(s, lanRequest(http.MethodGet, path, "192.168.1.10:4444"))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s without a token: content type = %q, want HTML", path,
				rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(body, "QR") || !strings.Contains(body, "Zugriffscode") {
			t.Errorf("the 401 page does not explain itself in both languages:\n%s", body)
		}
		if strings.Contains(body, s.token) {
			t.Error("the 401 page contains the token")
		}
	}

	// A refusal is answered by the guard, before the rest of the stack: it
	// carries the same headers, or a 401 page would be cacheable and could be
	// framed.
	denied := serveLAN(s, lanRequest(http.MethodGet, "/", "192.168.1.10:4444"))
	for header, want := range map[string]string{
		"Referrer-Policy": "no-referrer",
		"Cache-Control":   "no-store",
		"X-Frame-Options": "DENY",
	} {
		if got := denied.Header().Get(header); got != want {
			t.Errorf("the 401 page: %s = %q, want %q", header, got, want)
		}
	}

	// A wrong token is no better than none, and must not set a cookie.
	rec := serveLAN(s, lanRequest(http.MethodGet, "/?token=wrong", "192.168.1.10:4444"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a wrong token = %d, want 401", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a wrong token was answered with a cookie")
	}
	// A token that is a prefix of the real one is a wrong token.
	rec = serveLAN(s, lanRequest(http.MethodGet, "/?token="+s.token[:len(s.token)-1], "192.168.1.10:4444"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a truncated token = %d, want 401", rec.Code)
	}
}

// The token arrives once in the URL and is exchanged for a session cookie, so
// that it leaves the address bar and the browser's history.
func TestLANTokenBecomesACookie(t *testing.T) {
	s := lanTestServer(t)

	rec := serveLAN(s, lanRequest(http.MethodGet, "/?token="+s.token, "192.168.1.10:4444"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a correct token = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want the token cookie", len(cookies))
	}
	c := cookies[0]
	switch {
	case c.Name != tokenCookie:
		t.Errorf("cookie name = %q, want %q", c.Name, tokenCookie)
	case c.Value != s.token:
		t.Error("the cookie does not carry the token")
	case !c.HttpOnly:
		t.Error("the token cookie is readable from JavaScript")
	case c.SameSite != http.SameSiteLaxMode:
		t.Errorf("cookie SameSite = %v, want Lax", c.SameSite)
	case c.Path != "/":
		t.Errorf("cookie path = %q, want /", c.Path)
	case !c.Expires.IsZero() || c.MaxAge != 0:
		t.Error("the token cookie outlives the browser session")
	}

	// And the cookie is then enough for the API.
	req := lanRequest(http.MethodGet, "/api/v1/islands", "192.168.1.10:4444")
	req.AddCookie(c)
	if got := serveLAN(s, req); got.Code != http.StatusOK {
		t.Errorf("the cookie was not accepted: %d", got.Code)
	}
}

// The loopback listener is the PC itself: no token, no filter, and it is the
// side that sees the switch, the URL and the QR code.
func TestLoopbackNeedsNoToken(t *testing.T) {
	s := lanTestServer(t)

	for _, path := range []string{"/", "/api/v1/status", "/api/v1/islands", "/api/v1/lan"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s on the loopback listener = %d, want 200", path, rec.Code)
		}
		for header, want := range map[string]string{
			"Referrer-Policy":         "no-referrer",
			"Cache-Control":           "no-store",
			"X-Frame-Options":         "DENY",
			"Content-Security-Policy": "frame-ancestors 'none'",
		} {
			if got := rec.Header().Get(header); got != want {
				t.Errorf("GET %s: %s = %q, want %q", path, header, got, want)
			}
		}
	}
}

// A request that arrives on the LAN listener must not be able to reach the
// switch, however valid its token is.
func TestLANClientCannotToggle(t *testing.T) {
	s := lanTestServer(t)

	req := lanRequest(http.MethodPost, lanPath, "192.168.1.10:4444")
	req.AddCookie(&http.Cookie{Name: tokenCookie, Value: s.token})
	req.Header.Set("Content-Type", "application/json")
	rec := serveLAN(s, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST %s from the LAN = %d, want 403", lanPath, rec.Code)
	}
	if s.LANEnabled() {
		t.Error("a LAN client switched LAN mode on")
	}
}

// The state a LAN client may see never contains the token, and the one the
// PC sees does once the listener is open.
func TestLANStateHidesTheURLFromTheLAN(t *testing.T) {
	s := lanTestServer(t)

	req := lanRequest(http.MethodGet, lanPath, "192.168.1.10:4444")
	req.AddCookie(&http.Cookie{Name: tokenCookie, Value: s.token})
	rec := serveLAN(s, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s from the LAN = %d", lanPath, rec.Code)
	}
	var got lanStateDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != "" {
		t.Errorf("the LAN state sent %q to a LAN client", got.URL)
	}
	if strings.Contains(rec.Body.String(), s.token) {
		t.Error("the LAN state leaked the token to a LAN client")
	}
}

// Without a candidate address the switch cannot be used, and the reason is
// the answer: the UI shows it instead of a switch that would always fail.
func TestLANUnavailableExplainsWhy(t *testing.T) {
	s := New(Options{
		State: state.New(),
		LAN: LANOptions{Interfaces: func() ([]lan.Interface, error) {
			return []lan.Interface{
				{Name: "lo", Up: true, Loopback: true, Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
				{Name: "eth0", Up: true, Addrs: []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
			}, nil
		}},
	})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, lanPath, nil))
	var got lanStateDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Available {
		t.Errorf("state = %+v, want it unavailable", got)
	}
	if !strings.Contains(got.Reason, "private network address") {
		t.Errorf("reason = %q, want it to name the missing private address", got.Reason)
	}
}

// Only Tabularium 117's own page may operate the switch. A POST that carries a
// foreign Origin is a cross-site request, even when it reaches the loopback
// listener from the browser of the user sitting in front of it.
func TestToggleOriginCheck(t *testing.T) {
	s := lanTestServer(t)

	cases := []struct {
		origin string
		want   bool
	}{
		{"", true},
		{"http://127.0.0.1:53118", true},
		{"http://localhost:53118", true},
		{"http://[::1]:53118", true},
		{"http://evil.example", false},
		{"https://127.0.0.1:53118", false},
		{"null", false},
		{"http://127.0.0.1.evil.example:53118", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, lanPath, strings.NewReader(`{"enabled":false}`))
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if got := s.originAllowed(req); got != tc.want {
			t.Errorf("origin %q allowed = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

// The Host header is compared by hand, because net.SplitHostPort alone does
// not cover the shapes a Host may take: a bare name, a bracketed IPv6 literal
// without a port, and outright nonsense.
func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		value string
		host  string
		port  string
		ok    bool
	}{
		{"127.0.0.1:53118", "127.0.0.1", "53118", true},
		{"localhost:53118", "localhost", "53118", true},
		{"[::1]:53118", "::1", "53118", true},
		{"[::1]", "::1", "", true},
		{"example.com", "example.com", "", true},
		{"", "", "", false},
		{":53118", "", "", false},
		{"[::1", "", "", false},
		{"::1", "", "", false},   // an unbracketed literal is not a valid Host
		{"a:b:c", "", "", false}, // neither is this
		{"127.0.0.1:53118:80", "", "", false},
	}
	for _, tc := range cases {
		host, port, ok := splitHostPort(tc.value)
		if ok != tc.ok || (ok && (host != tc.host || port != tc.port)) {
			t.Errorf("splitHostPort(%q) = %q, %q, %v; want %q, %q, %v",
				tc.value, host, port, ok, tc.host, tc.port, tc.ok)
		}
	}

	// A Host without a port means the scheme's default, which Tabularium 117 only
	// ever serves if it really did bind port 80.
	if !portMatches("", 80) || portMatches("", 53118) {
		t.Error("an absent port is not read as 80")
	}
	if !portMatches("53118", 53118) || portMatches("53119", 53118) || portMatches("0053118", 53118) {
		t.Error("portMatches accepted the wrong port")
	}
}
