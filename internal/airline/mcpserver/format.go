package mcpserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/domain"
)

// flightView is the shape flights take in tool output. It flattens the domain
// type and renders times as strings so the model never has to parse a struct.
type flightView struct {
	ID                string `json:"id"`
	FlightNo          string `json:"flight_no"`
	Origin            string `json:"origin"`
	Destination       string `json:"destination"`
	DepartsAt         string `json:"departs_at"`
	ArrivesAt         string `json:"arrives_at"`
	DurationMinutes   int    `json:"duration_minutes"`
	Aircraft          string `json:"aircraft"`
	Status            string `json:"status"`
	Capacity          int    `json:"capacity"`
	SeatsTaken        int    `json:"seats_taken"`
	SeatsAvailable    int    `json:"seats_available"`
	BusinessAvailable int    `json:"business_available"`
	EconomyAvailable  int    `json:"economy_available"`
}

func newFlightView(f domain.Flight) flightView {
	v := flightView{
		ID:              f.ID,
		FlightNo:        f.FlightNo,
		Origin:          f.Origin,
		Destination:     f.Destination,
		DepartsAt:       f.DepartsAt.UTC().Format(time.RFC3339),
		ArrivesAt:       f.ArrivesAt.UTC().Format(time.RFC3339),
		DurationMinutes: int(f.Duration().Minutes()),
		Aircraft:        f.Aircraft,
		Status:          string(f.Status),
		Capacity:        f.Capacity,
	}
	if f.Availability != nil {
		v.SeatsTaken = f.Availability.SeatsTaken
		v.SeatsAvailable = f.Availability.SeatsAvailable
		v.BusinessAvailable = f.Availability.BusinessAvailable
		v.EconomyAvailable = f.Availability.EconomyAvailable
	}
	return v
}

func hhmm(t time.Time) string {
	return t.UTC().Format("15:04")
}

func humanDuration(d time.Duration) string {
	m := int(d.Minutes())
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}

// line renders one flight as a single readable row for the text content.
func (v flightView) line() string {
	dep, _ := time.Parse(time.RFC3339, v.DepartsAt)
	arr, _ := time.Parse(time.RFC3339, v.ArrivesAt)
	s := fmt.Sprintf("%-18s %s to %s UTC  %s  %-5s %-9s %2d free (business %d, economy %d)",
		v.ID, hhmm(dep), hhmm(arr), humanDuration(arr.Sub(dep)), v.Aircraft, v.Status,
		v.SeatsAvailable, v.BusinessAvailable, v.EconomyAvailable)
	return strings.TrimRight(s, " ")
}

func airportLabel(a domain.Airport) string {
	return fmt.Sprintf("%s (%s)", a.Code, a.City)
}

func textResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

// passengerView is one affected passenger as tool output.
type passengerView struct {
	PNR           string `json:"pnr"`
	PassengerID   string `json:"passenger_id"`
	FullName      string `json:"full_name"`
	Email         string `json:"email"`
	LoyaltyTier   string `json:"loyalty_tier"`
	Cabin         string `json:"cabin"`
	Seat          string `json:"seat"`
	BookingStatus string `json:"booking_status"`
}

func newPassengerView(ab domain.AffectedBooking) passengerView {
	return passengerView{
		PNR:           ab.Booking.PNR,
		PassengerID:   ab.Passenger.ID,
		FullName:      ab.Passenger.FullName,
		Email:         ab.Passenger.Email,
		LoyaltyTier:   string(ab.Passenger.LoyaltyTier),
		Cabin:         string(ab.Booking.Cabin),
		Seat:          ab.Booking.Seat,
		BookingStatus: string(ab.Booking.Status),
	}
}

func newPassengerViews(abs []domain.AffectedBooking) []passengerView {
	out := make([]passengerView, 0, len(abs))
	for _, ab := range abs {
		out = append(out, newPassengerView(ab))
	}
	return out
}

func (v passengerView) line() string {
	return fmt.Sprintf("%-7s %-22s %-9s %-9s seat %-4s %s",
		v.PNR, v.FullName, v.LoyaltyTier, v.Cabin, v.Seat, v.BookingStatus)
}

// tierBreakdown counts passengers per loyalty tier, highest tier first.
func tierBreakdown(vs []passengerView) string {
	counts := map[string]int{}
	for _, v := range vs {
		counts[v.LoyaltyTier]++
	}
	parts := []string{}
	for _, tier := range []string{"platinum", "gold", "silver", "none"} {
		if n := counts[tier]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, tier))
		}
	}
	return strings.Join(parts, ", ")
}

func writePassengerList(b *strings.Builder, vs []passengerView) {
	b.WriteString("\nIn rebooking priority order (loyalty tier, then booking date):\n\n")
	for _, v := range vs {
		b.WriteString(v.line())
		b.WriteByte('\n')
	}
}
