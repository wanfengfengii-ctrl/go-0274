package store

import (
	// Register the pure-Go SQLite driver under the name "sqlite". It compiles
	// without cgo, so the same binary builds for linux/amd64 and linux/arm64.
	_ "modernc.org/sqlite"
)
