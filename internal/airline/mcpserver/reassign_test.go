package mcpserver

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type bookingRow struct {
	pnr, flightID, cabin string
	seat                 *string
}

// snapshotBookings records where every booking on a flight currently sits, so
// a test that reassigns them can put them back.
func snapshotBookings(t *testing.T, flightID string) []bookingRow {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("snapshot connect: %v", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `select pnr, flight_id, cabin, seat from bookings where flight_id = $1`, flightID)
	if err != nil {
		t.Fatalf("snapshot query: %v", err)
	}
	defer rows.Close()

	var out []bookingRow
	for rows.Next() {
		var b bookingRow
		if err := rows.Scan(&b.pnr, &b.flightID, &b.cabin, &b.seat); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func restoreBookings(t *testing.T, flightID string, snap []bookingRow) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("restore connect: %v", err)
	}
	defer conn.Close(ctx)

	for _, b := range snap {
		if _, err := conn.Exec(ctx,
			`update bookings set flight_id = $2, cabin = $3, seat = $4 where pnr = $1`,
			b.pnr, b.flightID, b.cabin, b.seat); err != nil {
			t.Fatalf("restore %s: %v", b.pnr, err)
		}
	}
	if _, err := conn.Exec(ctx, `delete from reassignments where from_flight_id = $1`, flightID); err != nil {
		t.Fatalf("clear reassignments: %v", err)
	}
}

// restoreAfterReassignment registers cleanup for both the flight status and
// every booking a reassignment moves, so these tests can be run repeatedly.
func restoreAfterReassignment(t *testing.T) {
	t.Helper()
	snap := snapshotBookings(t, targetFlight)
	t.Cleanup(func() {
		restoreBookings(t, targetFlight, snap)
		restoreFlight(t, targetFlight)
	})
}

func TestFindReassignmentOptionsPlacesEveryone(t *testing.T) {
	s, ctx := testSession(t)
	restoreAfterReassignment(t)
	call(t, s, ctx, "cancel_flight", map[string]any{"flight_id": targetFlight, "reason": "crew shortage"})

	res := call(t, s, ctx, "find_reassignment_options", map[string]any{"flight_id": targetFlight})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}
	out := structured[findReassignmentOutput](t, res)

	if out.Plan.TotalPassengers != 18 || out.Plan.AssignedCount != 18 || out.Plan.UnassignedCount != 0 {
		t.Fatalf("total %d assigned %d unassigned %d, want 18, 18, 0",
			out.Plan.TotalPassengers, out.Plan.AssignedCount, out.Plan.UnassignedCount)
	}

	// Four business passengers, only two business seats free same day. The
	// other two should downgrade rather than wait a full day.
	if out.Plan.DowngradeCount != 2 {
		t.Errorf("downgrades %d, want 2", out.Plan.DowngradeCount)
	}

	// Seats are consumed in order, so the plan must exactly fill the same day
	// alternatives before spilling into the next morning.
	perFlight := map[string]int{}
	for _, a := range out.Plan.Assignments {
		perFlight[a.ToFlightID]++
	}
	want := map[string]int{"AV203-2026-09-14": 6, "AV205-2026-09-14": 5, "AV201-2026-09-15": 7}
	for id, n := range want {
		if perFlight[id] != n {
			t.Errorf("%s got %d passengers, want %d (full distribution %v)", id, perFlight[id], n, perFlight)
		}
	}
}

func TestFindReassignmentOptionsReturnsAlternatives(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "find_reassignment_options", map[string]any{
		"flight_id": targetFlight, "max_options_per_passenger": 3,
	})
	out := structured[findReassignmentOutput](t, res)
	if len(out.Plan.Assignments) == 0 {
		t.Fatal("no assignments")
	}
	if len(out.Plan.Assignments[0].Alternatives) == 0 {
		t.Error("expected alternatives when max_options_per_passenger is 3")
	}
}

func TestFindReassignmentOptionsOnScheduledFlightIsAWhatIf(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "find_reassignment_options", map[string]any{"flight_id": targetFlight})
	if res.IsError {
		t.Fatalf("should work as a what-if: %s", callText(t, res))
	}
	if !strings.Contains(callText(t, res), "what-if") {
		t.Errorf("text should flag this is hypothetical:\n%s", callText(t, res))
	}
}

