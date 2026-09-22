package lan

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

// tokenBytes is the token's entropy: 128 bit, as KONZEPT.md section 6
// specifies. Base64url turns it into 22 URL-safe characters, which is short
// enough to be typed off a phone screen if the QR code fails.
const tokenBytes = 16

// NewToken returns a fresh access token: 128 random bits, base64url encoded
// without padding.
//
// One token per process start (KONZEPT.md section 6). Restarting Tabularium 117
// therefore invalidates every phone that was let in before, which is the
// cheapest possible revocation and needs no storage at all.
func NewToken() string {
	var buf [tokenBytes]byte
	// crypto/rand.Read never returns an error; it panics if the system's
	// randomness is unavailable, which is the correct outcome here too: a
	// predictable token would be worse than no LAN mode.
	rand.Read(buf[:])
	return base64.RawURLEncoding.EncodeToString(buf[:])
}

// TokenEqual reports whether presented matches want, in constant time so
// that a wrong token cannot be guessed character by character from the
// server's response time.
func TokenEqual(presented, want string) bool {
	// ConstantTimeCompare returns 0 for differing lengths without looking at
	// the contents, so the length is compared first to keep the meaning
	// obvious rather than to save work.
	if len(presented) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(want)) == 1
}
