package database

import "time"

// Clock provides the current time and timezone. Inject a fixed implementation
// in tests to make time-dependent queries deterministic.
type Clock interface {
	Now() time.Time
	Location() *time.Location
	EndOfToday() time.Time
}

type realClock struct{ loc *time.Location }

func (c realClock) Now() time.Time         { return time.Now() }
func (c realClock) Location() *time.Location { return c.loc }
func (c realClock) EndOfToday() time.Time {
	now := c.Now().In(c.loc)
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, c.loc)
}

type fixedClock struct {
	t   time.Time
	loc *time.Location
}

func (c fixedClock) Now() time.Time         { return c.t }
func (c fixedClock) Location() *time.Location { return c.loc }
func (c fixedClock) EndOfToday() time.Time {
	now := c.t.In(c.loc)
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, c.loc)
}

// FixedClock returns a Clock that always returns t in loc. Use in tests to pin time and timezone.
func FixedClock(t time.Time, loc *time.Location) Clock { return fixedClock{t: t, loc: loc} }
