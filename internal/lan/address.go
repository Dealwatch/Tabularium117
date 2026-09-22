package lan

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
)

// privateRanges are the three RFC 1918 blocks, in the order a home user is
// most likely to be on: a consumer router hands out 192.168.x.x, a bigger or
// managed network 10.x.x.x, and 172.16/12 is the rarest of the three. The
// order is the preference order of Pick.
var privateRanges = []netip.Prefix{
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
}

// Interface is one network interface, reduced to what the choice depends on.
// It exists so that the selection can be tested against a synthetic list
// instead of the machine the test happens to run on.
type Interface struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []netip.Addr
}

// Address is one address Tabularium 117 could listen on, and the interface it
// belongs to (which is what the user recognises: "Wi-Fi", "Ethernet").
type Address struct {
	Interface string
	IP        netip.Addr
}

// String renders the address for a log line or the UI.
func (a Address) String() string {
	if a.Interface == "" {
		return a.IP.String()
	}
	return fmt.Sprintf("%s (%s)", a.IP, a.Interface)
}

// IsPrivate reports whether ip is an RFC 1918 IPv4 address, which is the
// only kind of address LAN mode binds and the only kind of client it serves
// (KONZEPT.md section 6).
//
// An IPv4-mapped IPv6 address ("::ffff:10.0.0.5") is unmapped first: that is
// how a v4 client appears on a dual-stack listener, and refusing it would be
// a bug, while accepting the v6 form of a public address would be a hole.
func IsPrivate(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.Is4() {
		return false
	}
	for _, prefix := range privateRanges {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// rank is the preference order of Pick: lower is better. It is only defined
// for addresses IsPrivate accepts.
func rank(ip netip.Addr) int {
	ip = ip.Unmap()
	for i, prefix := range privateRanges {
		if prefix.Contains(ip) {
			return i
		}
	}
	return len(privateRanges)
}

// Candidates returns every private IPv4 address of an interface that is up
// and not the loopback, best first.
//
// Everything else is left out on purpose: a down interface cannot be bound,
// the loopback is already served, and a public or link-local address would
// either expose Tabularium 117 beyond the household or point at a network nobody
// can reach it on.
func Candidates(ifaces []Interface) []Address {
	var out []Address
	for _, iface := range ifaces {
		if !iface.Up || iface.Loopback {
			continue
		}
		for _, ip := range iface.Addrs {
			if IsPrivate(ip) {
				out = append(out, Address{Interface: iface.Name, IP: ip.Unmap()})
			}
		}
	}
	// Range first, then interface name and address, so that a machine with
	// two equally good addresses picks the same one on every start.
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i].IP), rank(out[j].IP); ri != rj {
			return ri < rj
		}
		if out[i].Interface != out[j].Interface {
			return out[i].Interface < out[j].Interface
		}
		return out[i].IP.Less(out[j].IP)
	})
	return out
}

// Pick returns the address LAN mode should bind, or false when the machine
// has none. It is Candidates' first entry.
func Pick(ifaces []Interface) (Address, bool) {
	candidates := Candidates(ifaces)
	if len(candidates) == 0 {
		return Address{}, false
	}
	return candidates[0], true
}

// Resolve validates an address the user asked for with --lan-ip against the
// machine's interfaces and returns it with the interface it was found on.
//
// allowLoopback is the test aid described in README.md: it accepts a
// 127.0.0.0/8 address, which is reachable from this machine only, so that the
// LAN code path can be exercised end to end without a second machine. It
// never widens anything beyond the local host.
func Resolve(want string, ifaces []Interface, allowLoopback bool) (Address, error) {
	ip, err := netip.ParseAddr(want)
	if err != nil {
		return Address{}, fmt.Errorf("%q is not an IP address", want)
	}
	ip = ip.Unmap()
	if !ip.Is4() {
		return Address{}, fmt.Errorf("%s is not an IPv4 address; Tabularium 117 binds IPv4 only", ip)
	}
	if ip.IsLoopback() {
		if !allowLoopback {
			return Address{}, fmt.Errorf("%s is the loopback, which is already served; "+
				"choose the address of the network you want to share with", ip)
		}
		// The loopback interface carries 127.0.0.1/8, so every 127.x address
		// is bindable without being listed; there is nothing to look up.
		return Address{Interface: "loopback", IP: ip}, nil
	}
	if !IsPrivate(ip) {
		return Address{}, fmt.Errorf("%s is not a private address; Tabularium 117 only binds "+
			"10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16", ip)
	}
	for _, iface := range ifaces {
		for _, have := range iface.Addrs {
			if have.Unmap() != ip {
				continue
			}
			if !iface.Up {
				return Address{}, fmt.Errorf("%s belongs to %s, which is down", ip, iface.Name)
			}
			return Address{Interface: iface.Name, IP: ip}, nil
		}
	}
	return Address{}, fmt.Errorf("%s is not configured on any network interface of this PC", ip)
}

// SystemInterfaces reports this machine's interfaces in the reduced form the
// selection works on. It is the only part of the package that touches the
// operating system.
func SystemInterfaces() ([]Interface, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("lan: list the network interfaces: %w", err)
	}
	out := make([]Interface, 0, len(list))
	for _, iface := range list {
		reduced := Interface{
			Name: iface.Name,
			// FlagRunning means "carrier present"; FlagUp alone is true for a
			// configured but unplugged interface, whose address cannot carry
			// traffic. Both are required.
			Up:       iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagRunning != 0,
			Loopback: iface.Flags&net.FlagLoopback != 0,
		}
		addrs, err := iface.Addrs()
		if err != nil {
			// One interface that refuses to describe itself must not hide
			// the others; it simply contributes no candidate.
			continue
		}
		for _, addr := range addrs {
			prefix, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip, ok := netip.AddrFromSlice(prefix.IP); ok {
				reduced.Addrs = append(reduced.Addrs, ip.Unmap())
			}
		}
		out = append(out, reduced)
	}
	return out, nil
}
