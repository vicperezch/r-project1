"""Regenerates db/init/02_seed.sql.

Run from the repo root: python3 db/gen_seed.py

The seed is written by hand-tuned targets rather than free random data because
the demo depends on exact seat math: the cancelled flight must have more
displaced passengers than there are same day seats on the alternatives.
"""

import random
from datetime import datetime, timedelta, timezone

R = random.Random(20260914)
UTC = timezone.utc

AIRPORTS = [
    ("GUA", "Guatemala City", "La Aurora International"),
    ("MEX", "Mexico City", "Benito Juarez International"),
    ("SAL", "San Salvador", "El Salvador International"),
    ("SJO", "San Jose", "Juan Santamaria International"),
    ("PTY", "Panama City", "Tocumen International"),
    ("BOG", "Bogota", "El Dorado International"),
    ("MIA", "Miami", "Miami International"),
    ("LAX", "Los Angeles", "Los Angeles International"),
]

# flight_no, origin, dest, dep hh:mm, duration minutes, capacity, business capacity
SCHEDULE = [
    ("AV201", "GUA", "MEX", "06:00", 150, 20, 4),
    ("AV203", "GUA", "MEX", "12:15", 150, 20, 4),
    ("AV205", "GUA", "MEX", "18:30", 150, 16, 0),
    ("AV202", "MEX", "GUA", "09:00", 150, 20, 4),
    ("AV204", "MEX", "GUA", "16:00", 150, 16, 0),
    ("AV310", "GUA", "SAL", "07:00", 50, 16, 0),
    ("AV312", "GUA", "SAL", "15:00", 50, 16, 0),
    ("AV420", "GUA", "MIA", "08:00", 160, 24, 6),
    ("AV422", "GUA", "MIA", "17:30", 160, 20, 4),
    ("AV421", "MIA", "GUA", "11:00", 160, 24, 6),
    ("AV530", "GUA", "PTY", "13:00", 125, 20, 4),
    ("AV531", "PTY", "BOG", "16:30", 80, 20, 4),
    ("AV640", "SJO", "GUA", "10:00", 105, 16, 0),
    ("AV750", "GUA", "LAX", "09:30", 305, 24, 6),
]

DAYS = ["2026-09-14", "2026-09-15", "2026-09-16"]

flights = {}
for day in DAYS:
    for no, org, dst, hhmm, dur, cap, biz in SCHEDULE:
        dep = datetime.strptime(f"{day} {hhmm}", "%Y-%m-%d %H:%M").replace(tzinfo=UTC)
        fid = f"{no}-{day}"
        flights[fid] = {
            "id": fid, "no": no, "org": org, "dst": dst,
            "dep": dep, "arr": dep + timedelta(minutes=dur),
            "cap": cap, "biz": biz,
            "aircraft": "A320" if cap >= 20 else "E190",
        }

FIRST = ["Ana","Carlos","Maria","Jose","Lucia","Diego","Sofia","Miguel","Valeria","Andres",
         "Camila","Ricardo","Paula","Fernando","Isabel","Javier","Daniela","Rodrigo","Elena","Hugo",
         "Natalia","Pablo","Adriana","Sergio","Gabriela","Emilio","Renata","Tomas","Mariana","Felipe"]
LAST = ["Garcia","Morales","Hernandez","Lopez","Ramirez","Castillo","Mendez","Ortega","Vargas","Rivera",
        "Aguilar","Solis","Navarro","Bonilla","Cabrera","Delgado","Estrada","Fuentes","Guzman","Herrera"]

TIERS = ["platinum"] * 4 + ["gold"] * 8 + ["silver"] * 14 + ["none"] * 34
R.shuffle(TIERS)

passengers = []
used_names = set()
i = 0
while len(passengers) < 60:
    name = f"{FIRST[i % len(FIRST)]} {LAST[(i * 7) % len(LAST)]}"
    i += 1
    if name in used_names:
        continue
    used_names.add(name)
    n = len(passengers) + 1
    passengers.append({
        "id": f"PAX{n:03d}",
        "name": name,
        "email": name.lower().replace(" ", ".") + "@example.com",
        "tier": TIERS[n - 1],
    })

by_tier = {t: [p for p in passengers if p["tier"] == t] for t in ("platinum", "gold", "silver", "none")}

PNR_CHARS = "ABCDEFGHJKLMNPQRSTUVWXYZ0123456789"
seen_pnr = set()
def new_pnr():
    while True:
        p = "".join(R.choice(PNR_CHARS) for _ in range(6))
        if p not in seen_pnr:
            seen_pnr.add(p)
            return p

BASE = datetime(2026, 7, 1, 12, 0, tzinfo=UTC)
bookings = []
seat_ctr = {}

def add_booking(pax, fid, cabin):
    key = (fid, cabin)
    n = seat_ctr.get(key, 0)
    seat_ctr[key] = n + 1
    seat = f"{1 + n // 4}{'ABCD'[n % 4]}" if cabin == "business" else f"{10 + n // 6}{'ABCDEF'[n % 6]}"
    bookings.append({
        "pnr": new_pnr(), "pax": pax["id"], "flight": fid, "cabin": cabin, "seat": seat,
        "created": BASE + timedelta(hours=len(bookings) * 3 + R.randint(0, 2)),
    })

