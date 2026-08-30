-- Airline assistant schema. Loaded by the postgres image on first boot of an
-- empty data volume, in file name order. No migration tool by design.

create table airports (
    code text primary key check (code ~ '^[A-Z]{3}$'),
    city text not null,
    name text not null
);

create table flights (
    id text primary key,
    flight_no text not null,
    origin text not null references airports (code),
    destination text not null references airports (code),
    departs_at timestamptz not null,
    arrives_at timestamptz not null,
    aircraft text not null,
    capacity int not null check (capacity > 0),
    -- Business seats are a subset of capacity. Tracked separately so the
    -- reassignment optimizer can tell a downgrade from a same-cabin move.
    business_capacity int not null default 0 check (business_capacity >= 0),
    status text not null default 'scheduled'
        check (status in ('scheduled', 'cancelled', 'departed')),
    constraint flights_business_fits_capacity check (business_capacity <= capacity),
    constraint flights_arrives_after_departs check (arrives_at > departs_at),
    constraint flights_distinct_endpoints check (origin <> destination)
);

create index flights_route_idx on flights (origin, destination, departs_at);
create index flights_status_idx on flights (status);

create table passengers (
    id text primary key,
    full_name text not null,
    email text not null,
    loyalty_tier text not null default 'none'
        check (loyalty_tier in ('none', 'silver', 'gold', 'platinum'))
);

create table bookings (
    pnr text primary key check (pnr ~ '^[A-Z0-9]{6}$'),
    passenger_id text not null references passengers (id),
    flight_id text not null references flights (id),
    cabin text not null check (cabin in ('economy', 'business')),
    seat text,
    status text not null default 'confirmed'
        check (status in ('confirmed', 'cancelled', 'reaccommodated')),
    created_at timestamptz not null default now(),
    -- A passenger cannot hold two seats on the same flight.
    constraint bookings_one_per_passenger_per_flight unique (passenger_id, flight_id)
);

create index bookings_flight_idx on bookings (flight_id, status);
create index bookings_passenger_idx on bookings (passenger_id);

create table reassignments (
    id bigserial primary key,
    pnr text not null references bookings (pnr),
    from_flight_id text not null references flights (id),
    to_flight_id text not null references flights (id),
    delay_minutes int not null,
    reason text not null,
    created_at timestamptz not null default now()
);

create index reassignments_pnr_idx on reassignments (pnr);

-- Seat availability is derived, never stored, so it cannot drift from bookings.
create view flight_availability as
select
    f.id as flight_id,
    f.capacity,
    f.business_capacity,
    f.capacity - f.business_capacity as economy_capacity,
    count(b.pnr) filter (where b.status = 'confirmed') as seats_taken,
    count(b.pnr) filter (where b.status = 'confirmed' and b.cabin = 'business') as business_taken,
    count(b.pnr) filter (where b.status = 'confirmed' and b.cabin = 'economy') as economy_taken,
    f.capacity - count(b.pnr) filter (where b.status = 'confirmed') as seats_available,
    f.business_capacity - count(b.pnr) filter (where b.status = 'confirmed' and b.cabin = 'business') as business_available,
    (f.capacity - f.business_capacity) - count(b.pnr) filter (where b.status = 'confirmed' and b.cabin = 'economy') as economy_available
from flights f
left join bookings b on b.flight_id = f.id
group by f.id;
