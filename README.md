# padel CLI

CLI tool for checking Playtomic padel court availability and booking.

## Install / Build

```bash
go build -o padel
```

## Nix

```bash
# Build with Nix flakes
nix build
./result/bin/padel-cli --help
```

## Openclaw Plugin

This repo exports an `openclawPlugin` flake output for nix-openclaw. nix-openclaw
symlinks skills into `~/.openclaw/workspace/skills/<skill>` and adds the plugin
packages to `PATH`, so no `skills.load.extraDirs` is needed.

## Usage

```bash
# List clubs near a location
padel clubs --near "Madrid"

# Check availability for a club on a date (dates are DD-MM-YYYY)
padel availability --club-id <id> --date 05-01-2025

# Search for available courts
padel search --location "Barcelona" --date 05-01-2025 --time 18:00-22:00

# JSON output
padel clubs --near "Madrid" --json
```

## Venue Management

Save venues with aliases for quick access:

```bash
# Add a venue
padel venues add --id "<playtomic-id>" --alias myclub --name "My Club" --indoor --timezone "Europe/Madrid"

# Set (or change) a member discount later, without re-adding the venue
padel venues update --alias myclub --discount 12

# List saved venues (shows the discount column)
padel venues list

# Use alias in commands
padel availability --venue myclub --date 05-01-2025

# Search multiple venues
padel search --venues myclub,otherclub --date 05-01-2025 --time 09:00-11:00
```

