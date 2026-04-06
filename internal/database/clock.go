package database

import "time"

// Clock provides the current time. Inject a fixed implementation in tests
// to make time-dependent queries deterministic.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
