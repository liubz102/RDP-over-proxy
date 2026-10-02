package model

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a random identifier for a new proxy or profile: 16 lowercase
// hex digits. An ID is also the name of the entity's file on disk.
func NewID() string {
	var b [8]byte
	rand.Read(b[:]) // never fails since Go 1.24
	return hex.EncodeToString(b[:])
}

// ValidID reports whether id can be an identifier and a file name: 1 to 64
// lowercase ASCII letters and digits. Upper case is left out because Windows
// file names ignore case.
func ValidID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}
