package domain

import (
	"testing"
	"time"
)

func TestFlightDurationAndLoadFactor(t *testing.T) {
	dep := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	f := Flight{
		DepartsAt: dep, ArrivesAt: dep.Add(150 * time.Minute), Capacity: 20,
		Availability: &Availability{SeatsTaken: 18},
	}
	if got := f.Duration(); got != 150*time.Minute {
		t.Errorf("duration %v", got)
	}
	if got := f.LoadFactor(); got != 0.9 {
		t.Errorf("load factor %v, want 0.9", got)
	}

	// A flight queried without the availability join must not divide by a
	// value that is not there.
	f.Availability = nil
	if got := f.LoadFactor(); got != 0 {
		t.Errorf("load factor with no availability = %v, want 0", got)
	}
	f.Availability = &Availability{SeatsTaken: 5}
	f.Capacity = 0
	if got := f.LoadFactor(); got != 0 {
		t.Errorf("load factor with zero capacity = %v, want 0", got)
	}
}

func TestAvailabilityAvailableIn(t *testing.T) {
	a := Availability{BusinessAvailable: 2, EconomyAvailable: 7}
	if got := a.AvailableIn(CabinBusiness); got != 2 {
		t.Errorf("business = %d", got)
	}
	if got := a.AvailableIn(CabinEconomy); got != 7 {
		t.Errorf("economy = %d", got)
	}
	// Anything unrecognised is treated as economy, the larger cabin.
	if got := a.AvailableIn(Cabin("first")); got != 7 {
		t.Errorf("unknown cabin = %d, want the economy count", got)
	}
}

func TestEnumValidation(t *testing.T) {
	for _, c := range []struct {
		name  string
		valid bool
		got   bool
	}{
		{"tier none", true, TierNone.Valid()},
		{"tier platinum", true, TierPlatinum.Valid()},
		{"tier bogus", false, LoyaltyTier("diamond").Valid()},
		{"tier empty", false, LoyaltyTier("").Valid()},

		{"cabin economy", true, CabinEconomy.Valid()},
		{"cabin business", true, CabinBusiness.Valid()},
		{"cabin first", false, Cabin("first").Valid()},

		{"flight scheduled", true, FlightScheduled.Valid()},
		{"flight cancelled", true, FlightCancelled.Valid()},
		{"flight departed", true, FlightDeparted.Valid()},
		{"flight delayed", false, FlightStatus("delayed").Valid()},

		{"booking confirmed", true, BookingConfirmed.Valid()},
		{"booking reaccommodated", true, BookingReaccommodated.Valid()},
		{"booking pending", false, BookingStatus("pending").Valid()},
	} {
		if c.got != c.valid {
			t.Errorf("%s: Valid() = %v, want %v", c.name, c.got, c.valid)
		}
	}
}
