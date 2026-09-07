// Package service holds the airline logic that is not a database query,
// principally the reassignment optimizer.
//
// BuildPlan is deliberately pure: it takes the cancelled flight, the displaced
// bookings and the candidate flights, and returns a plan. Nothing in here
// touches Postgres, so the whole algorithm is testable without a database.
package service

import (
	"fmt"
	"sort"
	"time"

	"r-project1/internal/airline/domain"
)

const (
	// DowngradePenaltyMinutes prices a cabin downgrade in minutes of delay so
	// the two are comparable in one objective. At four hours, a business
	// passenger is downgraded rather than wait more than four hours for a
	// business seat, but will wait anything less than that.
	DowngradePenaltyMinutes = 240

	// EarliestDepartureLead lets a passenger be moved onto a flight leaving
	// somewhat before the one that was cancelled.
	EarliestDepartureLead = -2 * time.Hour

	// SearchWindow bounds how far ahead a replacement is looked for.
	SearchWindow = 48 * time.Hour
)

// Window returns the departure range to search for replacements.
func Window(cancelled domain.Flight) (from, to time.Time) {
	return cancelled.DepartsAt.Add(EarliestDepartureLead), cancelled.DepartsAt.Add(SearchWindow)
}

// Candidate is a flight a displaced passenger could be moved to, with the
// seats still free on it. Seat counts are decremented as the plan is built.
type Candidate struct {
	Flight        domain.Flight
	BusinessSeats int
	EconomySeats  int
}

func CandidatesFrom(flights []domain.Flight) []Candidate {
	out := make([]Candidate, 0, len(flights))
	for _, f := range flights {
		c := Candidate{Flight: f}
		if f.Availability != nil {
			c.BusinessSeats = f.Availability.BusinessAvailable
			c.EconomySeats = f.Availability.EconomyAvailable
		}
		out = append(out, c)
	}
	return out
}

func (c *Candidate) seats(cabin domain.Cabin) int {
	if cabin == domain.CabinBusiness {
		return c.BusinessSeats
	}
	return c.EconomySeats
}

func (c *Candidate) take(cabin domain.Cabin) {
	if cabin == domain.CabinBusiness {
		c.BusinessSeats--
		return
	}
	c.EconomySeats--
}

// Option is one way to reaccommodate a passenger, priced by Cost.
type Option struct {
	FlightID     string `json:"flight_id"`
	FlightNo     string `json:"flight_no"`
	DepartsAt    string `json:"departs_at"`
	ArrivesAt    string `json:"arrives_at"`
	Cabin        string `json:"cabin"`
	DelayMinutes int    `json:"delay_minutes"`
	Downgraded   bool   `json:"downgraded"`
	Cost         int    `json:"cost"`
}

type Assignment struct {
	PNR           string   `json:"pnr"`
	PassengerID   string   `json:"passenger_id"`
	PassengerName string   `json:"passenger_name"`
	LoyaltyTier   string   `json:"loyalty_tier"`
	FromFlightID  string   `json:"from_flight_id"`
	FromCabin     string   `json:"from_cabin"`
	ToFlightID    string   `json:"to_flight_id"`
	ToCabin       string   `json:"to_cabin"`
	DelayMinutes  int      `json:"delay_minutes"`
	Downgraded    bool     `json:"downgraded"`
	Cost          int      `json:"cost"`
	Alternatives  []Option `json:"alternatives,omitempty"`
}

type Unassigned struct {
	PNR           string `json:"pnr"`
	PassengerID   string `json:"passenger_id"`
	PassengerName string `json:"passenger_name"`
	LoyaltyTier   string `json:"loyalty_tier"`
	Cabin         string `json:"cabin"`
	Reason        string `json:"reason"`
}

type Plan struct {
	CancelledFlightID string       `json:"cancelled_flight_id"`
	TotalPassengers   int          `json:"total_passengers"`
	AssignedCount     int          `json:"assigned_count"`
	UnassignedCount   int          `json:"unassigned_count"`
	TotalDelayMinutes int          `json:"total_delay_minutes"`
	DowngradeCount    int          `json:"downgrade_count"`
	Assignments       []Assignment `json:"assignments"`
	Unassigned        []Unassigned `json:"unassigned"`
}

