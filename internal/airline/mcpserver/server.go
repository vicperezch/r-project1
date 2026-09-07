// Package mcpserver exposes the airline services as MCP tools.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/store"
)

const (
	Name    = "airline-assistant"
	Version = "0.1.0"
)

const instructions = `Airline assistant for counter and call center representatives.

Flights are identified by ids of the form FLIGHTNO-YYYY-MM-DD, for example AV201-2026-09-14.
Use search_flights to find those ids, then pass one to get_flight_details.

Cities can be given either as a three letter IATA code or as a city name; the tools resolve
either form. All times are UTC.`

type deps struct {
	store *store.Store
}

func New(st *store.Store) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:        Name,
		Title:       "Airline Assistant",
		Description: "Flight search, flight details, and irregularity handling for airline reps.",
		Version:     Version,
	}, &mcp.ServerOptions{Instructions: instructions})

	d := &deps{store: st}
	registerFlightTools(srv, d)
	registerIrregularityTools(srv, d)
	registerReassignmentTools(srv, d)
	return srv
}
