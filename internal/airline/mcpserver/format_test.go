package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
	"time"

	"r-project1/internal/airline/domain"
)

// These tests need no database. They cover the text the model actually reads,
// which is what makes the difference between a usable answer and a confusing
// one.

func sampleFlight() domain.Flight {
	dep := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	return domain.Flight{
		ID: "AV201-2026-09-14", FlightNo: "AV201", Origin: "GUA", Destination: "MEX",
		DepartsAt: dep, ArrivesAt: dep.Add(150 * time.Minute),
		Aircraft: "A320", Capacity: 20, BusinessCapacity: 4, Status: domain.FlightScheduled,
		Availability: &domain.Availability{
			SeatsTaken: 18, SeatsAvailable: 2, BusinessAvailable: 0, EconomyAvailable: 2,
		},
	}
}

func TestNewFlightViewFlattensTheDomainType(t *testing.T) {
	v := newFlightView(sampleFlight())

	if v.ID != "AV201-2026-09-14" || v.FlightNo != "AV201" {
		t.Errorf("identity lost: %+v", v)
	}
	if v.DurationMinutes != 150 {
		t.Errorf("duration %d, want 150", v.DurationMinutes)
	}
	if v.SeatsAvailable != 2 || v.BusinessAvailable != 0 || v.EconomyAvailable != 2 {
		t.Errorf("availability lost: %+v", v)
	}
	// Times are strings so the model never has to parse a struct.
	if _, err := time.Parse(time.RFC3339, v.DepartsAt); err != nil {
		t.Errorf("departs_at %q is not RFC3339", v.DepartsAt)
	}
}

func TestNewFlightViewWithoutAvailability(t *testing.T) {
	f := sampleFlight()
	f.Availability = nil
	v := newFlightView(f)
	if v.SeatsAvailable != 0 || v.SeatsTaken != 0 {
		t.Errorf("a flight with no availability joined should report zeros, got %+v", v)
	}
}

func TestFlightViewLineIsOneReadableRow(t *testing.T) {
	line := newFlightView(sampleFlight()).line()

	if strings.Contains(line, "\n") {
		t.Fatal("a flight row must be a single line")
	}
	for _, want := range []string{"AV201-2026-09-14", "06:00", "08:30", "2h30m", "A320", "scheduled"} {
		if !strings.Contains(line, want) {
			t.Errorf("row is missing %q: %s", want, line)
		}
	}
	if !strings.Contains(line, "business 0") || !strings.Contains(line, "economy 2") {
		t.Errorf("row should break seats down per cabin: %s", line)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		150 * time.Minute: "2h30m",
		50 * time.Minute:  "0h50m",
		0:                 "0h00m",
		305 * time.Minute: "5h05m",
	}
	for in, want := range cases {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanMinutes(t *testing.T) {
	cases := map[int]string{
		0: "0h00m", 375: "6h15m", 1440: "24h00m",
		// A replacement flight can arrive earlier than the original.
		-90: "-1h30m",
	}
	for in, want := range cases {
		if got := humanMinutes(in); got != want {
			t.Errorf("humanMinutes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHHMMIsAlwaysUTC(t *testing.T) {
	// Given a non UTC input, the rendered clock time must still be UTC, or a
	// rep reads the wrong departure time.
	loc := time.FixedZone("UTC-6", -6*3600)
	local := time.Date(2026, 9, 14, 0, 0, 0, 0, loc)
	if got := hhmm(local); got != "06:00" {
		t.Errorf("hhmm = %q, want 06:00 in UTC", got)
	}
}

func TestAirportLabel(t *testing.T) {
	got := airportLabel(domain.Airport{Code: "GUA", City: "Guatemala City"})
	if got != "GUA (Guatemala City)" {
		t.Errorf("label = %q", got)
	}
}

func TestTextResult(t *testing.T) {
	res := textResult("%d flights to %s", 3, "MEX")
	if len(res.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(res.Content))
	}
	if got := callTextOf(res); got != "3 flights to MEX" {
		t.Errorf("text = %q", got)
	}
}

func TestNewPassengerView(t *testing.T) {
	ab := domain.AffectedBooking{
		Booking: domain.Booking{
			PNR: "K7Q2XM", Cabin: domain.CabinBusiness, Seat: "1A",
			Status: domain.BookingConfirmed,
		},
		Passenger: domain.Passenger{
			ID: "PAX001", FullName: "Ana Garcia", Email: "ana@example.com",
			LoyaltyTier: domain.TierPlatinum,
		},
	}
	v := newPassengerView(ab)
	if v.PNR != "K7Q2XM" || v.FullName != "Ana Garcia" || v.LoyaltyTier != "platinum" {
		t.Errorf("view lost fields: %+v", v)
	}
	line := v.line()
	for _, want := range []string{"K7Q2XM", "Ana Garcia", "platinum", "business", "1A"} {
		if !strings.Contains(line, want) {
			t.Errorf("passenger row missing %q: %s", want, line)
		}
	}

	views := newPassengerViews([]domain.AffectedBooking{ab, ab})
	if len(views) != 2 {
		t.Errorf("newPassengerViews returned %d", len(views))
	}
	if len(newPassengerViews(nil)) != 0 {
		t.Error("nil input should produce an empty slice")
	}
}

func TestTierBreakdownCountsHighestTierFirst(t *testing.T) {
	vs := []passengerView{
		{LoyaltyTier: "none"}, {LoyaltyTier: "platinum"}, {LoyaltyTier: "none"},
		{LoyaltyTier: "gold"}, {LoyaltyTier: "none"}, {LoyaltyTier: "platinum"},
	}
	got := tierBreakdown(vs)
	if got != "2 platinum, 1 gold, 3 none" {
		t.Errorf("breakdown = %q, want highest tier first with silver omitted", got)
	}
	if tierBreakdown(nil) != "" {
		t.Error("no passengers should produce an empty breakdown")
	}
}

func TestWritePassengerListExplainsTheOrder(t *testing.T) {
	var b strings.Builder
	writePassengerList(&b, []passengerView{
		{PNR: "AAA111", FullName: "Ana", LoyaltyTier: "gold", Cabin: "economy", Seat: "12A"},
	})
	out := b.String()
	if !strings.Contains(out, "priority order") {
		t.Errorf("the list should say what order it is in: %s", out)
	}
	if !strings.Contains(out, "AAA111") {
		t.Errorf("the list is missing its row: %s", out)
	}
}

func TestDowngradeNote(t *testing.T) {
	if downgradeNote(true) != " (downgraded)" {
		t.Error("a downgrade should be called out")
	}
	if downgradeNote(false) != "" {
		t.Error("a same cabin move should add nothing")
	}
}

// callTextOf joins the text content of a result.
func callTextOf(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
