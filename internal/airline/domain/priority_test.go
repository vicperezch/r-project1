package domain

import (
	"testing"
	"time"
)

func booking(pnr string, tier LoyaltyTier, createdHour int) AffectedBooking {
	return AffectedBooking{
		Booking:   Booking{PNR: pnr, CreatedAt: time.Date(2026, 7, 1, createdHour, 0, 0, 0, time.UTC)},
		Passenger: Passenger{LoyaltyTier: tier},
	}
}

func order(bs []AffectedBooking) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Booking.PNR
	}
	return out
}

func equal(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSortByRebookingPriorityRanksTiersFirst(t *testing.T) {
	bs := []AffectedBooking{
		booking("AAA111", TierNone, 1),
		booking("BBB222", TierSilver, 2),
		booking("CCC333", TierPlatinum, 3),
		booking("DDD444", TierGold, 4),
	}
	SortByRebookingPriority(bs)
	if got := order(bs); !equal(got, "CCC333", "DDD444", "BBB222", "AAA111") {
		t.Errorf("got %v, want platinum, gold, silver, none", got)
	}
}

func TestSortByRebookingPriorityUsesBookingDateWithinATier(t *testing.T) {
	bs := []AffectedBooking{
		booking("LATE11", TierGold, 9),
		booking("EARLY1", TierGold, 2),
		booking("MID111", TierGold, 5),
	}
	SortByRebookingPriority(bs)
	if got := order(bs); !equal(got, "EARLY1", "MID111", "LATE11") {
		t.Errorf("got %v, want earliest booking first", got)
	}
}

func TestSortByRebookingPriorityBreaksTiesOnPNR(t *testing.T) {
	bs := []AffectedBooking{
		booking("ZZZ999", TierSilver, 3),
		booking("AAA111", TierSilver, 3),
	}
	SortByRebookingPriority(bs)
	if got := order(bs); !equal(got, "AAA111", "ZZZ999") {
		t.Errorf("got %v, want deterministic PNR tie-break", got)
	}
}

func TestSortByRebookingPriorityIsDeterministic(t *testing.T) {
	build := func() []AffectedBooking {
		return []AffectedBooking{
			booking("DDD444", TierNone, 4),
			booking("AAA111", TierPlatinum, 8),
			booking("CCC333", TierNone, 4),
			booking("BBB222", TierGold, 1),
		}
	}
	first := build()
	SortByRebookingPriority(first)
	for i := 0; i < 5; i++ {
		again := build()
		SortByRebookingPriority(again)
		if !equal(order(first), order(again)...) {
			t.Fatalf("run %d produced %v, first run produced %v", i, order(again), order(first))
		}
	}
}

func TestLoyaltyTierRank(t *testing.T) {
	if !(TierPlatinum.Rank() > TierGold.Rank() &&
		TierGold.Rank() > TierSilver.Rank() &&
		TierSilver.Rank() > TierNone.Rank()) {
		t.Error("tier ranks are not strictly ordered")
	}
	if LoyaltyTier("bogus").Rank() != TierNone.Rank() {
		t.Error("unknown tier should rank as none")
	}
}
