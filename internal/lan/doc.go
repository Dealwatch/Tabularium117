// Package lan holds the two decisions that make LAN mode safe to switch on:
// which address Tabularium 117 may listen on, and which client may talk to it
// (KONZEPT.md section 6).
//
// It is deliberately a package of its own and free of HTTP: the address
// selection and the RFC 1918 test are pure functions over an interface list,
// so they can be unit-tested without a network, and internal/server can be
// handed a synthetic list instead of this machine's interfaces.
//
// The rules it encodes:
//
//   - Only private IPv4 addresses (RFC 1918: 10/8, 172.16/12, 192.168/16)
//     are candidates, never 0.0.0.0, never a public address and never a
//     link-local one (169.254/16, which is what a Windows box has when DHCP
//     failed - a network nobody is reachable on anyway).
//   - Home networks come first: 192.168/16 beats 10/8 beats 172.16/12.
//   - The access token is 128 bits from crypto/rand, compared in constant
//     time, and is never part of a log line.
package lan
