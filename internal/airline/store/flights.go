package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"r-project1/internal/airline/domain"
)

const flightSelect = `
select f.id, f.flight_no, f.origin, f.destination, f.departs_at, f.arrives_at,
       f.aircraft, f.capacity, f.business_capacity, f.status,
       coalesce(f.cancellation_reason, ''), f.cancelled_at,
       a.seats_taken, a.seats_available, a.business_available, a.economy_available
from flights f
join flight_availability a on a.flight_id = f.id`

func scanFlight(row pgx.Row) (domain.Flight, error) {
	var f domain.Flight
	var av domain.Availability
	err := row.Scan(
		&f.ID, &f.FlightNo, &f.Origin, &f.Destination, &f.DepartsAt, &f.ArrivesAt,
		&f.Aircraft, &f.Capacity, &f.BusinessCapacity, &f.Status,
		&f.CancellationReason, &f.CancelledAt,
		&av.SeatsTaken, &av.SeatsAvailable, &av.BusinessAvailable, &av.EconomyAvailable,
	)
	if err != nil {
		return f, err
	}
	f.Availability = &av
	return f, nil
}

type SearchParams struct {
	Origin      string
	Destination string
	// Date is optional. When set, only flights departing on that UTC calendar
	// day are returned.
	Date             *time.Time
	IncludeCancelled bool
	Limit            int
}

func (s *Store) SearchFlights(ctx context.Context, p SearchParams) ([]domain.Flight, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	// Passed as a string rather than a time so the comparison cannot shift with
	// the server timezone.
	var day any
	if p.Date != nil {
		day = p.Date.UTC().Format("2006-01-02")
	}

	const q = flightSelect + `
where f.origin = $1
  and f.destination = $2
  and ($3::date is null or (f.departs_at at time zone 'UTC')::date = $3::date)
  and ($4 or f.status <> 'cancelled')
order by f.departs_at
limit $5`

	rows, err := s.pool.Query(ctx, q,
		strings.ToUpper(strings.TrimSpace(p.Origin)),
		strings.ToUpper(strings.TrimSpace(p.Destination)),
		day, p.IncludeCancelled, p.Limit)
	if err != nil {
		return nil, fmt.Errorf("search flights: %w", err)
	}
	defer rows.Close()

	var out []domain.Flight
	for rows.Next() {
		f, err := scanFlight(rows)
		if err != nil {
			return nil, fmt.Errorf("scan flight: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) GetFlight(ctx context.Context, id string) (domain.Flight, error) {
	const q = flightSelect + `
where f.id = $1`

	f, err := scanFlight(s.pool.QueryRow(ctx, q, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return f, fmt.Errorf("flight %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return f, fmt.Errorf("get flight: %w", err)
	}
	return f, nil
}

// FindAirports resolves what a rep typed, which may be a code or a city name,
// into airport rows. Exact code matches sort first.
func (s *Store) FindAirports(ctx context.Context, query string) ([]domain.Airport, error) {
	const q = `
select code, city, name
from airports
where code = upper($1) or city ilike '%' || $1 || '%' or name ilike '%' || $1 || '%'
order by (code = upper($1)) desc, city
limit 10`

	rows, err := s.pool.Query(ctx, q, strings.TrimSpace(query))
	if err != nil {
		return nil, fmt.Errorf("find airports: %w", err)
	}
	defer rows.Close()

	var out []domain.Airport
	for rows.Next() {
		var a domain.Airport
		if err := rows.Scan(&a.Code, &a.City, &a.Name); err != nil {
			return nil, fmt.Errorf("scan airport: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListAirports(ctx context.Context) ([]domain.Airport, error) {
	rows, err := s.pool.Query(ctx, `select code, city, name from airports order by code`)
	if err != nil {
		return nil, fmt.Errorf("list airports: %w", err)
	}
	defer rows.Close()

	var out []domain.Airport
	for rows.Next() {
		var a domain.Airport
		if err := rows.Scan(&a.Code, &a.City, &a.Name); err != nil {
			return nil, fmt.Errorf("scan airport: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
