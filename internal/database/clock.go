package database

import "time"

// Clock provides the current time. Inject a fixed implementation in tests
// to make time-dependent queries deterministic.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// FixedClock returns a Clock that always returns t. Use in tests to pin time.
func FixedClock(t time.Time) Clock { return fixedClock{t: t} }
