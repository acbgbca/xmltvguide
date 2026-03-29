# TV Guide — Project Context

A personal PWA TV guide that parses XMLTV data and displays it as a scrollable grid (channels × time). Deployed as a single Docker container behind Traefik + Authelia in a home-lab environment. Not a public application — security requirements are personal/internal only.

## Tech stack

| Layer | Choice | Reason |
|---|---|---|
| Backend | Go (stdlib only, no external deps) | Low resource usage, single binary |
| Frontend | Vanilla JS + HTML/CSS (no framework, no build step) | Lightweight, no toolchain needed |
| Container | Docker / docker-compose | Home-lab deployment |
| Reverse proxy | Traefik + Authelia | Handled externally, not in this repo |

## Project structure

```
tvguide/
├── main.go                   # Entry point: config, XMLTV poller, HTTP server, embed
├── go.mod                    # Module: github.com/acbgbca/xmltvguide
├── internal/
│   ├── xmltv/parser.go       # Fetch XMLTV from URL and parse into structs
│   ├── store/store.go        # Thread-safe in-memory channel + programme store
│   └── api/handlers.go       # REST API handlers
├── web/                      # Frontend — embedded into binary via go:embed
│   ├── index.html            # App shell (no JS framework)
│   ├── app.js                # All frontend logic
│   ├── style.css             # Dark theme, CSS grid layout
│   ├── manifest.json         # PWA manifest
│   └── sw.js                 # Service worker (cache-first static, network-first API)
├── Dockerfile                # Multi-stage: golang:1.23-alpine → alpine:3.20
└── docker-compose.yml
```

## API

| Endpoint | Description |
|---|---|
| `GET /api/channels` | All channels in source order |
| `GET /api/guide?date=YYYY-MM-DD` | Programmes overlapping the given date (local TZ). Defaults to today. |
| `GET /api/status` | Last refresh time, next refresh time, source URL |
| `GET /` | Serves the embedded frontend |

## Configuration (environment variables)

| Variable | Default | Description |
|---|---|---|
| `XMLTV_URL` | *(required)* | URL to poll for XMLTV data |
| `TZ` | *(required)* | Server timezone, e.g. `Australia/Melbourne`. Must match the user's local timezone so date boundaries resolve correctly. |
| `POLL_INTERVAL` | `12h` | How often to re-fetch the XMLTV file. Accepts Go duration strings: `1h`, `30m`, etc. |
| `PORT` | `8080` | HTTP listen port |

## How to build and run

```bash
# Run with docker compose (recommended)
docker compose up --build

# Or build and run the binary directly (requires Go 1.23+)
go build -o tvguide .
XMLTV_URL=http://... TZ=Australia/Melbourne ./tvguide
```

## Frontend layout constants

Two constants control the guide's visual layout. They must be kept in sync:

| Location | Name | Default | Effect |
|---|---|---|---|
| `web/app.js` line ~9 | `CONFIG.PX_PER_MIN` | `4` | Pixels per minute — controls horizontal zoom |
| `web/app.js` line ~10 | `CONFIG.ROW_HEIGHT` | `54` | Row height in px |
| `web/style.css` `:root` | `--row-height` | `54px` | Must match `CONFIG.ROW_HEIGHT` |

The guide renders the full day (1440 minutes) as a scrollable area and scrolls to the current time on load. The visible window is whatever fits the viewport at `PX_PER_MIN`.

## Channel preferences

Stored client-side in `localStorage` under the key `tvguide-prefs`:

```json
{ "hidden": { "channel-id": true }, "favourites": { "channel-id": true } }
```

Favourites appear at the top of the guide, above the regular channel order. Hidden channels are excluded from the guide view entirely. Both are managed via the "Channels" slide-out panel.

## Data flow

1. On startup, `main.go` fetches and parses the XMLTV URL, populating the in-memory store.
2. A background goroutine re-fetches on every `POLL_INTERVAL` tick.
3. The frontend fetches `/api/channels` and `/api/guide?date=...` in parallel on page load.
4. Programmes are rendered as absolutely-positioned cells within a CSS-grid layout.

## XMLTV notes

- Time format: `YYYYMMDDHHmmss ±HHMM` — the parser handles both `+1100` and `+11:00` offset styles.
- Large files (e.g. Melbourne.xml ~50MB) are parsed fully into memory on each poll. This is intentional and acceptable for the polling frequency used.
- The HTTP request sets `Accept: text/xml, application/xml, */*` and `User-Agent: xmltvguide/1.0` — required because some XMLTV hosts return 406 without an explicit Accept header.

## Deferred / future requirements

These were discussed and intentionally excluded from the MVP:

- **Multi-day navigation** — API already supports `?date=YYYY-MM-DD`; only the frontend nav buttons need enabling.
- **HDHomeRun integration** — Use the device's `http://[ip]/lineup.json` for channel data instead of XMLTV.
- **Favourite shows** — Track specific show titles and surface when they are scheduled.
- **Notifications** — Alert when a tracked show is about to start.
