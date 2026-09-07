// Package domain holds the airline entities shared by the store, the services
// and the MCP tool layer.
package domain

import "time"

type Airport struct {
	Code string `json:"code"`
	City string `json:"city"`
	Name string `json:"name"`
}

// Availability mirrors the flight_availability view. Seat counts are derived
// from confirmed bookings rather than stored on the flight.
type Availability struct {
	SeatsTaken        int `json:"seats_taken"`
	SeatsAvailable    int `json:"seats_available"`
	BusinessAvailable int `json:"business_available"`
	EconomyAvailable  int `json:"economy_available"`
}

// AvailableIn reports the free seats in one cabin.
func (a Availability) AvailableIn(c Cabin) int {
	if c == CabinBusiness {
		return a.BusinessAvailable
	}
	return a.EconomyAvailable
}

type Flight struct {
	ID               string       `json:"id"`
	FlightNo         string       `json:"flight_no"`
	Origin           string       `json:"origin"`
	Destination      string       `json:"destination"`
	DepartsAt        time.Time    `json:"departs_at"`
	ArrivesAt        time.Time    `json:"arrives_at"`
	Aircraft         string       `json:"aircraft"`
	Capacity         int          `json:"capacity"`
	BusinessCapacity int          `json:"business_capacity"`
	Status           FlightStatus `json:"status"`

	// Set only when Status is cancelled.
	CancellationReason string     `json:"cancellation_reason,omitempty"`
	CancelledAt        *time.Time `json:"cancelled_at,omitempty"`

	// Availability is set only by queries that join flight_availability.
	Availability *Availability `json:"availability,omitempty"`
}

func (f Flight) Duration() time.Duration {
	return f.ArrivesAt.Sub(f.DepartsAt)
}

func (f Flight) LoadFactor() float64 {
	if f.Capacity == 0 || f.Availability == nil {
		return 0
	}
	return float64(f.Availability.SeatsTaken) / float64(f.Capacity)
}

type Passenger struct {
	ID          string      `json:"id"`
	FullName    string      `json:"full_name"`
	Email       string      `json:"email"`
	LoyaltyTier LoyaltyTier `json:"loyalty_tier"`
}

type Booking struct {
	PNR         string        `json:"pnr"`
	PassengerID string        `json:"passenger_id"`
	FlightID    string        `json:"flight_id"`
	Cabin       Cabin         `json:"cabin"`
	Seat        string        `json:"seat"`
	Status      BookingStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
}

// AffectedBooking is one passenger stranded by a disruption, joined with the
// details a rep needs to act on the case.
type AffectedBooking struct {
	Booking   Booking   `json:"booking"`
	Passenger Passenger `json:"passenger"`
}

type Reassignment struct {
	ID           int64     `json:"id"`
	PNR          string    `json:"pnr"`
	FromFlightID string    `json:"from_flight_id"`
	ToFlightID   string    `json:"to_flight_id"`
	DelayMinutes int       `json:"delay_minutes"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}
