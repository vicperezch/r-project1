package domain

// LoyaltyTier ranks a passenger for rebooking priority when a flight is
// disrupted and seats are scarce.
type LoyaltyTier string

const (
	TierNone     LoyaltyTier = "none"
	TierSilver   LoyaltyTier = "silver"
	TierGold     LoyaltyTier = "gold"
	TierPlatinum LoyaltyTier = "platinum"
)

// Rank orders tiers from 0 for an ordinary passenger up to 3 for platinum.
// The reassignment optimizer serves higher ranks first.
func (t LoyaltyTier) Rank() int {
	switch t {
	case TierPlatinum:
		return 3
	case TierGold:
		return 2
	case TierSilver:
		return 1
	default:
		return 0
	}
}

func (t LoyaltyTier) Valid() bool {
	switch t {
	case TierNone, TierSilver, TierGold, TierPlatinum:
		return true
	}
	return false
}

type Cabin string

const (
	CabinEconomy  Cabin = "economy"
	CabinBusiness Cabin = "business"
)

func (c Cabin) Valid() bool {
	return c == CabinEconomy || c == CabinBusiness
}

type FlightStatus string

const (
	FlightScheduled FlightStatus = "scheduled"
	FlightCancelled FlightStatus = "cancelled"
	FlightDeparted  FlightStatus = "departed"
)

func (s FlightStatus) Valid() bool {
	switch s {
	case FlightScheduled, FlightCancelled, FlightDeparted:
		return true
	}
	return false
}

type BookingStatus string

const (
	BookingConfirmed      BookingStatus = "confirmed"
	BookingCancelled      BookingStatus = "cancelled"
	BookingReaccommodated BookingStatus = "reaccommodated"
)

func (s BookingStatus) Valid() bool {
	switch s {
	case BookingConfirmed, BookingCancelled, BookingReaccommodated:
		return true
	}
	return false
}
