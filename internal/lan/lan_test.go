package lan_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/Dealwatch/Tabularium117/internal/lan"
)

// addrs is a small helper so the synthetic interface lists stay readable.
func addrs(list ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(list))
	for _, s := range list {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
}

// The RFC 1918 test is the whole client filter, so its edges matter more
// than its middle: 172.16/12 ends at 172.31.255.255, and everything outside
// the three blocks is refused no matter how "internal" it looks.
func TestIsPrivate(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.5", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.20.1.1", true},
		{"172.31.255.254", true},
		{"192.168.1.10", true},
		{"::ffff:192.168.1.10", true}, // a v4 client on a dual-stack listener
		{"172.15.255.255", false},
		{"172.32.0.1", false},
		{"8.8.8.8", false},
		{"127.0.0.1", false},   // loopback is served by the other listener
		{"169.254.1.1", false}, // link-local: DHCP failed, nobody is there
		{"0.0.0.0", false},
		{"255.255.255.255", false},
		{"fd00::1", false}, // unique local IPv6 is not RFC 1918
		{"::1", false},
		{"2001:db8::1", false},
	}
	for _, tc := range cases {
		if got := lan.IsPrivate(netip.MustParseAddr(tc.ip)); got != tc.want {
			t.Errorf("IsPrivate(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

// Home networks first: 192.168/16, then 10/8, then 172.16/12. Anything that
// is not a private IPv4 address of a live, non-loopback interface is not a
// candidate at all.
func TestCandidatePreferenceOrder(t *testing.T) {
	ifaces := []lan.Interface{
		{Name: "docker0", Up: true, Addrs: addrs("172.17.0.1")},
		{Name: "eth0", Up: true, Addrs: addrs("10.1.2.3", "fe80::1")},
		{Name: "lo", Up: true, Loopback: true, Addrs: addrs("127.0.0.1")},
		{Name: "wlan0", Up: true, Addrs: addrs("192.168.178.42")},
		{Name: "vpn0", Up: false, Addrs: addrs("192.168.9.9")},
		{Name: "wan0", Up: true, Addrs: addrs("93.184.216.34", "169.254.7.7")},
	}

	got := lan.Candidates(ifaces)
	want := []string{"192.168.178.42", "10.1.2.3", "172.17.0.1"}
	if len(got) != len(want) {
		t.Fatalf("Candidates() = %v, want %v", got, want)
	}
	for i, ip := range want {
		if got[i].IP.String() != ip {
			t.Errorf("candidate %d = %s, want %s", i, got[i].IP, ip)
		}
	}
	if got[0].Interface != "wlan0" {
		t.Errorf("the best candidate reports interface %q, want wlan0", got[0].Interface)
	}

	pick, ok := lan.Pick(ifaces)
	if !ok || pick.IP.String() != "192.168.178.42" {
		t.Errorf("Pick() = %v, %v; want the 192.168 address", pick, ok)
	}
	if !strings.Contains(pick.String(), "wlan0") {
		t.Errorf("Pick().String() = %q, want the interface name in it", pick.String())
	}
}

// Two equally good addresses must not make the chosen one depend on the
// order the operating system happened to list the interfaces in.
func TestCandidateOrderIsStable(t *testing.T) {
	a := []lan.Interface{
		{Name: "eth1", Up: true, Addrs: addrs("192.168.2.2")},
		{Name: "eth0", Up: true, Addrs: addrs("192.168.1.1")},
	}
	b := []lan.Interface{
		{Name: "eth0", Up: true, Addrs: addrs("192.168.1.1")},
		{Name: "eth1", Up: true, Addrs: addrs("192.168.2.2")},
	}
	first, _ := lan.Pick(a)
	second, _ := lan.Pick(b)
	if first != second {
		t.Errorf("Pick depends on the list order: %v vs %v", first, second)
	}
	if first.Interface != "eth0" {
		t.Errorf("Pick() = %v, want the lower interface name", first)
	}
}

// A machine with nothing but a loopback and a public address cannot host LAN
// mode, and saying so is the point: the UI turns it into a reason.
func TestNoCandidate(t *testing.T) {
	ifaces := []lan.Interface{
		{Name: "lo", Up: true, Loopback: true, Addrs: addrs("127.0.0.1")},
		{Name: "eth0", Up: true, Addrs: addrs("93.184.216.34")},
	}
	if addr, ok := lan.Pick(ifaces); ok {
		t.Errorf("Pick() = %v, want no candidate", addr)
	}
	if got := lan.Candidates(nil); got != nil {
		t.Errorf("Candidates(nil) = %v, want nothing", got)
	}
}

// --lan-ip must name a private address that this PC really has. Every
// rejection names what is wrong, because the user typed it.
func TestResolve(t *testing.T) {
	ifaces := []lan.Interface{
		{Name: "lo", Up: true, Loopback: true, Addrs: addrs("127.0.0.1")},
		{Name: "wlan0", Up: true, Addrs: addrs("192.168.178.42")},
		{Name: "eth0", Up: true, Addrs: addrs("10.1.2.3")},
		{Name: "vpn0", Up: false, Addrs: addrs("10.9.9.9")},
	}

	got, err := lan.Resolve("192.168.178.42", ifaces, false)
	if err != nil {
		t.Fatalf("Resolve of a configured address: %v", err)
	}
	if got.Interface != "wlan0" || got.IP.String() != "192.168.178.42" {
		t.Errorf("Resolve = %v, want the wlan0 address", got)
	}

	bad := []struct {
		ip   string
		want string
	}{
		{"not-an-ip", "not an IP address"},
		{"fd00::1", "IPv4"},
		{"8.8.8.8", "not a private address"},
		{"172.32.0.1", "not a private address"},
		{"0.0.0.0", "not a private address"},
		{"192.168.1.1", "not configured on any network interface"},
		{"10.9.9.9", "is down"},
		{"127.0.0.1", "loopback"},
	}
	for _, tc := range bad {
		_, err := lan.Resolve(tc.ip, ifaces, false)
		if err == nil {
			t.Errorf("Resolve(%q) was accepted", tc.ip)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Resolve(%q) = %v, want it to mention %q", tc.ip, err, tc.want)
		}
	}

	// The documented test aid, and only for the loopback.
	loop, err := lan.Resolve("127.0.0.2", ifaces, true)
	if err != nil {
		t.Fatalf("Resolve(127.0.0.2, allowLoopback) = %v", err)
	}
	if loop.IP.String() != "127.0.0.2" {
		t.Errorf("Resolve = %v, want 127.0.0.2", loop)
	}
	if _, err := lan.Resolve("8.8.8.8", ifaces, true); err == nil {
		t.Error("allowLoopback must not accept a public address")
	}
}

// The token is a fresh 128-bit secret per call, URL-safe and unpadded, and
// it is compared as a whole.
func TestToken(t *testing.T) {
	first, second := lan.NewToken(), lan.NewToken()
	if first == second {
		t.Fatal("two tokens are identical; they are not random")
	}
	// 128 bit in base64 without padding is 22 characters.
	if len(first) != 22 {
		t.Errorf("token %q is %d characters, want 22", first, len(first))
	}
	if strings.ContainsAny(first, "+/=") {
		t.Errorf("token %q is not URL-safe", first)
	}
	if !lan.TokenEqual(first, first) {
		t.Error("a token does not equal itself")
	}
	if lan.TokenEqual(first, second) || lan.TokenEqual("", first) || lan.TokenEqual(first, "") {
		t.Error("TokenEqual accepted a wrong token")
	}
	if lan.TokenEqual(first[:len(first)-1], first) {
		t.Error("TokenEqual accepted a prefix of the token")
	}
}

// SystemInterfaces must not invent candidates: whatever it reports has to
// survive the same filter the synthetic lists go through.
func TestSystemInterfaces(t *testing.T) {
	ifaces, err := lan.SystemInterfaces()
	if err != nil {
		t.Fatalf("SystemInterfaces: %v", err)
	}
	if len(ifaces) == 0 {
		t.Skip("this machine reports no network interfaces")
	}
	for _, c := range lan.Candidates(ifaces) {
		if !lan.IsPrivate(c.IP) {
			t.Errorf("candidate %v is not a private address", c)
		}
	}
}
