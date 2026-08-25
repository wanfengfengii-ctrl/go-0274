package service

import (
	"crypto/rand"
	"encoding/hex"
)

// newTaskID returns a random, unique task identifier. It has no dependency on
// wall-clock time so task identity is stable across restarts and tests.
func newTaskID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on supported platforms; fall back defensively.
		return "task-fallback"
	}
	return "T" + hex.EncodeToString(b[:])
}
