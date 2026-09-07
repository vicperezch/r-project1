package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/store"
)

type cancelFlightInput struct {
	FlightID string `json:"flight_id" jsonschema:"Flight id to cancel, for example AV201-2026-09-14"`
	Reason   string `json:"reason" jsonschema:"Why the flight is cancelled, for example weather at GUA or a technical fault"`
}

type cancelFlightOutput struct {
	Flight        flightView      `json:"flight"`
	Reason        string          `json:"reason"`
	AffectedCount int             `json:"affected_count"`
	Passengers    []passengerView `json:"passengers"`
}

type listAffectedInput struct {
	FlightID string `json:"flight_id" jsonschema:"Flight id whose passengers to list, for example AV201-2026-09-14"`
}

type listAffectedOutput struct {
	Flight      flightView      `json:"flight"`
	IsCancelled bool            `json:"is_cancelled"`
	Count       int             `json:"count"`
	Passengers  []passengerView `json:"passengers"`
}

type getBookingInput struct {
	PNR string `json:"pnr" jsonschema:"Six character booking reference, for example K7Q2XM"`
}

type getBookingOutput struct {
	Passenger passengerView `json:"passenger"`
	Flight    flightView    `json:"flight"`
}

func registerIrregularityTools(srv *mcp.Server, d *deps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cancel_flight",
		Title:       "Cancel a flight",
		Description: "Cancel a flight and return every passenger it strands, in rebooking priority order. This changes stored data.",
	}, d.cancelFlight)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_affected_passengers",
		Title:       "List affected passengers",
		Description: "List the confirmed passengers on a flight, in rebooking priority order. Read only, safe to call before deciding anything.",
	}, d.listAffectedPassengers)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_booking",
		Title:       "Look up a booking",
		Description: "Look up one booking by its PNR, with the passenger and the flight it is on.",
	}, d.getBooking)
}

func (d *deps) cancelFlight(ctx context.Context, _ *mcp.CallToolRequest, in cancelFlightInput) (*mcp.CallToolResult, cancelFlightOutput, error) {
	var out cancelFlightOutput

	flight, err := d.store.CancelFlight(ctx, in.FlightID, in.Reason)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, out, fmt.Errorf("no flight with id %q. Use search_flights to find valid ids", in.FlightID)
	case errors.Is(err, store.ErrAlreadyCancelled):
		return nil, out, fmt.Errorf("flight %s is already cancelled. Use list_affected_passengers to see who is stranded", in.FlightID)
	case err != nil:
		return nil, out, err
	}

	affected, err := d.store.BookingsForFlight(ctx, flight.ID, false)
	if err != nil {
		return nil, out, err
	}

	out = cancelFlightOutput{
		Flight:        newFlightView(flight),
		Reason:        flight.CancellationReason,
		AffectedCount: len(affected),
		Passengers:    newPassengerViews(affected),
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Cancelled %s (%s, %s to %s, departing %s UTC).\nReason: %s\n\n",
		flight.ID, flight.FlightNo, flight.Origin, flight.Destination,
		flight.DepartsAt.UTC().Format("2006-01-02 15:04"), flight.CancellationReason)

	if len(affected) == 0 {
		b.WriteString("No passengers were booked, so nobody needs reaccommodation.")
		return textResult("%s", b.String()), out, nil
	}
	fmt.Fprintf(&b, "%d passenger(s) need reaccommodation (%s).",
		len(affected), tierBreakdown(out.Passengers))
	writePassengerList(&b, out.Passengers)
	b.WriteString("\nNext step: find_reassignment_options for this flight.")
	return textResult("%s", b.String()), out, nil
}

func (d *deps) listAffectedPassengers(ctx context.Context, _ *mcp.CallToolRequest, in listAffectedInput) (*mcp.CallToolResult, listAffectedOutput, error) {
	var out listAffectedOutput

	flight, err := d.store.GetFlight(ctx, in.FlightID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, out, fmt.Errorf("no flight with id %q. Use search_flights to find valid ids", in.FlightID)
	}
	if err != nil {
		return nil, out, err
	}

	affected, err := d.store.BookingsForFlight(ctx, flight.ID, false)
	if err != nil {
		return nil, out, err
	}

	out = listAffectedOutput{
		Flight:      newFlightView(flight),
		IsCancelled: flight.Status == "cancelled",
		Count:       len(affected),
		Passengers:  newPassengerViews(affected),
	}

	var b strings.Builder
	if out.IsCancelled {
		fmt.Fprintf(&b, "%s is cancelled (%s).\n", flight.ID, flight.CancellationReason)
	} else {
		fmt.Fprintf(&b, "%s is still %s, so these passengers are not stranded yet.\n", flight.ID, flight.Status)
	}
	if len(affected) == 0 {
		b.WriteString("\nNo confirmed passengers on this flight.")
		return textResult("%s", b.String()), out, nil
	}
	fmt.Fprintf(&b, "\n%d confirmed passenger(s) (%s).", len(affected), tierBreakdown(out.Passengers))
	writePassengerList(&b, out.Passengers)
	return textResult("%s", b.String()), out, nil
}

func (d *deps) getBooking(ctx context.Context, _ *mcp.CallToolRequest, in getBookingInput) (*mcp.CallToolResult, getBookingOutput, error) {
	var out getBookingOutput

	ab, err := d.store.GetBooking(ctx, in.PNR)
	if errors.Is(err, store.ErrNotFound) {
		return nil, out, fmt.Errorf("no booking with PNR %q", in.PNR)
	}
	if err != nil {
		return nil, out, err
	}

	flight, err := d.store.GetFlight(ctx, ab.Booking.FlightID)
	if err != nil {
		return nil, out, err
	}

	out = getBookingOutput{Passenger: newPassengerView(ab), Flight: newFlightView(flight)}

	var b strings.Builder
	fmt.Fprintf(&b, "Booking %s\n", ab.Booking.PNR)
	fmt.Fprintf(&b, "  Passenger: %s (%s, %s)\n", ab.Passenger.FullName, ab.Passenger.Email, ab.Passenger.LoyaltyTier)
	fmt.Fprintf(&b, "  Cabin:     %s, seat %s\n", ab.Booking.Cabin, ab.Booking.Seat)
	fmt.Fprintf(&b, "  Status:    %s\n", ab.Booking.Status)
	fmt.Fprintf(&b, "  Flight:    %s, %s to %s, departing %s UTC, currently %s\n",
		flight.ID, flight.Origin, flight.Destination,
		flight.DepartsAt.UTC().Format("2006-01-02 15:04"), flight.Status)
	return textResult("%s", b.String()), out, nil
}
