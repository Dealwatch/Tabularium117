package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Everything the command line can decide about LAN mode on its own: whether
// the address is one Tabularium 117 may bind at all, and that the loopback test aid
// cannot be turned into a way of binding something else.
func TestLANFlagValidation(t *testing.T) {
	good := [][]string{
		{"--lan"},
		{"--lan", "--lan-ip", "192.168.1.10"},
		{"--lan-ip", "10.0.0.5"},
		{"--lan-ip", "172.20.1.1"},
		{"--lan", "--lan-ip", "127.0.0.2", "--lan-allow-loopback"},
	}
	for _, args := range good {
		cfg := parse(t, args...)
		if len(args) > 1 && cfg.lanIP == "" {
			t.Errorf("parseFlags(%v) lost the address: %+v", args, cfg)
		}
	}

	bad := []struct {
		args []string
		want string
	}{
		{[]string{"--lan-ip", "nonsense"}, "not an IP address"},
		{[]string{"--lan-ip", "fd00::1"}, "IPv4"},
		{[]string{"--lan-ip", "8.8.8.8"}, "not a private address"},
		{[]string{"--lan-ip", "172.32.0.1"}, "not a private address"},
		{[]string{"--lan-ip", "127.0.0.1"}, "loopback"},
		{[]string{"--lan-allow-loopback"}, "only works together with"},
		{[]string{"--lan-ip", "192.168.1.10", "--lan-allow-loopback"}, "test aid"},
	}
	for _, tc := range bad {
		_, err := parseFlags(tc.args, io.Discard)
		if err == nil {
			t.Errorf("parseFlags(%v) was accepted", tc.args)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseFlags(%v) = %v, want it to mention %q", tc.args, err, tc.want)
		}
	}
}

// --lan really opens the second listener, and that listener really asks for
// the token. The address is 127.0.0.2 (with the documented test aid), so the
// check needs no second machine and opens nothing to the network.
func TestLANFlagOpensTheSecondListener(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("this machine cannot bind 127.0.0.2: %v", err)
	}
	probe.Close()

	cfg := parse(t, "--replay", fixturePath, "--replay-speed", "0", "--data-dir", t.TempDir(),
		"--port", "0", "--no-browser", "--serve-after-replay",
		"--lan", "--lan-ip", "127.0.0.2", "--lan-allow-loopback")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, stdout, stderr) }()

	url := waitForURL(t, stderr)
	_, port, err := net.SplitHostPort(strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/"))
	if err != nil {
		t.Fatalf("cannot read the port out of %q: %v", url, err)
	}

	// The log says where LAN mode is, and never what the token is.
	log := stderr.String()
	if !strings.Contains(log, "LAN mode enabled on http://127.0.0.2:"+port+"/ (token in the UI)") {
		t.Fatalf("the log does not report LAN mode:\n%s", log)
	}
	if strings.Contains(log, "token=") {
		t.Error("the log contains a token")
	}

	// The LAN listener is open, and it wants the token.
	resp, err := http.Get("http://127.0.0.2:" + port + "/")
	if err != nil {
		t.Fatalf("GET the LAN address: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET the LAN address without a token = %d, want 401", resp.StatusCode)
	}
	if !strings.Contains(string(body), "QR") {
		t.Errorf("the 401 page does not explain itself:\n%s", body)
	}

	// And the PC's own UI is untouched by any of it.
	local, err := http.Get(url + "api/v1/status")
	if err != nil {
		t.Fatalf("GET the local status: %v", err)
	}
	local.Body.Close()
	if local.StatusCode != http.StatusOK {
		t.Errorf("the loopback listener = %d, want 200", local.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Ctrl+C must end the run cleanly, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not stop after the context was cancelled")
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.2:"+port, 2*time.Second); err == nil {
		conn.Close()
		t.Error("the LAN listener is still open after the run returned")
	}
}