func TestApplyReassignmentRequiresACancelledFlight(t *testing.T) {
	s, ctx := testSession(t)

	res := call(t, s, ctx, "apply_reassignment", map[string]any{"flight_id": targetFlight})
	if !res.IsError {
		t.Fatal("applying to a scheduled flight should be refused")
	}
	if !strings.Contains(callText(t, res), "cancel_flight") {
		t.Errorf("error should say to cancel first: %s", callText(t, res))
	}
}

func TestApplyReassignmentMovesEveryone(t *testing.T) {
	s, ctx := testSession(t)
	restoreAfterReassignment(t)
	call(t, s, ctx, "cancel_flight", map[string]any{"flight_id": targetFlight, "reason": "weather"})

	res := call(t, s, ctx, "apply_reassignment", map[string]any{"flight_id": targetFlight})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}
	out := structured[applyReassignmentOutput](t, res)
	if out.AppliedCount != 18 {
		t.Fatalf("applied %d, want 18", out.AppliedCount)
	}

	// The cancelled flight should now have nobody left on it.
	after := call(t, s, ctx, "list_affected_passengers", map[string]any{"flight_id": targetFlight})
	if n := structured[listAffectedOutput](t, after).Count; n != 0 {
		t.Errorf("%d passengers still on the cancelled flight, want 0", n)
	}

	// And the same day alternative should now be full.
	det := call(t, s, ctx, "get_flight_details", map[string]any{"flight_id": "AV203-2026-09-14"})
	d := structured[getFlightDetailsOutput](t, det)
	if d.Flight.SeatsAvailable != 0 {
		t.Errorf("AV203 has %d seats left, want 0 after absorbing 6 passengers", d.Flight.SeatsAvailable)
	}
}

func TestApplyReassignmentWithExplicitAssignments(t *testing.T) {
	s, ctx := testSession(t)
	restoreAfterReassignment(t)
	call(t, s, ctx, "cancel_flight", map[string]any{"flight_id": targetFlight, "reason": "technical"})

	list := call(t, s, ctx, "list_affected_passengers", map[string]any{"flight_id": targetFlight})
	pnr := structured[listAffectedOutput](t, list).Passengers[0].PNR

	res := call(t, s, ctx, "apply_reassignment", map[string]any{
		"flight_id": targetFlight,
		"assignments": []map[string]any{
			{"pnr": pnr, "to_flight_id": "AV205-2026-09-14", "cabin": "economy"},
		},
	})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", callText(t, res))
	}
	out := structured[applyReassignmentOutput](t, res)
	if out.AppliedCount != 1 || out.Moves[0].PNR != pnr {
		t.Fatalf("applied %+v, want just %s", out.Moves, pnr)
	}

	got := call(t, s, ctx, "get_booking", map[string]any{"pnr": pnr})
	if id := structured[getBookingOutput](t, got).Flight.ID; id != "AV205-2026-09-14" {
		t.Errorf("booking is on %s, want AV205-2026-09-14", id)
	}
}

func TestApplyReassignmentRejectsAWrongRoute(t *testing.T) {
	s, ctx := testSession(t)
	restoreAfterReassignment(t)
	call(t, s, ctx, "cancel_flight", map[string]any{"flight_id": targetFlight, "reason": "technical"})

	list := call(t, s, ctx, "list_affected_passengers", map[string]any{"flight_id": targetFlight})
	pnr := structured[listAffectedOutput](t, list).Passengers[0].PNR

	res := call(t, s, ctx, "apply_reassignment", map[string]any{
		"flight_id": targetFlight,
		"assignments": []map[string]any{
			{"pnr": pnr, "to_flight_id": "AV310-2026-09-14"},
		},
	})
	if !res.IsError {
		t.Fatal("moving a passenger onto a different route should be refused")
	}
	if !strings.Contains(callText(t, res), "does not fly") {
		t.Errorf("unhelpful message: %s", callText(t, res))
	}
}
