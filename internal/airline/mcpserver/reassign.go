package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/domain"
	"r-project1/internal/airline/service"
	"r-project1/internal/airline/store"
)

type findReassignmentInput struct {
	FlightID               string `json:"flight_id" jsonschema:"Flight whose passengers need reaccommodation, for example AV201-2026-09-14"`
	MaxOptionsPerPassenger int    `json:"max_options_per_passenger,omitempty" jsonschema:"How many options to list per passenger including the recommended one. Defaults to 1"`
}

type findReassignmentOutput struct {
	Plan       service.Plan `json:"plan"`
	Candidates []flightView `json:"candidate_flights"`
	IsApplied  bool         `json:"is_applied"`
}

type applyAssignmentInput struct {
	PNR        string `json:"pnr" jsonschema:"Booking reference to move"`
	ToFlightID string `json:"to_flight_id" jsonschema:"Flight id to move the booking to"`
	Cabin      string `json:"cabin,omitempty" jsonschema:"economy or business. Defaults to the cabin the passenger already holds"`
}

type applyReassignmentInput struct {
	FlightID    string                 `json:"flight_id" jsonschema:"The cancelled flight whose passengers are being moved"`
	Assignments []applyAssignmentInput `json:"assignments,omitempty" jsonschema:"Explicit moves to apply. Omit this to apply the plan that find_reassignment_options recommends"`
}

type appliedMove struct {
	PNR           string `json:"pnr"`
	PassengerName string `json:"passenger_name"`
	ToFlightID    string `json:"to_flight_id"`
	ToCabin       string `json:"to_cabin"`
	DelayMinutes  int    `json:"delay_minutes"`
	Downgraded    bool   `json:"downgraded"`
}

type applyReassignmentOutput struct {
	FlightID     string               `json:"flight_id"`
	AppliedCount int                  `json:"applied_count"`
	Moves        []appliedMove        `json:"moves"`
	Unassigned   []service.Unassigned `json:"unassigned,omitempty"`
}

func registerReassignmentTools(srv *mcp.Server, d *deps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "find_reassignment_options",
		Title: "Find reassignment options",
		Description: "Propose replacement flights for everyone on a disrupted flight. Read only, nothing is changed. " +
			"Alternatives listed for a passenger are the ones that were free at the moment that passenger was served, " +
			"so a later passenger may have taken one since.",
	}, d.findReassignmentOptions)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "apply_reassignment",
		Title: "Apply a reassignment",
		Description: "Move passengers off a cancelled flight onto their new flights. This changes stored data. " +
			"With no assignments given it applies the same plan find_reassignment_options recommends.",
	}, d.applyReassignment)
}

// planFor loads everything the optimizer needs and runs it.
func (d *deps) planFor(ctx context.Context, flightID string, maxOptions int) (domain.Flight, []domain.Flight, service.Plan, error) {
	var plan service.Plan

	flight, err := d.store.GetFlight(ctx, flightID)
	if errors.Is(err, store.ErrNotFound) {
		return flight, nil, plan, fmt.Errorf("no flight with id %q. Use search_flights to find valid ids", flightID)
	}
	if err != nil {
		return flight, nil, plan, err
	}

	affected, err := d.store.BookingsForFlight(ctx, flight.ID, false)
	if err != nil {
		return flight, nil, plan, err
	}

	from, to := service.Window(flight)
	candidates, err := d.store.CandidateFlights(ctx, flight.Origin, flight.Destination, flight.ID, from, to)
	if err != nil {
		return flight, nil, plan, err
	}

	plan = service.BuildPlan(flight, affected, service.CandidatesFrom(candidates), maxOptions)
	return flight, candidates, plan, nil
}

func (d *deps) findReassignmentOptions(ctx context.Context, _ *mcp.CallToolRequest, in findReassignmentInput) (*mcp.CallToolResult, findReassignmentOutput, error) {
	var out findReassignmentOutput

	flight, candidates, plan, err := d.planFor(ctx, in.FlightID, in.MaxOptionsPerPassenger)
	if err != nil {
		return nil, out, err
	}

	out = findReassignmentOutput{Plan: plan}
	for _, c := range candidates {
		out.Candidates = append(out.Candidates, newFlightView(c))
	}

	var b strings.Builder
	if flight.Status != domain.FlightCancelled {
		fmt.Fprintf(&b, "Note: %s is still %s. This is a what-if plan.\n\n", flight.ID, flight.Status)
	}
	fmt.Fprintf(&b, "Reassignment plan for %s (%s to %s).\n%s\n",
		flight.ID, flight.Origin, flight.Destination, plan.Summary())
	fmt.Fprintf(&b, "\n%d candidate flight(s) within 48 hours:\n", len(candidates))
	for _, v := range out.Candidates {
		b.WriteString("  " + v.line() + "\n")
	}

	if len(plan.Assignments) > 0 {
		b.WriteString("\nProposed moves, in the order seats were assigned:\n\n")
		for _, a := range plan.Assignments {
			fmt.Fprintf(&b, "%-7s %-22s %-9s %s to %s, %s, delay %s%s\n",
				a.PNR, a.PassengerName, a.LoyaltyTier, a.FromFlightID, a.ToFlightID,
				a.ToCabin, humanMinutes(a.DelayMinutes), downgradeNote(a.Downgraded))
			for _, alt := range a.Alternatives {
				fmt.Fprintf(&b, "%-7s   also possible: %s, %s, delay %s (cost %d)\n",
					"", alt.FlightID, alt.Cabin, humanMinutes(alt.DelayMinutes), alt.Cost)
			}
		}
	}
	if len(plan.Unassigned) > 0 {
		fmt.Fprintf(&b, "\n%d passenger(s) could not be placed:\n\n", len(plan.Unassigned))
		for _, u := range plan.Unassigned {
			fmt.Fprintf(&b, "%-7s %-22s %-9s %s\n", u.PNR, u.PassengerName, u.LoyaltyTier, u.Reason)
		}
	}
	if flight.Status == domain.FlightCancelled && len(plan.Assignments) > 0 {
		b.WriteString("\nTo commit this, call apply_reassignment with just the flight id.")
	}
	return textResult("%s", b.String()), out, nil
}

