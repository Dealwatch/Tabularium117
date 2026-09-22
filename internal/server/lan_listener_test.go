package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/lan"
	"github.com/Dealwatch/Tabularium117/internal/server"
)

// lanJSON is GET/POST /api/v1/lan as the UI sees it.
type lanJSON struct {
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	IP        string `json:"ip"`
	Interface string `json:"interface"`
	Port      int    `json:"port"`
	URL       string `json:"url"`
	Reason    string `json:"reason"`
}

// lanFixture is a running server with both listeners available: the real
// loopback one, and a "LAN" one on 127.0.0.2, which is reachable from this
// machine only. That is the documented test aid (LANOptions.AllowLoopback):
// it exercises the whole LAN path - the second listener, the client filter,
// the token - without needing a second machine or this machine's own network.
type lanFixture struct {
	srv       *server.Server
	localURL  string
	lanURL    string
	port      int
	done      chan error
	cancel    context.CancelFunc
	lanClient *http.Client
	stopped   bool
}

// stop ends the server and waits for it, at most once: the cleanup runs it
// too, for the tests that do not stop it themselves.
func (f *lanFixture) stop(t *testing.T) {
	t.Helper()
	if f.stopped {
		return
	}
	f.stopped = true
	f.cancel()
	select {
	case err := <-f.done:
		if err != nil {
			t.Errorf("shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("the server did not shut down")
	}
}

func startLANFixture(t *testing.T) *lanFixture {
	t.Helper()
	fx := loadFixture(t, false)

	ready := make(chan net.Addr, 1)
	srv := server.New(server.Options{
		State:   fx.state,
		Version: "test",
		Now:     func() time.Time { return fx.now },
		OnReady: func(addr net.Addr) { ready <- addr },
		LAN: server.LANOptions{
			IP:            "127.0.0.2",
			AllowLoopback: true,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx, "127.0.0.1:0") }()

	var addr net.Addr
	select {
	case addr = <-ready:
	case err := <-done:
		cancel()
		t.Fatalf("the server stopped instead of binding: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("the server did not bind")
	}
	_, portText, _ := net.SplitHostPort(addr.String())
	port, _ := strconv.Atoi(portText)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	lf := &lanFixture{
		srv:      srv,
		localURL: "http://" + addr.String(),
		lanURL:   fmt.Sprintf("http://127.0.0.2:%d", port),
		port:     port,
		done:     done,
		cancel:   cancel,
		lanClient: &http.Client{
			Jar: jar,
			// Following the 303 by hand is the point: the test wants to see
			// it, and the cookie that comes with it.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Timeout:       10 * time.Second,
		},
	}
	t.Cleanup(func() { lf.stop(t) })
	return lf
}

// toggle posts to the LAN switch on the loopback listener, as the UI does.
func (f *lanFixture) toggle(t *testing.T, body string, header http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.localURL+"/api/v1/lan", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", f.localURL)
	for k, values := range header {
		req.Header.Del(k)
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST the LAN switch: %v", err)
	}
	return resp
}

// lanState reads the LAN state from the loopback listener.
func (f *lanFixture) lanState(t *testing.T) lanJSON {
	t.Helper()
	var got lanJSON
	getJSON(t, f.localURL+"/api/v1/lan", http.StatusOK, &got)
	return got
}

// The whole switch, end to end: off, on, a phone that gets in with the token,
// off again - and the listener really is gone afterwards.
func TestLANToggleAndTokenFlow(t *testing.T) {
	f := startLANFixture(t)

	// --- off, but available ---
	before := f.lanState(t)
	if before.Enabled {
		t.Fatal("LAN mode is on before anything switched it on")
	}
	if !before.Available || before.IP != "127.0.0.2" || before.Port != f.port {
		t.Fatalf("state = %+v, want the test address available on the serving port", before)
	}
	if before.URL != "" {
		t.Errorf("url = %q before LAN mode is on", before.URL)
	}
	// Nothing is listening on the LAN address yet.
	if conn, err := net.DialTimeout("tcp", "127.0.0.2:"+strconv.Itoa(f.port), 2*time.Second); err == nil {
		conn.Close()
		t.Fatal("something is already listening on the LAN address")
	}

	// --- the malformed attempts change nothing ---
	bad := []struct {
		name   string
		body   string
		header http.Header
		want   int
	}{
		{"not json", `enabled=true`, http.Header{"Content-Type": {"text/plain"}}, http.StatusUnsupportedMediaType},
		{"no content type", `{"enabled":true}`, http.Header{"Content-Type": {""}}, http.StatusUnsupportedMediaType},
		{"foreign origin", `{"enabled":true}`, http.Header{"Origin": {"http://evil.example"}}, http.StatusForbidden},
		{"broken body", `{"enabled":`, nil, http.StatusBadRequest},
		{"no field", `{}`, nil, http.StatusBadRequest},
	}
	for _, tc := range bad {
		resp := f.toggle(t, tc.body, tc.header)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
	if f.lanState(t).Enabled {
		t.Fatal("a refused request switched LAN mode on")
	}

	// --- on ---
	resp := f.toggle(t, `{"enabled":true}`, nil)
	var enabled lanJSON
	decode(t, resp, &enabled)
	resp.Body.Close()
	if !enabled.Enabled || enabled.IP != "127.0.0.2" {
		t.Fatalf("state after enabling = %+v", enabled)
	}
	token := tokenOf(t, enabled.URL)
	if !strings.HasPrefix(enabled.URL, f.lanURL+"/?token=") {
		t.Fatalf("url = %q, want the LAN address with the token", enabled.URL)
	}

	var status statusJSON
	getJSON(t, f.localURL+"/api/v1/status", http.StatusOK, &status)
	if !status.LAN.Enabled || !status.LAN.Local {
		t.Errorf("status.lan = %+v on the loopback listener, want enabled and local", status.LAN)
	}

	// --- the QR code ---
	qr := get(t, f.localURL+"/api/v1/lan/qr.png", nil)
	defer qr.Body.Close()
	if qr.StatusCode != http.StatusOK {
		t.Fatalf("GET the QR code = %d", qr.StatusCode)
	}
	if ct := qr.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("QR content type = %q", ct)
	}
	if cc := qr.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("QR cache control = %q, want no-store: the image carries the token", cc)
	}
	img, format, err := image.Decode(qr.Body)
	if err != nil {
		t.Fatalf("the QR code is not an image: %v", err)
	}
	if format != "png" {
		t.Errorf("QR format = %q, want png", format)
	}
	if b := img.Bounds(); b.Dx() < 256 || b.Dx() != b.Dy() {
		t.Errorf("QR image is %dx%d, want a square of at least 256 px", b.Dx(), b.Dy())
	}

	// --- a phone, without the token ---
	page, err := f.lanClient.Get(f.lanURL + "/")
	if err != nil {
		t.Fatalf("GET the LAN address: %v", err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if page.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET the LAN address without a token = %d, want 401", page.StatusCode)
	}
	if !strings.Contains(string(body), "QR") || strings.Contains(string(body), token) {
		t.Errorf("the 401 page is wrong:\n%s", body)
	}
	if ref := page.Header.Get("Referrer-Policy"); ref != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", ref)
	}
	apiDenied, err := f.lanClient.Get(f.lanURL + "/api/v1/islands")
	if err != nil {
		t.Fatal(err)
	}
	apiDenied.Body.Close()
	if apiDenied.StatusCode != http.StatusUnauthorized {
		t.Errorf("the API answered %d without a token, want 401", apiDenied.StatusCode)
	}

	// --- the phone scans the code ---
	redirect, err := f.lanClient.Get(enabled.URL)
	if err != nil {
		t.Fatalf("GET the token URL: %v", err)
	}
	redirect.Body.Close()
	if redirect.StatusCode != http.StatusSeeOther {
		t.Fatalf("the token URL = %d, want 303", redirect.StatusCode)
	}
	if loc := redirect.Header.Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want the token to be gone from the URL", loc)
	}

	// The jar now has the cookie, and the site works.
	ui, err := f.lanClient.Get(f.lanURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	uiBody, _ := io.ReadAll(ui.Body)
	ui.Body.Close()
	if ui.StatusCode != http.StatusOK || !strings.Contains(string(uiBody), "Tabularium 117") {
		t.Fatalf("the UI on the LAN listener = %d", ui.StatusCode)
	}

	var islands []islandJSON
	getWithClient(t, f.lanClient, f.lanURL+"/api/v1/islands", &islands)
	if len(islands) != 14 {
		t.Errorf("the phone sees %d islands, want the capture's 14", len(islands))
	}

	var lanStatus statusJSON
	getWithClient(t, f.lanClient, f.lanURL+"/api/v1/status", &lanStatus)
	if !lanStatus.LAN.Enabled || lanStatus.LAN.Local {
		t.Errorf("status.lan = %+v on the LAN listener, want enabled and not local", lanStatus.LAN)
	}
	var phoneView lanJSON
	getWithClient(t, f.lanClient, f.lanURL+"/api/v1/lan", &phoneView)
	if phoneView.URL != "" {
		t.Errorf("the phone was told the token URL: %q", phoneView.URL)
	}

	// The event stream is covered by the same cookie: EventSource sends it.
	if name := firstEventName(t, f.lanClient, f.lanURL+"/api/v1/events"); name != "status" {
		t.Errorf("the first event on the LAN stream = %q, want status", name)
	}

	// --- and the phone cannot open anything further ---
	toggleFromLAN, err := f.lanClient.Post(f.lanURL+"/api/v1/lan", "application/json",
		strings.NewReader(`{"enabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	toggleFromLAN.Body.Close()
	if toggleFromLAN.StatusCode != http.StatusForbidden {
		t.Errorf("POST the switch from the LAN = %d, want 403", toggleFromLAN.StatusCode)
	}
	qrFromLAN, err := f.lanClient.Get(f.lanURL + "/api/v1/lan/qr.png")
	if err != nil {
		t.Fatal(err)
	}
	qrFromLAN.Body.Close()
	if qrFromLAN.StatusCode != http.StatusNotFound {
		t.Errorf("GET the QR code from the LAN = %d, want 404", qrFromLAN.StatusCode)
	}

	// --- off again ---
	off := f.toggle(t, `{"enabled":false}`, nil)
	var offState lanJSON
	decode(t, off, &offState)
	off.Body.Close()
	if offState.Enabled {
		t.Fatal("the switch reported LAN mode as still on")
	}
	// The listener is really gone, not merely reported as gone.
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", "127.0.0.2:"+strconv.Itoa(f.port), 2*time.Second)
		if err != nil {
			break
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the LAN listener is still accepting connections after being switched off")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if msg := errorOf(t, f.localURL+"/api/v1/lan/qr.png", http.StatusNotFound); !strings.Contains(msg, "LAN") {
		t.Errorf("the QR code error = %q, want it to name LAN mode", msg)
	}
	getJSON(t, f.localURL+"/api/v1/status", http.StatusOK, &status)
	if status.LAN.Enabled {
		t.Error("the status still reports LAN mode as on")
	}
}

// A port that is taken on the LAN address is a 409 with the reason, not a
// crash and not a silent "enabled".
func TestLANEnableWithBusyPort(t *testing.T) {
	f := startLANFixture(t)

	busy, err := net.Listen("tcp", "127.0.0.2:"+strconv.Itoa(f.port))
	if err != nil {
		t.Skipf("cannot occupy the LAN port on this machine: %v", err)
	}
	defer busy.Close()

	resp := f.toggle(t, `{"enabled":true}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("enabling onto a busy port = %d, want 409", resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Error, "127.0.0.2") {
		t.Errorf("error = %q, want it to name the address it could not bind", body.Error)
	}
	if f.lanState(t).Enabled {
		t.Error("a failed bind still reported LAN mode as on")
	}
}

// Shutting the server down closes both listeners, not just the loopback one.
func TestShutdownClosesBothListeners(t *testing.T) {
	f := startLANFixture(t)

	resp := f.toggle(t, `{"enabled":true}`, nil)
	resp.Body.Close()
	if !f.srv.LANEnabled() {
		t.Fatal("LAN mode did not come up")
	}

	f.stop(t)

	for _, addr := range []string{"127.0.0.1:" + strconv.Itoa(f.port), "127.0.0.2:" + strconv.Itoa(f.port)} {
		if conn, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
			conn.Close()
			t.Errorf("%s is still accepting connections after the shutdown", addr)
		}
	}
}

// An explicit --lan-ip that this PC does not have must fail with a reason,
// not fall back to some other address.
func TestLANIPMustExist(t *testing.T) {
	srv := server.New(server.Options{
		State: loadFixture(t, false).state,
		LAN: server.LANOptions{
			IP: "192.168.222.1",
			Interfaces: func() ([]lan.Interface, error) {
				return []lan.Interface{
					{Name: "eth0", Up: true, Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.5")}},
				}, nil
			},
		},
	})
	err := srv.EnableLAN()
	if err == nil {
		t.Fatal("an address this PC does not have was accepted")
	}
	if !strings.Contains(err.Error(), "192.168.222.1") {
		t.Errorf("error = %v, want it to name the address", err)
	}
	if srv.LANEnabled() {
		t.Error("LAN mode is on although enabling failed")
	}
}

// tokenOf pulls the token out of the URL the loopback listener reports.
func tokenOf(t *testing.T, rawURL string) string {
	t.Helper()
	_, query, ok := strings.Cut(rawURL, "?token=")
	if !ok || query == "" {
		t.Fatalf("url %q carries no token", rawURL)
	}
	return query
}

// getWithClient performs a GET with a client that carries the token cookie.
func getWithClient(t *testing.T, client *http.Client, url string, out any) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

// firstEventName opens an event stream and returns the name of the first
// event on it, then closes the stream.
func firstEventName(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open the stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read the stream: %v", err)
		}
		if name, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "event: "); ok {
			return name
		}
	}
}

// hostRequest dials target but asks for host, the way a page that rebound its
// own name to 127.0.0.1 would: the connection goes to this server, the name
// in the request is somebody else's.
func hostRequest(t *testing.T, client *http.Client, target, host, path string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s with Host %q: %v", path, host, err)
	}
	return resp
}

// DNS rebinding is the standing attack on a service that listens on
// localhost: a page whose name resolves to 127.0.0.1 is same-origin with
// Tabularium 117 as far as the browser is concerned, and could read the LAN
// address with its token. The Host header is what still tells the two apart.
func TestForeignHostIsRefused(t *testing.T) {
	f := startLANFixture(t)
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	port := strconv.Itoa(f.port)

	// Switch LAN mode on, so the things worth stealing exist.
	f.toggle(t, `{"enabled":true}`, nil).Body.Close()

	paths := []string{"/api/v1/lan", "/api/v1/lan/qr.png", "/api/v1/islands", "/api/v1/events",
		"/api/v1/status", "/", "/js/app.js"}
	for _, path := range paths {
		for _, host := range []string{"evil.example:" + port, "evil.example", "tabularium117.local:" + port,
			"127.0.0.1", "127.0.0.1:1", "localhost:1", "127.0.0.1:" + port + ":80"} {
			resp := hostRequest(t, client, f.localURL, host, path)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusMisdirectedRequest {
				t.Errorf("GET %s with Host %q = %d, want 421", path, host, resp.StatusCode)
			}
			if strings.Contains(string(body), "token") {
				t.Errorf("the 421 for Host %q leaked something:\n%s", host, body)
			}
		}
	}

	// A request with no Host at all - only an HTTP/1.0 client can send one,
	// Go's own client always fills it in - is refused too.
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 5*time.Second)
	if err != nil {
		t.Fatalf("dial the loopback listener: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, "GET /api/v1/lan HTTP/1.0\r\n\r\n"); err != nil {
		t.Fatalf("write a request without a Host: %v", err)
	}
	raw, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read the answer: %v", err)
	}
	if !strings.HasPrefix(string(raw), "HTTP/1.0 421") {
		t.Errorf("a request without a Host header was answered with:\n%s", raw)
	}
	if strings.Contains(string(raw), "token") {
		t.Errorf("a request without a Host header leaked something:\n%s", raw)
	}

	// And the names this server really answers to still work.
	for _, host := range []string{"127.0.0.1:" + port, "localhost:" + port, "LOCALHOST:" + port,
		"[::1]:" + port} {
		resp := hostRequest(t, client, f.localURL, host, "/api/v1/status")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /api/v1/status with Host %q = %d, want 200", host, resp.StatusCode)
		}
	}

	// The LAN listener answers to its own address and to nothing else - not
	// even to the loopback name, which belongs to the other listener.
	refused := hostRequest(t, client, f.lanURL, "127.0.0.1:"+port, "/")
	refused.Body.Close()
	if refused.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("the LAN listener answered to the loopback name: %d", refused.StatusCode)
	}
	// Its own name gets past the host check and lands on the token check,
	// which is the next thing in the chain.
	allowed := hostRequest(t, client, f.lanURL, "127.0.0.2:"+port, "/")
	allowed.Body.Close()
	if allowed.StatusCode != http.StatusUnauthorized {
		t.Errorf("the LAN listener with its own name = %d, want the token check's 401",
			allowed.StatusCode)
	}

	// A refused host is refused before anything else happens: no cookie, no
	// redirect, whatever the token says.
	state := f.lanState(t)
	token := tokenOf(t, state.URL)
	sneaky := hostRequest(t, client, f.lanURL, "127.0.0.1:"+port, "/?token="+token)
	sneaky.Body.Close()
	if sneaky.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("a token under a foreign host = %d, want 421", sneaky.StatusCode)
	}
	if len(sneaky.Cookies()) != 0 {
		t.Error("a request with a refused host was answered with a cookie")
	}
}
