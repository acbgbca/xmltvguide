package database

import (
	"testing"
	"time"
)

func TestRealClock_Now(t *testing.T) {
	c := realClock{loc: time.UTC}
	now := c.Now()
	if now.IsZero() {
		t.Error("expected non-zero time from realClock.Now()")
	}
}

func TestRealClock_Location(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Melbourne")
	c := realClock{loc: loc}
	if c.Location() != loc {
		t.Errorf("expected location %v, got %v", loc, c.Location())
	}
}

func TestRealClock_EndOfToday(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Melbourne")
	c := realClock{loc: loc}
	result := c.EndOfToday()

	now := time.Now().In(loc)
	expected := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)

	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
	if result.Location() != loc {
		t.Errorf("expected location %v, got %v", loc, result.Location())
	}
}

func TestFixedClock_Now(t *testing.T) {
	pinned := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)
	c := fixedClock{t: pinned, loc: time.UTC}
	if !c.Now().Equal(pinned) {
		t.Errorf("expected %v, got %v", pinned, c.Now())
	}
}

func TestFixedClock_Location(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Melbourne")
	c := fixedClock{t: time.Now(), loc: loc}
	if c.Location() != loc {
		t.Errorf("expected location %v, got %v", loc, c.Location())
	}
}

func TestFixedClock_EndOfToday(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Melbourne")
	// 2025-06-10T14:00:00 UTC = 2025-06-11T00:00:00 Melbourne (UTC+10)
	pinned := time.Date(2025, 6, 10, 14, 0, 0, 0, time.UTC)
	c := fixedClock{t: pinned, loc: loc}

	result := c.EndOfToday()

	// In Melbourne, pinned time is 2025-06-11 00:00:00, so EndOfToday = 2025-06-12 00:00:00 Melbourne
	pinnedInMelbourne := pinned.In(loc)
	expected := time.Date(pinnedInMelbourne.Year(), pinnedInMelbourne.Month(), pinnedInMelbourne.Day()+1, 0, 0, 0, 0, loc)

	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
	if result.Location() != loc {
		t.Errorf("expected result location %v, got %v", loc, result.Location())
	}
}

func TestFixedClock_EndOfToday_MidnightBoundary(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Melbourne")
	// 2025-06-10T08:00:00 UTC = 2025-06-10T18:00:00 Melbourne
	pinned := time.Date(2025, 6, 10, 8, 0, 0, 0, time.UTC)
	c := fixedClock{t: pinned, loc: loc}

	result := c.EndOfToday()

	// In Melbourne it's still June 10 at 18:00, so EndOfToday = 2025-06-11 00:00:00 Melbourne
	expected := time.Date(2025, 6, 11, 0, 0, 0, 0, loc)

	if !result.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}
