package idgen

import "github.com/google/uuid"

// New returns a new UUIDv4 string.
func New() string {
	return uuid.NewString()
}

// MustParse parses a UUID string and panics on failure.
// Intended for test fixtures only.
func MustParse(s string) uuid.UUID {
	return uuid.MustParse(s)
}
