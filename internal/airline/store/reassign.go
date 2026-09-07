package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"r-project1/internal/airline/domain"
)

// CandidateFlights returns scheduled flights on the same route inside a
// departure window, with availability joined, ready to be priced.
func (s *Store) CandidateFlights(ctx context.Context, origin, destination, excludeID string, from, to time.Time) ([]domain.Flight, error) {
	const q = flightSelect + `
where f.origin = $1
  and f.destination = $2
  and f.id <> $3
  and f.status = 'scheduled'
  and f.departs_at >= $4
  and f.departs_at <= $5
order by f.departs_at`

	rows, err := s.pool.Query(ctx, q, origin, destination, excludeID, from, to)
	if err != nil {
		return nil, fmt.Errorf("candidate flights: %w", err)
	}
	defer rows.Close()

	var out []domain.Flight
	for rows.Next() {
		f, err := scanFlight(rows)
		if err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Move is one booking being reaccommodated onto another flight.
type Move struct {
	PNR          string
	ToFlightID   string
	ToCabin      domain.Cabin
	DelayMinutes int
	Reason       string
}

// ApplyReassignments moves bookings onto their new flights and records the
// audit trail, all in one transaction so a partial reaccommodation can never
// be committed.
//
// The booking keeps status 'confirmed', because the passenger is confirmed on
// the new flight. The reassignments table is what records that a move happened.
// Seat is cleared, since the old seat number means nothing on a new aircraft.
func (s *Store) ApplyReassignments(ctx context.Context, fromFlightID string, moves []Move) error {
	if len(moves) == 0 {
		return errors.New("no reassignments to apply")
	}
	fromFlightID = strings.TrimSpace(fromFlightID)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	targets := make([]string, 0, len(moves))
	seen := map[string]bool{}
	for _, m := range moves {
		tag, err := tx.Exec(ctx, `
update bookings
set flight_id = $2, cabin = $3, seat = null
where pnr = $1 and flight_id = $4 and status = 'confirmed'`,
			strings.ToUpper(strings.TrimSpace(m.PNR)), m.ToFlightID, string(m.ToCabin), fromFlightID)
		if err != nil {
			return fmt.Errorf("move %s: %w", m.PNR, err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("booking %s is not a confirmed booking on %s, nothing was applied", m.PNR, fromFlightID)
		}

		_, err = tx.Exec(ctx, `
insert into reassignments (pnr, from_flight_id, to_flight_id, delay_minutes, reason)
values ($1, $2, $3, $4, $5)`,
			strings.ToUpper(strings.TrimSpace(m.PNR)), fromFlightID, m.ToFlightID, m.DelayMinutes, m.Reason)
		if err != nil {
			return fmt.Errorf("record reassignment for %s: %w", m.PNR, err)
		}

		if !seen[m.ToFlightID] {
			seen[m.ToFlightID] = true
			targets = append(targets, m.ToFlightID)
		}
	}

	// The availability view sees uncommitted rows inside this transaction, so
	// this catches an oversell before it can be committed.
	rows, err := tx.Query(ctx, `
select flight_id, business_available, economy_available
from flight_availability
where flight_id = any($1) and (business_available < 0 or economy_available < 0)`, targets)
	if err != nil {
		return fmt.Errorf("oversell check: %w", err)
	}
	var oversold []string
	for rows.Next() {
		var id string
		var biz, eco int
		if err := rows.Scan(&id, &biz, &eco); err != nil {
			rows.Close()
			return err
		}
		oversold = append(oversold, fmt.Sprintf("%s (business %d, economy %d)", id, biz, eco))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(oversold) > 0 {
		return fmt.Errorf("would oversell %s, nothing was applied", strings.Join(oversold, "; "))
	}

	return tx.Commit(ctx)
}

// ReassignmentsForFlight returns the audit trail of moves off a flight.
func (s *Store) ReassignmentsForFlight(ctx context.Context, fromFlightID string) ([]domain.Reassignment, error) {
	const q = `
select id, pnr, from_flight_id, to_flight_id, delay_minutes, reason, created_at
from reassignments
where from_flight_id = $1
order by id`

	rows, err := s.pool.Query(ctx, q, strings.TrimSpace(fromFlightID))
	if err != nil {
		return nil, fmt.Errorf("reassignments for flight: %w", err)
	}
	defer rows.Close()

	var out []domain.Reassignment
	for rows.Next() {
		var r domain.Reassignment
		if err := rows.Scan(&r.ID, &r.PNR, &r.FromFlightID, &r.ToFlightID, &r.DelayMinutes, &r.Reason, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reassignment: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
