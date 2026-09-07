package domain

import "sort"

// SortByRebookingPriority orders passengers the way seats are handed out after
// a disruption: highest loyalty tier first, then the earliest booking, with the
// PNR as a final tie-break so the same input always produces the same order.
//
// This is the single definition of that order. Both the affected passenger
// listing and the reassignment optimizer use it, so what a rep is shown matches
// the order seats are actually assigned in.
func SortByRebookingPriority(bs []AffectedBooking) {
	sort.SliceStable(bs, func(i, j int) bool {
		a, b := bs[i], bs[j]
		if ra, rb := a.Passenger.LoyaltyTier.Rank(), b.Passenger.LoyaltyTier.Rank(); ra != rb {
			return ra > rb
		}
		if !a.Booking.CreatedAt.Equal(b.Booking.CreatedAt) {
			return a.Booking.CreatedAt.Before(b.Booking.CreatedAt)
		}
		return a.Booking.PNR < b.Booking.PNR
	})
}
