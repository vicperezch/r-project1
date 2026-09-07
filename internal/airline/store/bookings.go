package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"r-project1/internal/airline/domain"
)

// ErrAlreadyCancelled distinguishes a no-op cancellation from a failure, so
// the tool layer can say so instead of reporting an error.
var ErrAlreadyCancelled = errors.New("flight is already cancelled")

const affectedSelect = `
select b.pnr, b.passenger_id, b.flight_id, b.cabin, b.seat, b.status, b.created_at,
       p.id, p.full_name, p.email, p.loyalty_tier
from bookings b
join passengers p on p.id = b.passenger_id`

func scanAffected(rows pgx.Rows) (domain.AffectedBooking, error) {
	var ab domain.AffectedBooking
	err := rows.Scan(
		&ab.Booking.PNR, &ab.Booking.PassengerID, &ab.Booking.FlightID, &ab.Booking.Cabin,
		&ab.Booking.Seat, &ab.Booking.Status, &ab.Booking.CreatedAt,
		&ab.Passenger.ID, &ab.Passenger.FullName, &ab.Passenger.Email, &ab.Passenger.LoyaltyTier,
	)
	return ab, err
}

// BookingsForFlight returns the bookings on a flight joined with their
// passengers, already in rebooking priority order.
func (s *Store) BookingsForFlight(ctx context.Context, flightID string, includeNonConfirmed bool) ([]domain.AffectedBooking, error) {
	const q = affectedSelect + `
where b.flight_id = $1
  and ($2 or b.status = 'confirmed')
order by b.created_at, b.pnr`

	rows, err := s.pool.Query(ctx, q, strings.TrimSpace(flightID), includeNonConfirmed)
	if err != nil {
		return nil, fmt.Errorf("bookings for flight: %w", err)
	}
	defer rows.Close()

	var out []domain.AffectedBooking
	for rows.Next() {
		ab, err := scanAffected(rows)
		if err != nil {
			return nil, fmt.Errorf("scan booking: %w", err)
		}
		out = append(out, ab)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	domain.SortByRebookingPriority(out)
	return out, nil
}

func (s *Store) GetBooking(ctx context.Context, pnr string) (domain.AffectedBooking, error) {
	const q = affectedSelect + `
where b.pnr = upper($1)`

	rows, err := s.pool.Query(ctx, q, strings.TrimSpace(pnr))
	if err != nil {
		return domain.AffectedBooking{}, fmt.Errorf("get booking: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.AffectedBooking{}, err
		}
		return domain.AffectedBooking{}, fmt.Errorf("booking %q: %w", pnr, ErrNotFound)
	}
	ab, err := scanAffected(rows)
	if err != nil {
		return ab, fmt.Errorf("scan booking: %w", err)
	}
	return ab, nil
}

// CancelFlight marks a flight cancelled and records why. Bookings are left
// confirmed on purpose: those passengers still hold a claim to travel, and
// clearing them here would erase the very list the reassignment step needs.
func (s *Store) CancelFlight(ctx context.Context, flightID, reason string) (domain.Flight, error) {
	flightID = strings.TrimSpace(flightID)
	if strings.TrimSpace(reason) == "" {
		return domain.Flight{}, errors.New("a cancellation reason is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Flight{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `select status from flights where id = $1 for update`, flightID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Flight{}, fmt.Errorf("flight %q: %w", flightID, ErrNotFound)
	}
	if err != nil {
		return domain.Flight{}, fmt.Errorf("lock flight: %w", err)
	}
	if domain.FlightStatus(status) == domain.FlightCancelled {
		return domain.Flight{}, fmt.Errorf("flight %q: %w", flightID, ErrAlreadyCancelled)
	}

	_, err = tx.Exec(ctx, `
update flights
set status = 'cancelled', cancellation_reason = $2, cancelled_at = now()
where id = $1`, flightID, strings.TrimSpace(reason))
	if err != nil {
		return domain.Flight{}, fmt.Errorf("cancel flight: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Flight{}, fmt.Errorf("commit: %w", err)
	}

	return s.GetFlight(ctx, flightID)
}
