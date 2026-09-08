package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"r-project1/internal/airline/domain"
)

// These tests assert against the fixed rows in db/init/02_seed.sql. They are
// skipped unless DATABASE_URL points at a database loaded with that seed:
//
//	make db-up
//	make test-db
func testStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping store integration tests")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	return s, ctx
}

func day(t *testing.T, s string) *time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestSearchFlightsByRouteAndDate(t *testing.T) {
	s, ctx := testStore(t)

	got, err := s.SearchFlights(ctx, SearchParams{
		Origin: "GUA", Destination: "MEX", Date: day(t, "2026-09-14"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"AV201-2026-09-14", "AV203-2026-09-14", "AV205-2026-09-14"}
	if len(got) != len(want) {
		t.Fatalf("got %d flights, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d: got %s, want %s (results must be ordered by departure)", i, got[i].ID, id)
		}
	}
}

func TestSearchFlightsLowercaseCodesAreAccepted(t *testing.T) {
	s, ctx := testStore(t)

	got, err := s.SearchFlights(ctx, SearchParams{Origin: " gua ", Destination: "mex", Date: day(t, "2026-09-14")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d flights, want 3", len(got))
	}
}

func TestSearchFlightsWithoutDateSpansTheWholeSchedule(t *testing.T) {
	s, ctx := testStore(t)

	got, err := s.SearchFlights(ctx, SearchParams{Origin: "GUA", Destination: "MEX"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 9 {
		t.Fatalf("got %d flights, want 9 (3 daily departures across 3 seeded days)", len(got))
	}
}

func TestGetFlightReportsDerivedAvailability(t *testing.T) {
	s, ctx := testStore(t)

	cases := []struct {
		id                        string
		capacity, business        int
		taken, free, freeBiz, eco int
	}{
		{"AV201-2026-09-14", 20, 4, 18, 2, 0, 2},
		{"AV203-2026-09-14", 20, 4, 14, 6, 2, 4},
		{"AV205-2026-09-14", 16, 0, 11, 5, 0, 5},
		{"AV201-2026-09-15", 20, 4, 9, 11, 3, 8},
	}
	for _, c := range cases {
		f, err := s.GetFlight(ctx, c.id)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if f.Availability == nil {
			t.Fatalf("%s: availability not populated", c.id)
		}
		a := *f.Availability
		if f.Capacity != c.capacity || f.BusinessCapacity != c.business {
			t.Errorf("%s: capacity %d/%d, want %d/%d", c.id, f.Capacity, f.BusinessCapacity, c.capacity, c.business)
		}
		if a.SeatsTaken != c.taken || a.SeatsAvailable != c.free {
			t.Errorf("%s: taken %d free %d, want taken %d free %d", c.id, a.SeatsTaken, a.SeatsAvailable, c.taken, c.free)
		}
		if a.BusinessAvailable != c.freeBiz || a.EconomyAvailable != c.eco {
			t.Errorf("%s: business %d economy %d, want business %d economy %d",
				c.id, a.BusinessAvailable, a.EconomyAvailable, c.freeBiz, c.eco)
		}
		if f.Status != domain.FlightScheduled {
			t.Errorf("%s: status %q, want scheduled", c.id, f.Status)
		}
	}
}

func TestGetFlightUnknownIDIsNotFound(t *testing.T) {
	s, ctx := testStore(t)

	_, err := s.GetFlight(ctx, "NOPE-2026-01-01")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestFlightDurationAndLoadFactor(t *testing.T) {
	s, ctx := testStore(t)

	f, err := s.GetFlight(ctx, "AV201-2026-09-14")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Duration(); got != 150*time.Minute {
		t.Errorf("duration %v, want 2h30m", got)
	}
	if got := f.LoadFactor(); got != 0.9 {
		t.Errorf("load factor %v, want 0.9", got)
	}
}

func TestFindAirportsMatchesCityAndCode(t *testing.T) {
	s, ctx := testStore(t)

	got, err := s.FindAirports(ctx, "Guatemala")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Code != "GUA" {
		t.Fatalf("got %+v, want GUA first", got)
	}

	got, err = s.FindAirports(ctx, "mex")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Code != "MEX" {
		t.Fatalf("got %+v, want exact code match MEX first", got)
	}
}

func TestListAirports(t *testing.T) {
	s, ctx := testStore(t)

	got, err := s.ListAirports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("got %d airports, want 8", len(got))
	}
}

// These two need no database.

func TestNewRejectsAnEmptyDSN(t *testing.T) {
	if _, err := New(context.Background(), ""); err == nil {
		t.Fatal("expected an error for an empty dsn")
	}
}

func TestConnectionErrorsDoNotLeakThePassword(t *testing.T) {
	// The DSN reaches stderr and the log on a startup failure, so a password
	// must never survive into the error text.
	const password = "sup3rs3cret"
	_, err := New(context.Background(),
		"postgres://airline:"+password+"@nonexistent.invalid:5432/db?sslmode=bogus")
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("the password leaked into the error: %v", err)
	}
}