def pick(pool, used, k):
    out = []
    for p in pool:
        if len(out) == k:
            break
        if p["id"] not in used:
            out.append(p)
            used.add(p["id"])
    return out

# Demo scenario. AV201 on the 14th is the flight meant to be cancelled.
# 18 of 20 seats sold, 4 of them business.
used = set()
cancel_pax = (pick(by_tier["platinum"], used, 2) + pick(by_tier["gold"], used, 3)
              + pick(by_tier["silver"], used, 4) + pick(by_tier["none"], used, 9))
business_holders = {cancel_pax[0]["id"], cancel_pax[1]["id"], cancel_pax[2]["id"], cancel_pax[3]["id"]}
for p in cancel_pax:
    add_booking(p, "AV201-2026-09-14", "business" if p["id"] in business_holders else "economy")

# Same day alternatives, deliberately left with partial availability.
# AV203 frees 2 business and 4 economy, AV205 frees 5 economy.
def fill(fid, n_biz, n_eco, exclude):
    used2 = set(exclude)
    pool = [p for p in passengers if p["id"] not in used2]
    R.shuffle(pool)
    chosen = pool[: n_biz + n_eco]
    for j, p in enumerate(chosen):
        add_booking(p, fid, "business" if j < n_biz else "economy")

fill("AV203-2026-09-14", 2, 12, {p["id"] for p in cancel_pax[:0]})
fill("AV205-2026-09-14", 0, 11, set())
# Next morning, the overflow valve: 3 business and 8 economy left open.
fill("AV201-2026-09-15", 1, 8, set())

# Filler traffic so the database is not only about one route.
other = [f for f in flights.values()
         if f["id"] not in ("AV201-2026-09-14", "AV203-2026-09-14", "AV205-2026-09-14", "AV201-2026-09-15")]
other.sort(key=lambda f: f["id"])
for f in other:
    n = R.randint(0, 2)
    if n:
        fill(f["id"], 1 if f["biz"] and R.random() < 0.4 else 0, n, set())

def ts(d):
    return d.strftime("%Y-%m-%d %H:%M:%S+00")

out = []
w = out.append
w("-- Seed data for the airline assistant. Generated by db/gen_seed.py, which is")
w("-- deterministic: rerunning it reproduces this file byte for byte.")
w("--")
w("-- All timestamps are UTC.")
w("--")
w("-- Demo scenario, used by the irregularity and reassignment tools:")
w("--   AV201-2026-09-14  GUA to MEX 06:00, 20 seats, 18 sold (4 business). Cancel this one.")
w("--   AV203-2026-09-14  same route 12:15, leaves 2 business and 4 economy free.")
w("--   AV205-2026-09-14  same route 18:30, leaves 5 economy free, no business cabin.")
w("--   AV201-2026-09-15  next morning, leaves 3 business and 8 economy free.")
w("-- 18 displaced passengers against 11 same day seats forces the optimizer to")
w("-- push the lowest priority 7 to the next morning.")
w("")
w("insert into airports (code, city, name) values")
w(",\n".join(f"    ('{c}', '{city}', '{n}')" for c, city, n in AIRPORTS) + ";")
w("")
w("insert into flights (id, flight_no, origin, destination, departs_at, arrives_at, aircraft, capacity, business_capacity) values")
rows = [f"    ('{f['id']}', '{f['no']}', '{f['org']}', '{f['dst']}', '{ts(f['dep'])}', '{ts(f['arr'])}', '{f['aircraft']}', {f['cap']}, {f['biz']})"
        for f in sorted(flights.values(), key=lambda x: (x["dep"], x["no"]))]
w(",\n".join(rows) + ";")
w("")
w("insert into passengers (id, full_name, email, loyalty_tier) values")
w(",\n".join(f"    ('{p['id']}', '{p['name']}', '{p['email']}', '{p['tier']}')" for p in passengers) + ";")
w("")
w("insert into bookings (pnr, passenger_id, flight_id, cabin, seat, created_at) values")
brows = [f"    ('{b['pnr']}', '{b['pax']}', '{b['flight']}', '{b['cabin']}', '{b['seat']}', '{ts(b['created'])}')"
         for b in bookings]
w(",\n".join(brows) + ";")
w("")

with open("db/init/02_seed.sql", "w") as fh:
    fh.write("\n".join(out))

from collections import Counter
cnt = Counter(b["flight"] for b in bookings)
print(f"airports={len(AIRPORTS)} flights={len(flights)} passengers={len(passengers)} bookings={len(bookings)}")
for fid in ("AV201-2026-09-14", "AV203-2026-09-14", "AV205-2026-09-14", "AV201-2026-09-15"):
    f = flights[fid]
    b = [x for x in bookings if x["flight"] == fid]
    nb = sum(1 for x in b if x["cabin"] == "business")
    ne = len(b) - nb
    print(f"{fid} cap={f['cap']}(biz {f['biz']}) sold={len(b)} biz={nb} eco={ne} "
          f"free_biz={f['biz']-nb} free_eco={f['cap']-f['biz']-ne}")
