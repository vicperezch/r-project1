package service

import (
	"reflect"
	"testing"
	"time"

	"r-project1/internal/airline/domain"
)

var base = time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

func at(h, m int) time.Time {
	return base.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

// The cancelled flight departs 06:00 and would have arrived 08:30.
func cancelledFlight() domain.Flight {
	return domain.Flight{
		ID: "AV201-2026-09-14", FlightNo: "AV201", Origin: "GUA", Destination: "MEX",
		DepartsAt: at(6, 0), ArrivesAt: at(8, 30), Status: domain.FlightCancelled,
	}
}

func cand(id string, depH, depM, arrH, arrM, biz, eco int) Candidate {
	return Candidate{
		Flight: domain.Flight{
			ID: id, FlightNo: id, Origin: "GUA", Destination: "MEX",
			DepartsAt: at(depH, depM), ArrivesAt: at(arrH, arrM), Status: domain.FlightScheduled,
		},
		BusinessSeats: biz, EconomySeats: eco,
	}
}

func pax(pnr string, tier domain.LoyaltyTier, cabin domain.Cabin, createdHour int) domain.AffectedBooking {
	return domain.AffectedBooking{
		Booking: domain.Booking{
			PNR: pnr, PassengerID: "PAX-" + pnr, FlightID: "AV201-2026-09-14",
			Cabin: cabin, Status: domain.BookingConfirmed,
			CreatedAt: time.Date(2026, 7, 1, createdHour, 0, 0, 0, time.UTC),
		},
		Passenger: domain.Passenger{ID: "PAX-" + pnr, FullName: "Pax " + pnr, LoyaltyTier: tier},
	}
}

func TestEveryoneFitsOnTheEarliestFlight(t *testing.T) {
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{
			pax("AAA111", domain.TierNone, domain.CabinEconomy, 1),
			pax("BBB222", domain.TierNone, domain.CabinEconomy, 2),
		},
		[]Candidate{cand("EARLY", 12, 15, 14, 45, 0, 5)}, 1)

	if plan.AssignedCount != 2 || plan.UnassignedCount != 0 {
		t.Fatalf("assigned %d unassigned %d, want 2 and 0", plan.AssignedCount, plan.UnassignedCount)
	}
	for _, a := range plan.Assignments {
		if a.ToFlightID != "EARLY" || a.DelayMinutes != 375 {
			t.Errorf("%s went to %s with delay %d, want EARLY and 375", a.PNR, a.ToFlightID, a.DelayMinutes)
		}
	}
}

func TestSeatsAreConsumedSoPriorityDecidesWhoGetsThem(t *testing.T) {
	// One seat on the good flight, one on the bad. Gold must get the good one.
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{
			pax("LOWLY1", domain.TierNone, domain.CabinEconomy, 1),
			pax("GOLD11", domain.TierGold, domain.CabinEconomy, 9),
		},
		[]Candidate{
			cand("GOOD", 12, 15, 14, 45, 0, 1),
			cand("BAD", 18, 30, 21, 0, 0, 1),
		}, 1)

	got := map[string]string{}
	for _, a := range plan.Assignments {
		got[a.PNR] = a.ToFlightID
	}
	if got["GOLD11"] != "GOOD" || got["LOWLY1"] != "BAD" {
		t.Fatalf("got %v, want gold on GOOD and none-tier on BAD", got)
	}
}

func TestShortCapacityLeavesTheLowestPriorityUnassigned(t *testing.T) {
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{
			pax("PLAT11", domain.TierPlatinum, domain.CabinEconomy, 5),
			pax("NONE11", domain.TierNone, domain.CabinEconomy, 1),
		},
		[]Candidate{cand("ONLY", 12, 15, 14, 45, 0, 1)}, 1)

	if plan.AssignedCount != 1 || plan.UnassignedCount != 1 {
		t.Fatalf("assigned %d unassigned %d, want 1 and 1", plan.AssignedCount, plan.UnassignedCount)
	}
	if plan.Assignments[0].PNR != "PLAT11" {
		t.Errorf("assigned %s, want the platinum passenger", plan.Assignments[0].PNR)
	}
	if plan.Unassigned[0].PNR != "NONE11" {
		t.Errorf("stranded %s, want the untiered passenger", plan.Unassigned[0].PNR)
	}
	if plan.Unassigned[0].Reason == "" {
		t.Error("unassigned passengers must carry a reason")
	}
}

func TestBusinessPassengerWaitsWhenTheWaitIsShorterThanThePenalty(t *testing.T) {
	// Economy now costs 90 + 240 = 330. Business later costs 270. Waiting wins.
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{pax("BIZ111", domain.TierGold, domain.CabinBusiness, 1)},
		[]Candidate{
			cand("ECONOW", 9, 0, 10, 0, 0, 5),
			cand("BIZLATER", 11, 30, 13, 0, 2, 5),
		}, 1)

	a := plan.Assignments[0]
	if a.ToFlightID != "BIZLATER" || a.Downgraded {
		t.Fatalf("got %s downgraded=%v, want BIZLATER without a downgrade", a.ToFlightID, a.Downgraded)
	}
	if a.Cost != 270 {
		t.Errorf("cost %d, want 270", a.Cost)
	}
}