`--discount` is a flat euro amount subtracted from the court total before the
per-person split (see [Watch](#watch-for-openings-telegram-alerts)). Playtomic's
public feed only returns the rack price; if your account gets a fixed member
discount per booking it never appears in the feed, so you calibrate it once here.

## Booking History

```bash
# List upcoming bookings
padel bookings list

# List past bookings
padel bookings list --past

# Add a booking manually
padel bookings add --venue myclub --date 04-01-2025 --time 10:30 --court "Court 5" --price 42

# Sync from Playtomic account
padel bookings sync

# View stats
padel bookings stats
```

## Authentication

```bash
# Login to Playtomic
padel auth login --email you@example.com --password yourpass

# Check status
padel auth status

# Book a court (requires auth)
padel book --venue myclub --date 05-01-2025 --time 10:30 --duration 90
```

## Watch for Openings (Telegram alerts)

`padel watch` polls a venue/date/time window and alerts you whenever a slot opens up — for example when someone cancels. It is **notify-only**: it never books and never spends money. Targeting mirrors `padel search`.

```bash
# Watch a saved venue's weekday evening every 60s; alert on Telegram
padel watch --venues myclub --date 22-06-2026 --time 18:00-20:00 --interval 60s

# Watch several days at once
padel watch --venues myclub --date 22-06-2026,23-06-2026 --time 18:00-20:00

# Or watch by location / club id, only 90-minute slots
padel watch --location "Berlin" --date 22-06-2026 --time 18:00-20:00 --duration 90

# Subtract the Wellpass subsidy (9 € per person) from displayed prices
padel watch --venues myclub --date 22-06-2026 --time 18:00-20:00 --wellpass

# One-shot mode for Windows Task Scheduler / cron (persists state between runs)
padel watch --venues myclub --date 22-06-2026 --time 18:00-20:00 --once
```

In loop mode, watch sends a one-time **"watch started"** summary (venue, dates, time window, duration) to Telegram and the console at startup, then runs until you press Ctrl-C. Each poll only alerts on slots it hasn't alerted on before; if a slot disappears and later reappears, it alerts again. In `--once` mode there is no start message, and the alerted-slot set is persisted to `watch-state.json` in the config dir (`~/.config/padel`, or `PADEL_CONFIG_DIR`) so a scheduler doesn't re-alert the same slot.

**Prices** in alerts are shown **per person** in euros — the court price divided by 4, minus the venue's `--discount` (set via `venues add/update --discount`), and minus a further 9 € with `--wellpass` (clamped at 0). **Dates** in all output render as `Weekday DD.MM.` (e.g. `Thursday 09.07.`); input is `DD-MM-YYYY`.

Key flags: `--interval` (base loop cadence, default `3m`, actual cadence varies ±20% to avoid a robotic fixed heartbeat), `--once`, `--duration` (filter by slot length), `--indoor` / `--outdoor` (default is all courts), `--weekend`, `--wellpass`, `--telegram` (default on).

### Run watch in Docker

The bundled `docker-compose.yml` defines a long-running `watch` service that builds a Linux binary (no Go needed on the host) and reads all parameters from a `.env` file next to the compose file:

```env
PADEL_TZ=Europe/Berlin
WATCH_VENUES=myclub
WATCH_DATE=22-06-2026
WATCH_TIME=18:00-21:00
WATCH_DURATION=90
WATCH_INTERVAL=3m
WATCH_WELLPASS=false
```

```bash
docker compose up -d watch          # build + start
docker compose logs -f watch        # follow alerts
# After the watched date passes, edit WATCH_DATE in .env then:
docker compose up -d --force-recreate watch
```

Both services mount the same `~/.config/padel`, so `venues.json` (with per-venue discounts) and `telegram.json` are shared with the host CLI.

### Telegram setup

1. In Telegram, message **@BotFather** → `/newbot` → copy the **bot token**.
2. Message your new bot once (say "hi"), then open `https://api.telegram.org/bot<TOKEN>/getUpdates` and read `result[].message.chat.id` — that's your **chat id**.
3. Save both to `~/.config/padel/telegram.json` (created automatically if the config dir exists):

```json
{
  "bot_token": "123456:ABC...",
  "chat_id": "987654321"
}
```

The file is shared by `padel watch` and `padel auto-book` (when `notifications.telegram.enabled: true` in the auto-book YAML). If the file is absent or incomplete, `watch` falls back to printing alerts to the console instead of failing. Pass `--telegram=false` to force console-only.

## Autonomous Booking

`padel auto-book` is a strict personal automation for Indoor Padel Australia Alexandria. It computes the target date as the current date in `Australia/Sydney` plus 14 days, runs only for one of two configured profiles, and only books in the 18:30–18:35 Sydney release window.

### Doctrine

The strategy is **pre-grab privately at release, you decide within 48h**:

1. The bot races at 18:30 Sydney to grab a great slot the moment Playtomic releases it for non-gold members (14 days out).
2. The booking is **always private** — the bot never publishes a match to Playtomic's open feed, because publishing forfeits the free-cancel window.
3. Every confirmed booking includes a cancel deadline in the audit log and Telegram message: free cancel is allowed up to 48h before play. The bot also refuses to book any slot less than 72h away, so there's always a ≥24h margin above the cancel deadline.
4. Within 48h before play you either (a) invite 3 players in the Playtomic app and keep the booking, or (b) cancel for a full wallet refund.

The publish endpoint is structurally forbidden — `api/forbidden_test.go` fails CI if any future change adds a method that hits Playtomic's matches-publish path.

### Profiles

| Profile | Days | Start window | Duration | Caps (shared) |
|---|---|---|---|---|
| `weekday` (default) | Mon–Thu | 18:30–20:00 | 90 min | 1 per day, 3 per week |
| `weekend` | Sat–Sun | 10:00–18:00 | 90 or 120 min (prefers 90) | 1 per day, 3 per week |

The validator refuses any config that widens beyond the profile bounds.

### Setup

Configs live in `~/.config/padel/` alongside `credentials.json`, `venues.json`, and `bookings.db`. The dashboard (`padel serve`) and the CLI both look for them there.

```bash
mkdir -p ~/.config/padel
cp config.example.yaml ~/.config/padel/config.yaml       # weekday profile
cp config.example.yaml ~/.config/padel/config.weekend.yaml  # then edit mode/window/durations

# Confirm the Playtomic tenant id:
padel clubs --near "Alexandria NSW" --json

# Login once; credentials are stored under ~/.config/padel/credentials.json
padel auth login --email you@example.com

# Local dry-run test outside the release window
padel auto-book --config ~/.config/padel/config.yaml --ignore-release-window
```

`dry_run: true` is the default — the first runs only log what would happen. Flip to `false` only after both dry-run output and `auto_book_audit` rows look correct.

### Production schedule

Run both profiles at the release time in Sydney:

```cron
TZ=Australia/Sydney
30 18 * * * /path/to/padel auto-book --config /path/to/config.weekday.yaml >> /path/to/padel-auto-book.log 2>&1
30 18 * * * /path/to/padel auto-book --config /path/to/config.weekend.yaml >> /path/to/padel-auto-book.log 2>&1
```

Each command retries inside 18:30–18:35 with conservative 15–30 second polling. The weekday config no-ops on weekend target dates and vice versa, so running both is safe.

### Required settings

- `mode`: `weekday` or `weekend`. Each picks the right weekdays, window, and durations.
- `dry_run: false` only after dry-run output looks correct.
- `venue.id` must resolve to exactly `Indoor Padel Australia Alexandria`.
- `payment.method` defaults to `MERCHANT_WALLET`. Use another exact Playtomic method code only if you have independently confirmed that your account exposes it as a saved payment method.
- Optional Telegram notifications use `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` unless you change the env var names in config.
- Optional calendar conflict checks use `calendar.ical_url`; if any selected slot overlaps a VEVENT, that slot is skipped. Daily and weekly RRULE events are expanded; unsupported recurrence forms fail closed.

### Audit trail

Every decision is written to `~/.config/padel/bookings.db` in the `auto_book_audit` table; confirmed bookings land in the `bookings` table with `source: auto_booked`. The `booking_confirmed` audit event includes `cancel_deadline_local` and `cancel_deadline_utc` so you can sort by what needs your attention soonest.

### Release-window behaviour

The auto-book contract has two modes:

- **Scheduled (no `--ignore-release-window`)**: the binary polls every 10s waiting for the configured release window to open (default 18:30 Sydney), then enters the normal booking flow. After the window closes (default 18:35), it skips. This means the routine cron can be set a couple of minutes early without missing the window — the binary waits.
- **Manual (`--ignore-release-window`)**: the binary searches for any eligible slot regardless of time. Useful for opportunistic booking when slots come back onto the market via cancellations between release windows. All other safety guards (72h lead time, caps, venue verify, payment-challenge abort) still apply.

### Safety behaviour

- Refuses config that widens venue, timezone, release timing, profile weekdays, profile start window, allowed durations, or booking caps.
- Refuses to book any slot less than 72h away (defense in depth above today+14).
- Syncs Playtomic bookings before checking the per-day and per-week caps.
- Stops and notifies on missing/expired login that cannot be refreshed, venue mismatch, iCalendar errors, payment method mismatch, checkout challenge indicators, or an unexpected confirmation payload.
- Does not implement CAPTCHA, MFA, 3DS, or payment-challenge bypasses.
- Does not publish matches; the publish endpoint is structurally forbidden.

### Why no SPLIT / open-match support

Playtomic's iOS app can create open matches that only charge each player their share, but the customer-match endpoints exposed to public bearer tokens only accept the `SINGLE_PAYER` shape — the full court is charged to the booking owner. Probes for `split_payment_parts`, `payment_plan: SPLIT`, multi-registration payloads, and direct `PATCH` on the match's `payment_type` all silently dropped the field or returned 500/403. The SPLIT mechanism appears to live in an internal endpoint not reachable with our token. Until that changes, the safest play is what's documented above: book privately, decide within 48h.

## Dashboard

`padel serve` runs a small web dashboard that reads the same `~/.config/padel/bookings.db` the CLI writes to. Three pages:

- **Dashboard** (`/`) — upcoming bookings with the cancel deadline coloured by urgency (red <24h, amber <72h).
- **Audit** (`/audit`) — every `auto_book_audit` event, grouped by run, filterable by run id and level.
- **Run** (`/run`) — trigger a dry-run of either profile; live output streams via SSE.

```bash
# Standalone (host process, defaults to 127.0.0.1:8080)
padel serve

# LAN-accessible
padel serve --bind 0.0.0.0 --port 8080
```

### Docker

```bash
docker compose up -d
# Dashboard at http://127.0.0.1:8080
# Compose mounts $HOME/.config/padel into the container so it sees the same DB
# and credentials the host CLI uses. Override PADEL_CONFIG_DIR to mount a
# different directory.
```

The container binds to 0.0.0.0:8080 internally but compose only publishes it on `127.0.0.1` on the host — flip the port mapping in `docker-compose.yml` to expose on your LAN.

## Indoor/Outdoor Filtering

Default shows **all** courts (like the Playtomic app). Narrow with `--indoor` or `--outdoor`. Note: Playtomic types courts as `indoor`, `outdoor`, or `roofed`; only `indoor` counts as indoor, so a roofed/covered court (e.g. a "Kalthalle") is grouped with `--outdoor`.

```bash
# All courts (default)
padel search --venues myclub --date 05-01-2025

# Indoor only
padel search --venues myclub --date 05-01-2025 --indoor

# Outdoor only (includes roofed/covered courts)
padel search --venues myclub --date 05-01-2025 --outdoor
```

## Output Formats

- Default: human-readable tables
- `--json`: structured JSON output
- `--compact`: single-line summaries (useful for chat bots)

## Configuration

Config stored in `~/.config/padel/`:

```
~/.config/padel/
├── config.json          # preferences
├── credentials.json     # auth tokens
├── venues.json          # saved venues
└── bookings.db          # SQLite booking history
```

Environment overrides:

- `PADEL_CONFIG_DIR`: override the config directory (defaults to `~/.config/padel`)
- `XDG_CONFIG_HOME`: used if set and `PADEL_CONFIG_DIR` is not set
- `PADEL_AUTH_FILE`: default for `padel auth login --auth-file`

Example config.json:

```json
{
  "default_location": "Madrid",
  "favourite_clubs": [
    {"id": "abc123", "alias": "myclub"}
  ],
  "preferred_times": ["18:00", "19:30"],
  "preferred_duration": 90
}
```

## API Notes

Uses Playtomic API endpoints reverse-engineered from:
- https://mattrighetti.com/2025/03/03/reverse-engineering-playtomic
- https://github.com/ypk46/playtomic-scheduler

## License

MIT
