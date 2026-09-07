package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/domain"
	"r-project1/internal/airline/store"
)

type searchFlightsInput struct {
	Origin           string `json:"origin" jsonschema:"Departure city name or three letter IATA code, for example Guatemala City or GUA"`
	Destination      string `json:"destination" jsonschema:"Arrival city name or three letter IATA code, for example Mexico City or MEX"`
	Date             string `json:"date,omitempty" jsonschema:"Optional departure date as YYYY-MM-DD in UTC. Omit to search every scheduled day"`
	IncludeCancelled bool   `json:"include_cancelled,omitempty" jsonschema:"Set true to also return cancelled flights. Defaults to false"`
}

type searchFlightsOutput struct {
	Origin      domain.Airport `json:"origin"`
	Destination domain.Airport `json:"destination"`
	Date        string         `json:"date,omitempty"`
	Count       int            `json:"count"`
	Flights     []flightView   `json:"flights"`
}

type getFlightDetailsInput struct {
	FlightID string `json:"flight_id" jsonschema:"Flight id as returned by search_flights, for example AV201-2026-09-14"`
}

type getFlightDetailsOutput struct {
	Flight           flightView     `json:"flight"`
	Origin           domain.Airport `json:"origin"`
	Destination      domain.Airport `json:"destination"`
	BusinessCapacity int            `json:"business_capacity"`
	LoadFactor       float64        `json:"load_factor"`
}

func registerFlightTools(srv *mcp.Server, d *deps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_flights",
		Title:       "Search flights",
		Description: "Find the flights between two cities, with seat availability. Returns flight ids to pass to get_flight_details.",
	}, d.searchFlights)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_flight_details",
		Title:       "Get flight details",
		Description: "Full detail for one flight: schedule, aircraft, capacity, how full it is, and seats left per cabin.",
	}, d.getFlightDetails)
}

func (d *deps) searchFlights(ctx context.Context, _ *mcp.CallToolRequest, in searchFlightsInput) (*mcp.CallToolResult, searchFlightsOutput, error) {
	var out searchFlightsOutput

	origin, err := d.resolveAirport(ctx, in.Origin)
	if err != nil {
		return nil, out, err
	}
	destination, err := d.resolveAirport(ctx, in.Destination)
	if err != nil {
		return nil, out, err
	}
	if origin.Code == destination.Code {
		return nil, out, fmt.Errorf("origin and destination are both %s", airportLabel(origin))
	}

	p := store.SearchParams{Origin: origin.Code, Destination: destination.Code, IncludeCancelled: in.IncludeCancelled}
	if in.Date != "" {
		day, err := time.Parse("2006-01-02", strings.TrimSpace(in.Date))
		if err != nil {
			return nil, out, fmt.Errorf("date %q is not in YYYY-MM-DD form", in.Date)
		}
		p.Date = &day
	}

	flights, err := d.store.SearchFlights(ctx, p)
	if err != nil {
		return nil, out, err
	}

	out = searchFlightsOutput{Origin: origin, Destination: destination, Date: in.Date, Count: len(flights)}
	for _, f := range flights {
		out.Flights = append(out.Flights, newFlightView(f))
	}

	route := fmt.Sprintf("%s to %s", airportLabel(origin), airportLabel(destination))
	when := "across the whole schedule"
	if in.Date != "" {
		when = "on " + in.Date
	}
	if len(flights) == 0 {
		return textResult("No flights %s %s.", route, when), out, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d flight(s) %s %s:\n\n", len(flights), route, when)
	for _, v := range out.Flights {
		b.WriteString(v.line())
		b.WriteByte('\n')
	}
	return textResult("%s", b.String()), out, nil
}

func (d *deps) getFlightDetails(ctx context.Context, _ *mcp.CallToolRequest, in getFlightDetailsInput) (*mcp.CallToolResult, getFlightDetailsOutput, error) {
	var out getFlightDetailsOutput

	f, err := d.store.GetFlight(ctx, in.FlightID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, out, fmt.Errorf("no flight with id %q. Use search_flights to find valid ids", in.FlightID)
	}
	if err != nil {
		return nil, out, err
	}

	origin, err := d.resolveAirport(ctx, f.Origin)
	if err != nil {
		return nil, out, err
	}
	destination, err := d.resolveAirport(ctx, f.Destination)
	if err != nil {
		return nil, out, err
	}

	out = getFlightDetailsOutput{
		Flight:           newFlightView(f),
		Origin:           origin,
		Destination:      destination,
		BusinessCapacity: f.BusinessCapacity,
		LoadFactor:       f.LoadFactor(),
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Flight %s (%s)\n", f.ID, f.FlightNo)
	fmt.Fprintf(&b, "  Route:     %s to %s\n", airportLabel(origin), airportLabel(destination))
	fmt.Fprintf(&b, "  Departs:   %s UTC\n", f.DepartsAt.UTC().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "  Arrives:   %s UTC (%s)\n", f.ArrivesAt.UTC().Format("2006-01-02 15:04"), humanDuration(f.Duration()))
	fmt.Fprintf(&b, "  Aircraft:  %s\n", f.Aircraft)
	fmt.Fprintf(&b, "  Status:    %s\n", f.Status)
	fmt.Fprintf(&b, "  Capacity:  %d seats, %d of them business\n", f.Capacity, f.BusinessCapacity)
	if a := f.Availability; a != nil {
		fmt.Fprintf(&b, "  Sold:      %d of %d (%.0f%% full)\n", a.SeatsTaken, f.Capacity, f.LoadFactor()*100)
		fmt.Fprintf(&b, "  Available: %d total, %d business, %d economy\n", a.SeatsAvailable, a.BusinessAvailable, a.EconomyAvailable)
	}
	return textResult("%s", b.String()), out, nil
}

// resolveAirport turns whatever a rep typed, a code or a city name, into one
// airport. Ambiguity is reported back rather than guessed at.
func (d *deps) resolveAirport(ctx context.Context, query string) (domain.Airport, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return domain.Airport{}, errors.New("empty airport, give a city name or a three letter code")
	}

	matches, err := d.store.FindAirports(ctx, query)
	if err != nil {
		return domain.Airport{}, err
	}
	switch {
	case len(matches) == 0:
		return domain.Airport{}, fmt.Errorf("no airport matches %q", query)
	case len(matches) == 1 || strings.EqualFold(matches[0].Code, query):
		return matches[0], nil
	default:
		labels := make([]string, 0, len(matches))
		for _, a := range matches {
			labels = append(labels, airportLabel(a))
		}
		return domain.Airport{}, fmt.Errorf("%q matches several airports: %s. Ask which one is meant",
			query, strings.Join(labels, ", "))
	}
}