func TestBusinessPassengerDowngradesWhenTheWaitExceedsThePenalty(t *testing.T) {
	// Economy now costs 90 + 240 = 330. Business later costs 390. Downgrading wins.
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{pax("BIZ111", domain.TierGold, domain.CabinBusiness, 1)},
		[]Candidate{
			cand("ECONOW", 9, 0, 10, 0, 0, 5),
			cand("BIZLATER", 13, 30, 15, 0, 2, 5),
		}, 1)

	a := plan.Assignments[0]
	if a.ToFlightID != "ECONOW" || !a.Downgraded {
		t.Fatalf("got %s downgraded=%v, want ECONOW with a downgrade", a.ToFlightID, a.Downgraded)
	}
	if a.ToCabin != string(domain.CabinEconomy) || a.FromCabin != string(domain.CabinBusiness) {
		t.Errorf("cabins %s to %s, want business to economy", a.FromCabin, a.ToCabin)
	}
	if a.Cost != 330 || plan.DowngradeCount != 1 {
		t.Errorf("cost %d downgrades %d, want 330 and 1", a.Cost, plan.DowngradeCount)
	}
}

func TestEconomyPassengerIsNeverUpgraded(t *testing.T) {
	// Business seats are free, economy is not. The passenger stays unassigned.
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{pax("ECO111", domain.TierPlatinum, domain.CabinEconomy, 1)},
		[]Candidate{cand("BIZONLY", 12, 15, 14, 45, 4, 0)}, 1)

	if plan.UnassignedCount != 1 {
		t.Fatalf("assigned %+v, want the economy passenger left unassigned", plan.Assignments)
	}
}

func TestNoCandidatesStrandsEveryone(t *testing.T) {
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{
			pax("AAA111", domain.TierPlatinum, domain.CabinBusiness, 1),
			pax("BBB222", domain.TierNone, domain.CabinEconomy, 2),
		},
		nil, 1)

	if plan.AssignedCount != 0 || plan.UnassignedCount != 2 {
		t.Fatalf("assigned %d unassigned %d, want 0 and 2", plan.AssignedCount, plan.UnassignedCount)
	}
	if plan.Summary() == "" {
		t.Error("summary should still describe the outcome")
	}
}

func TestAlternativesAreReturnedWhenAsked(t *testing.T) {
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{pax("AAA111", domain.TierNone, domain.CabinEconomy, 1)},
		[]Candidate{
			cand("FIRST", 12, 15, 14, 45, 0, 5),
			cand("SECOND", 18, 30, 21, 0, 0, 5),
			cand("THIRD", 20, 0, 22, 0, 0, 5),
		}, 3)

	a := plan.Assignments[0]
	if a.ToFlightID != "FIRST" {
		t.Fatalf("chose %s, want the cheapest FIRST", a.ToFlightID)
	}
	if len(a.Alternatives) != 2 {
		t.Fatalf("got %d alternatives, want 2", len(a.Alternatives))
	}
	if a.Alternatives[0].FlightID != "SECOND" || a.Alternatives[1].FlightID != "THIRD" {
		t.Errorf("alternatives %v, want SECOND then THIRD by cost", a.Alternatives)
	}
	if a.Alternatives[0].Cost <= a.Cost {
		t.Error("alternatives must be worse than the chosen option")
	}
}

func TestMaxOptionsOfOneReturnsNoAlternatives(t *testing.T) {
	plan := BuildPlan(cancelledFlight(),
		[]domain.AffectedBooking{pax("AAA111", domain.TierNone, domain.CabinEconomy, 1)},
		[]Candidate{
			cand("FIRST", 12, 15, 14, 45, 0, 5),
			cand("SECOND", 18, 30, 21, 0, 0, 5),
		}, 1)

	if len(plan.Assignments[0].Alternatives) != 0 {
		t.Errorf("got %d alternatives, want none", len(plan.Assignments[0].Alternatives))
	}
}

func TestBuildPlanDoesNotMutateItsInputs(t *testing.T) {
	affected := []domain.AffectedBooking{
		pax("ZZZ999", domain.TierNone, domain.CabinEconomy, 1),
		pax("AAA111", domain.TierPlatinum, domain.CabinEconomy, 2),
	}
	candidates := []Candidate{cand("ONLY", 12, 15, 14, 45, 0, 5)}

	BuildPlan(cancelledFlight(), affected, candidates, 1)

	if affected[0].Booking.PNR != "ZZZ999" {
		t.Error("the caller's booking slice was reordered")
	}
	if candidates[0].EconomySeats != 5 {
		t.Errorf("the caller's candidate seats were decremented to %d", candidates[0].EconomySeats)
	}
}

func TestBuildPlanIsDeterministic(t *testing.T) {
	build := func() Plan {
		return BuildPlan(cancelledFlight(),
			[]domain.AffectedBooking{
				pax("AAA111", domain.TierNone, domain.CabinEconomy, 3),
				pax("BBB222", domain.TierNone, domain.CabinEconomy, 3),
				pax("CCC333", domain.TierGold, domain.CabinBusiness, 1),
				pax("DDD444", domain.TierGold, domain.CabinEconomy, 2),
			},
			[]Candidate{
				cand("ONE", 12, 15, 14, 45, 1, 2),
				cand("TWO", 18, 30, 21, 0, 0, 1),
			}, 2)
	}
	first := build()
	for i := 0; i < 5; i++ {
		again := build()
		if len(again.Assignments) != len(first.Assignments) {
			t.Fatalf("run %d assigned a different number", i)
		}
		for j := range first.Assignments {
			if !reflect.DeepEqual(nil2(first.Assignments[j]), nil2(again.Assignments[j])) {
				t.Fatalf("run %d differs at %d: %+v vs %+v", i, j, again.Assignments[j], first.Assignments[j])
			}
		}
	}
}

// nil2 compares assignments ignoring the Alternatives slice, which is not
// comparable with ==.
func nil2(a Assignment) Assignment {
	a.Alternatives = nil
	return a
}
