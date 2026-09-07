package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"r-project1/internal/airline/domain"
)

// The demo flight, whose passenger list is fixed by the seed.
const targetFlight = "AV201-2026-09-14"

// restoreFlight puts a flight back to scheduled so cancellation tests can be
// run repeatedly against the same database. It talks to Postgres directly
// rather than widening the store API for a test-only need.
func restoreFlight(t *testing.T, id string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("restore connect: %v", err)
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, `
update flights set status = 'scheduled', cancellation_reason = null, cancelled_at = null
where id = $1`, id)
	if err != nil {
		t.Fatalf("restore %s: %v", id, err)
	}
}

func structured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCancelFlightReturnsStrandedPassengersInPriorityOrder(t *testing.T) {
	s, ctx := testSession(t)
	t.Cleanup(func() { restoreFlight(t, targetFlight) })

	res := call(t, s, ctx, "cancel_flight", map[string]any{
		"flight_id": targetFlight, "reason": "crew shortage at GUA",
	})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}

	out := structured[cancelFlightOutput](t, res)
	if out.AffectedCount != 18 || len(out.Passengers) != 18 {
		t.Fatalf("affected %d, passengers %d, want 18 and 18", out.AffectedCount, len(out.Passengers))
	}
	if out.Flight.Status != "cancelled" {
		t.Errorf("flight status %q, want cancelled", out.Flight.Status)
	}
	if out.Reason != "crew shortage at GUA" {
		t.Errorf("reason %q was not persisted", out.Reason)
	}

	// Priority order: tier rank must never increase as the list goes on.
	prev := 99
	for i, p := range out.Passengers {
		r := domain.LoyaltyTier(p.LoyaltyTier).Rank()
		if r > prev {
			t.Fatalf("position %d (%s, %s) outranks the one before it", i, p.FullName, p.LoyaltyTier)
		}
		prev = r
	}
	if got := out.Passengers[0].LoyaltyTier; got != "platinum" {
		t.Errorf("first passenger is %s, want platinum", got)
	}

	text := callText(t, res)
	for _, want := range []string{"Cancelled " + targetFlight, "crew shortage at GUA",
		"2 platinum, 3 gold, 4 silver, 9 none", "find_reassignment_options"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
}

func TestCancelFlightTwiceIsReportedNotRepeated(t *testing.T) {
	s, ctx := testSession(t)
	t.Cleanup(func() { restoreFlight(t, targetFlight) })

	if res := call(t, s, ctx, "cancel_flight", map[string]any{
		"flight_id": targetFlight, "reason": "weather",
	}); res.IsError {
		t.Fatalf("first cancel failed: %s", callText(t, res))
	}

	res := call(t, s, ctx, "cancel_flight", map[string]any{
		"flight_id": targetFlight, "reason": "weather again",
	})
	if !res.IsError {
		t.Fatal("second cancel should report the flight is already cancelled")
	}
	if !strings.Contains(callText(t, res), "already cancelled") {
		t.Errorf("unhelpful message: %s", callText(t, res))
	}
}

func TestCancelFlightUnknownID(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "cancel_flight", map[string]any{"flight_id": "NOPE", "reason": "x"})
	if !res.IsError {
		t.Fatal("expected a tool error")
	}
}

func TestCancelFlightRequiresReason(t *testing.T) {
	s, ctx := testSession(t)

	// reason has no omitempty, so schema validation rejects the call.
	res := call(t, s, ctx, "cancel_flight", map[string]any{"flight_id": targetFlight})
	if !res.IsError {
		t.Fatalf("expected a validation error, got: %s", callText(t, res))
	}
}

func TestListAffectedPassengersOnScheduledFlight(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "list_affected_passengers", map[string]any{"flight_id": targetFlight})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}
	out := structured[listAffectedOutput](t, res)
	if out.IsCancelled {
		t.Error("flight should not be cancelled in this test")
	}
	if out.Count != 18 {
		t.Errorf("count %d, want 18", out.Count)
	}
	if !strings.Contains(callText(t, res), "not stranded yet") {
		t.Errorf("text should flag that the flight is still scheduled:\n%s", callText(t, res))
	}
}

func TestListAffectedPassengersAfterCancellation(t *testing.T) {
	s, ctx := testSession(t)
	t.Cleanup(func() { restoreFlight(t, targetFlight) })

	call(t, s, ctx, "cancel_flight", map[string]any{"flight_id": targetFlight, "reason": "bird strike"})

	res := call(t, s, ctx, "list_affected_passengers", map[string]any{"flight_id": targetFlight})
	out := structured[listAffectedOutput](t, res)
	if !out.IsCancelled || out.Count != 18 {
		t.Fatalf("cancelled=%v count=%d, want true and 18", out.IsCancelled, out.Count)
	}
	if !strings.Contains(callText(t, res), "bird strike") {
		t.Errorf("text should cite the cancellation reason:\n%s", callText(t, res))
	}
}

func TestGetBookingRoundTrip(t *testing.T) {
	s, ctx := testSession(t)

	list := call(t, s, ctx, "list_affected_passengers", map[string]any{"flight_id": targetFlight})
	out := structured[listAffectedOutput](t, list)
	if len(out.Passengers) == 0 {
		t.Fatal("no passengers to look up")
	}
	want := out.Passengers[0]

	res := call(t, s, ctx, "get_booking", map[string]any{"pnr": want.PNR})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}
	got := structured[getBookingOutput](t, res)
	if got.Passenger.PNR != want.PNR || got.Passenger.FullName != want.FullName {
		t.Errorf("got %+v, want PNR %s for %s", got.Passenger, want.PNR, want.FullName)
	}
	if got.Flight.ID != targetFlight {
		t.Errorf("flight %s, want %s", got.Flight.ID, targetFlight)
	}
}

func TestGetBookingIsCaseInsensitive(t *testing.T) {
	s, ctx := testSession(t)

	list := call(t, s, ctx, "list_affected_passengers", map[string]any{"flight_id": targetFlight})
	pnr := structured[listAffectedOutput](t, list).Passengers[0].PNR

	res := call(t, s, ctx, "get_booking", map[string]any{"pnr": strings.ToLower(pnr)})
	if res.IsError {
		t.Fatalf("lowercase PNR should resolve: %s", callText(t, res))
	}
}

func TestGetBookingUnknownPNR(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "get_booking", map[string]any{"pnr": "ZZZZZZ"})
	if !res.IsError {
		t.Fatal("expected a tool error")
	}
}