func (d *deps) applyReassignment(ctx context.Context, _ *mcp.CallToolRequest, in applyReassignmentInput) (*mcp.CallToolResult, applyReassignmentOutput, error) {
	var out applyReassignmentOutput

	flight, err := d.store.GetFlight(ctx, in.FlightID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, out, fmt.Errorf("no flight with id %q", in.FlightID)
	}
	if err != nil {
		return nil, out, err
	}
	if flight.Status != domain.FlightCancelled {
		return nil, out, fmt.Errorf("flight %s is %s, not cancelled. Cancel it first with cancel_flight",
			flight.ID, flight.Status)
	}

	reason := fmt.Sprintf("reaccommodation after %s was cancelled", flight.ID)
	if flight.CancellationReason != "" {
		reason += ": " + flight.CancellationReason
	}

	var moves []store.Move
	var applied []appliedMove

	if len(in.Assignments) == 0 {
		_, _, plan, err := d.planFor(ctx, flight.ID, 1)
		if err != nil {
			return nil, out, err
		}
		if len(plan.Assignments) == 0 {
			return nil, out, fmt.Errorf("no seats are available for the %d passenger(s) on %s", plan.TotalPassengers, flight.ID)
		}
		for _, a := range plan.Assignments {
			moves = append(moves, store.Move{
				PNR: a.PNR, ToFlightID: a.ToFlightID, ToCabin: domain.Cabin(a.ToCabin),
				DelayMinutes: a.DelayMinutes, Reason: reason,
			})
			applied = append(applied, appliedMove{
				PNR: a.PNR, PassengerName: a.PassengerName, ToFlightID: a.ToFlightID,
				ToCabin: a.ToCabin, DelayMinutes: a.DelayMinutes, Downgraded: a.Downgraded,
			})
		}
		out.Unassigned = plan.Unassigned
	} else {
		moves, applied, err = d.explicitMoves(ctx, flight, in.Assignments, reason)
		if err != nil {
			return nil, out, err
		}
	}

	if err := d.store.ApplyReassignments(ctx, flight.ID, moves); err != nil {
		return nil, out, err
	}

	out.FlightID = flight.ID
	out.AppliedCount = len(applied)
	out.Moves = applied

	var b strings.Builder
	fmt.Fprintf(&b, "Applied %d reaccommodation(s) off %s.\n\n", len(applied), flight.ID)
	for _, m := range applied {
		fmt.Fprintf(&b, "%-7s %-22s to %s, %s, delay %s%s\n",
			m.PNR, m.PassengerName, m.ToFlightID, m.ToCabin,
			humanMinutes(m.DelayMinutes), downgradeNote(m.Downgraded))
	}
	if len(out.Unassigned) > 0 {
		fmt.Fprintf(&b, "\n%d passenger(s) still have no seat:\n", len(out.Unassigned))
		for _, u := range out.Unassigned {
			fmt.Fprintf(&b, "%-7s %-22s %s\n", u.PNR, u.PassengerName, u.Reason)
		}
	}
	return textResult("%s", b.String()), out, nil
}

// explicitMoves validates rep-supplied assignments and fills in the delay and
// the cabin default from what the passenger currently holds.
func (d *deps) explicitMoves(ctx context.Context, flight domain.Flight, in []applyAssignmentInput, reason string) ([]store.Move, []appliedMove, error) {
	var moves []store.Move
	var applied []appliedMove

	for _, a := range in {
		ab, err := d.store.GetBooking(ctx, a.PNR)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("no booking with PNR %q", a.PNR)
		}
		if err != nil {
			return nil, nil, err
		}
		if ab.Booking.FlightID != flight.ID {
			return nil, nil, fmt.Errorf("booking %s is on %s, not on %s", a.PNR, ab.Booking.FlightID, flight.ID)
		}

		target, err := d.store.GetFlight(ctx, a.ToFlightID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, fmt.Errorf("no flight with id %q", a.ToFlightID)
		}
		if err != nil {
			return nil, nil, err
		}
		if target.Origin != flight.Origin || target.Destination != flight.Destination {
			return nil, nil, fmt.Errorf("flight %s does not fly %s to %s", target.ID, flight.Origin, flight.Destination)
		}

		cabin := ab.Booking.Cabin
		if a.Cabin != "" {
			cabin = domain.Cabin(strings.ToLower(strings.TrimSpace(a.Cabin)))
			if !cabin.Valid() {
				return nil, nil, fmt.Errorf("cabin %q is not economy or business", a.Cabin)
			}
		}

		delay := int(target.ArrivesAt.Sub(flight.ArrivesAt).Minutes())
		moves = append(moves, store.Move{
			PNR: ab.Booking.PNR, ToFlightID: target.ID, ToCabin: cabin,
			DelayMinutes: delay, Reason: reason,
		})
		applied = append(applied, appliedMove{
			PNR: ab.Booking.PNR, PassengerName: ab.Passenger.FullName, ToFlightID: target.ID,
			ToCabin: string(cabin), DelayMinutes: delay,
			Downgraded: ab.Booking.Cabin == domain.CabinBusiness && cabin == domain.CabinEconomy,
		})
	}
	return moves, applied, nil
}