// BuildPlan assigns displaced passengers to replacement flights.
//
// The rule is priority-ordered greedy: passengers are served in rebooking
// priority order, and each takes the cheapest seat still free at the moment
// they are served. It is not globally optimal, and it is not meant to be. It is
// deterministic and explainable, which matters more when a rep has to justify
// the outcome to a passenger standing at the counter.
//
// An economy passenger is never upgraded into an empty business seat; only the
// downgrade direction is priced.
func BuildPlan(cancelled domain.Flight, affected []domain.AffectedBooking, candidates []Candidate, maxOptions int) Plan {
	if maxOptions < 1 {
		maxOptions = 1
	}

	queue := make([]domain.AffectedBooking, len(affected))
	copy(queue, affected)
	domain.SortByRebookingPriority(queue)

	pool := make([]Candidate, len(candidates))
	copy(pool, candidates)

	plan := Plan{CancelledFlightID: cancelled.ID, TotalPassengers: len(queue)}

	for _, ab := range queue {
		options := feasibleOptions(cancelled, ab, pool)
		if len(options) == 0 {
			plan.Unassigned = append(plan.Unassigned, Unassigned{
				PNR:           ab.Booking.PNR,
				PassengerID:   ab.Passenger.ID,
				PassengerName: ab.Passenger.FullName,
				LoyaltyTier:   string(ab.Passenger.LoyaltyTier),
				Cabin:         string(ab.Booking.Cabin),
				Reason:        "no flight on this route has a seat free within 48 hours",
			})
			continue
		}

		best := options[0]
		for i := range pool {
			if pool[i].Flight.ID == best.FlightID {
				pool[i].take(domain.Cabin(best.Cabin))
				break
			}
		}

		a := Assignment{
			PNR:           ab.Booking.PNR,
			PassengerID:   ab.Passenger.ID,
			PassengerName: ab.Passenger.FullName,
			LoyaltyTier:   string(ab.Passenger.LoyaltyTier),
			FromFlightID:  cancelled.ID,
			FromCabin:     string(ab.Booking.Cabin),
			ToFlightID:    best.FlightID,
			ToCabin:       best.Cabin,
			DelayMinutes:  best.DelayMinutes,
			Downgraded:    best.Downgraded,
			Cost:          best.Cost,
		}
		if len(options) > 1 {
			end := min(maxOptions, len(options))
			a.Alternatives = options[1:end]
		}
		plan.Assignments = append(plan.Assignments, a)
		plan.TotalDelayMinutes += best.DelayMinutes
		if best.Downgraded {
			plan.DowngradeCount++
		}
	}

	plan.AssignedCount = len(plan.Assignments)
	plan.UnassignedCount = len(plan.Unassigned)
	return plan
}

// feasibleOptions prices every candidate with a seat this passenger could take,
// cheapest first. Ties break on departure time then flight id so the result is
// reproducible.
func feasibleOptions(cancelled domain.Flight, ab domain.AffectedBooking, pool []Candidate) []Option {
	var out []Option
	for i := range pool {
		if o, ok := evaluate(cancelled, ab, &pool[i]); ok {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost < out[j].Cost
		}
		if out[i].DepartsAt != out[j].DepartsAt {
			return out[i].DepartsAt < out[j].DepartsAt
		}
		return out[i].FlightID < out[j].FlightID
	})
	return out
}

func evaluate(cancelled domain.Flight, ab domain.AffectedBooking, c *Candidate) (Option, bool) {
	delay := int(c.Flight.ArrivesAt.Sub(cancelled.ArrivesAt).Minutes())
	base := Option{
		FlightID:     c.Flight.ID,
		FlightNo:     c.Flight.FlightNo,
		DepartsAt:    c.Flight.DepartsAt.UTC().Format(time.RFC3339),
		ArrivesAt:    c.Flight.ArrivesAt.UTC().Format(time.RFC3339),
		DelayMinutes: delay,
	}

	held := ab.Booking.Cabin
	if c.seats(held) > 0 {
		base.Cabin = string(held)
		base.Cost = delay
		return base, true
	}
	if held == domain.CabinBusiness && c.seats(domain.CabinEconomy) > 0 {
		base.Cabin = string(domain.CabinEconomy)
		base.Downgraded = true
		base.Cost = delay + DowngradePenaltyMinutes
		return base, true
	}
	return Option{}, false
}

// Summary is a one line description of the outcome, for the tool text output.
func (p Plan) Summary() string {
	if p.AssignedCount == 0 {
		return fmt.Sprintf("No seats available for any of the %d displaced passenger(s).", p.TotalPassengers)
	}
	avg := p.TotalDelayMinutes / p.AssignedCount
	return fmt.Sprintf("%d of %d passenger(s) reaccommodated, %d downgraded, average delay %dh%02dm.",
		p.AssignedCount, p.TotalPassengers, p.DowngradeCount, avg/60, avg%60)
}
