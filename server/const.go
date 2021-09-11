package server

import "time"

const (
	VERSION = "1.0.0"

	DEFAULT_PORT = 6430

	DEFAULT_HOST = "0.0.0.0"

	// ACCEPT_MIN_SLEEP id minimum acceptable sleep times on temporary errors.
	ACCEPT_MIN_SLEEP = 10 * time.Millisecond
	// ACCEPT_MAX_SLEEP id maximum acceptable sleep times on temporary errors.
	ACCEPT_MAX_SLEEP = 1 * time.Second
)
