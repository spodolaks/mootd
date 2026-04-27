package admin

import (
	"crypto/rand"
	"encoding/hex"
)

// randomHex returns 2*n hex characters of crypto-randomness. Used for
// audit IDs; small enough that we don't take a dep on the shared/id
// package (which is for user-facing IDs and pulls extra structure).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is unrecoverable — panic is correct.
		panic("admin: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
