# TV Guide — Project Context

A personal PWA TV guide that parses XMLTV data and displays it as a scrollable grid (channels × time). Deployed as a single Docker container behind Traefik + Authelia in a home-lab environment. Not a public application — security requirements are personal/internal only.

## Tech stack

| Layer | Choice | Reason |
|---|---|---|
| Backend | Go (one external dep: `modernc.org/sqlite`) | Low resource usage, single binary |
| Frontend | Vanilla JS + HTML/CSS (no framework, no build step) | Lightweight, no toolchain needed |
| Database | SQLite via `modernc.org/sqlite` (pure Go, no CGO) | Persistent storage, simple queries, no separate service |
| Container | Docker / docker-compose | Home-lab deployment |
| Reverse proxy | Traefik + Authelia | Handled externally, not in this repo |

## Project structure

```
tvguide/
├── main.go                      # Entry point: config, XMLTV poller, HTTP server, embed
├── go.mod                       # Module: github.com/acbgbca/xmltvguide
├── go.sum                       # Generated on first Docker build (commit after first build)
├── internal/
│   ├── xmltv/parser.go          # Fetch XMLTV from URL and parse into structs
│   ├── database/db.go           # SQLite store: schema, Refresh, GetChannels, GetAirings
│   └── api/handlers.go          # REST API handlers
├── web/                         # Frontend — embedded into binary via go:embed
│   ├── index.html               # App shell (no JS framework)
│   ├── app.js                   # All frontend logic
│   ├── style.css                # Dark theme, CSS grid layout
│   ├── manifest.json            # PWA manifest
│   └── sw.js                    # Service worker (cache-first static, network-first API)
├── Dockerfile                   # Multi-stage: golang:1.23-alpine → alpine:3.20
└── docker-compose.yml
```

## API

| Endpoint | Description |
|---|---|
| `GET /api/channels` | All channels in source order |
| `GET /api/guide?date=YYYY-MM-DD` | Airings overlapping the given date (local TZ). Defaults to today. |
| `GET /api/status` | Last refresh time, next refresh time, source URL |
| `GET /api/search?q=...&mode=...` | Search airings. `q` (required): search text. `mode`: `simple` (title only, default) or `advanced` (title+subtitle+description). Advanced-only params: `categories` (comma-separated), `include_past` (bool, default false). Shared: `include_repeats` (bool, default true). Returns results grouped by title. |
| `GET /api/categories` | Sorted JSON array of all distinct category strings |
| `GET /images/channel/{channel-id}` | Cached channel logo. Re-downloads from upstream if the local file is missing. Returns 404 if the channel has no icon. |
| `GET /` | Serves the embedded frontend (SPA shell) |
| `GET /{any}` | SPA fallback — serves `index.html` for any path not matching API, images, or static files. Enables client-side routing via History API. |

## Configuration (environment variables)

| Variable | Default | Description |
|---|---|---|
| `XMLTV_URL` | *(required)* | URL to poll for XMLTV data |
| `TZ` | *(required)* | Server timezone, e.g. `Australia/Melbourne`. Must match the user's local timezone so date boundaries resolve correctly. |
| `POLL_INTERVAL` | `12h` | How often to re-fetch the XMLTV file. Accepts Go duration strings: `1h`, `30m`, etc. |
| `RETENTION_DAYS` | `7` | Days of airing history to keep. Airings older than this are pruned on each refresh. |
| `DB_PATH` | `/data/tvguide.db` | Path to the SQLite database file. Mount `/data` as a Docker volume. |
| `IMAGE_CACHE_DIR` | `/data/images` | Directory for cached channel icon files. Files are stored at `{IMAGE_CACHE_DIR}/channels/{channel-id}.{ext}`. The directory is created on startup if absent. The `/data` volume already covers this path. |
| `PORT` | `8080` | HTTP listen port |

## How to build and run

```bash
# Run with docker compose (recommended — no Go install required on host)
docker compose up --build

# After the first successful build, commit go.mod and go.sum for reproducibility:
# docker cp <container>:/app/go.sum .   (or copy from build output)
```

Go does **not** need to be installed on the host. The `golang:1.23-alpine` build stage runs `go mod tidy` to resolve and download `modernc.org/sqlite`, then compiles the binary.

## Database

SQLite file at `DB_PATH` (default `/data/tvguide.db`). Mount `/data` as a named volume in docker-compose.

### Schema

```sql
channels (id PK, display_name, icon, sort_order, lcn, icon_url)
-- lcn:     logical channel number from <lcn> element (nullable); used for display ordering
-- icon:    local filesystem path of the cached icon file (e.g. /data/images/channels/ch1.png); NULL if not yet downloaded
-- icon_url: original external URL from the XMLTV source; used to detect URL changes and to re-download if the local file goes missing

airings (
  channel_id + start_time  -- composite PK
  stop_time, title, sub_title, description,
  categories               -- JSON array e.g. '["News","Sport"]'
  episode_num              -- xmltv_ns format: "1.2.0/1"
  episode_num_display      -- onscreen/SxxExx format: "S02E04"
  prog_id                  -- dd_progid (TMS/Gracenote stable ID, if present)
  star_rating, content_rating, year, icon, country,
  is_repeat, is_premiere
)

airings_fts  -- FTS5 virtual table for full-text search
  (channel_id, start_time, title, sub_title, description)
  -- Rebuilt on each Refresh(): cleared and repopulated from airings table
  -- Used by SearchSimple (title only) and SearchAdvanced (all text columns)

categories (name TEXT PRIMARY KEY)
  -- Distinct category values extracted from airings.categories JSON arrays
  -- Rebuilt on each Refresh()
```

### Refresh strategy

On each poll: upsert channels → `INSERT OR REPLACE` airings (composite PK handles duplicates) → prune airings older than `RETENTION_DAYS` → rebuild FTS index → rebuild categories table. All in one transaction.

Historical airings not present in the latest XMLTV file are preserved until they age out.

## Frontend layout constants

Two constants control the guide's visual layout. They must be kept in sync:

| Location | Name | Default | Effect |
|---|---|---|---|
| `web/app.js` line ~9 | `CONFIG.PX_PER_MIN` | `4` | Pixels per minute — controls horizontal zoom |
| `web/app.js` line ~10 | `CONFIG.ROW_HEIGHT` | `54` | Row height in px |
| `web/style.css` `:root` | `--row-height` | `54px` | Must match `CONFIG.ROW_HEIGHT` |

The guide renders the full day (1440 minutes) as a scrollable area and scrolls to the current time on load.

## Channel preferences

Stored client-side in `localStorage` under the key `tvguide-prefs`:

```json
{ "hidden": { "channel-id": true }, "favourites": { "channel-id": true } }
```

Favourites appear at the top of the guide. The remaining visible channels are sorted by `lcn` (logical channel number) when present; channels without an `lcn` fall back to source order. Hidden channels are excluded from the guide view entirely. Both are managed via the Settings tab (bottom navigation).

## Frontend navigation

The app uses a bottom navigation bar (iOS-style) with four tabs:

| Tab | Route | Description |
|---|---|---|
| Guide | `/` or `/guide` | The main TV guide grid (default) |
| Search | `/search` | Placeholder — search coming soon |
| Favourites | `/favourites` | Placeholder — favourites coming soon |
| Settings | `/settings` | Channel visibility and favourite toggles |

Navigation uses the **History API** (`pushState`/`popstate`) for client-side routing without full page reloads. The top bar (date display + prev/next day buttons) is only visible on the Guide tab. The Guide tab preserves its `?date=YYYY-MM-DD` query parameter behaviour.

The backend serves `index.html` for any unmatched path (SPA fallback via `spaHandler` in `main.go`), so direct navigation to `/search` or browser refresh on `/favourites` works correctly.

The now-line timer only runs when the Guide tab is active.

## Data flow

1. On startup, `main.go` opens the SQLite database and performs an initial XMLTV fetch.
2. A background goroutine re-fetches on every `POLL_INTERVAL` tick.
3. Each fetch calls `db.Refresh()`: upserts channels and airings, then prunes old data.
4. The frontend fetches `/api/channels` and `/api/guide?date=...` in parallel on page load.
5. Airings are rendered as absolutely-positioned cells within a CSS-grid layout.

## XMLTV notes

- Time format: `YYYYMMDDHHmmss ±HHMM` — the parser handles both `+1100` and `+11:00` offset styles.
- Large files (e.g. Melbourne.xml ~50MB) are parsed fully into memory on each poll, then written to SQLite in a single transaction.
- The HTTP request sets `Accept: text/xml, application/xml, */*` and `User-Agent: xmltvguide/1.0` — required because some XMLTV hosts return 406 without an explicit Accept header.

## Testing

### Test levels

The application has three levels of tests:

| Level | Scope | Dependencies |
|---|---|---|
| **Component tests** | Database and API logic in isolation | Real SQLite database; WireMock for external HTTP (XMLTV source) |
| **API integration tests** | Full API end-to-end | Real SQLite database; WireMock simulating the XMLTV source |
| **UI tests** | Full application E2E in a browser | Real API + database; WireMock simulating external services |

### Development approach

New features must be built using **TDD**:

1. Write the tests first (they will fail — that is expected).
2. Implement just enough code so that everything compiles but the tests still fail.
3. Iterate until all tests pass.

Do not skip ahead to writing implementation code before the tests exist.

### Documentation hygiene

After completing any change, verify that CLAUDE.md (and any other relevant docs) still accurately reflects the current state of the application. Update stale sections before closing the task — documentation drift is a bug.

## Deferred / future requirements

These were discussed and intentionally excluded from the MVP:

- **Search** — The `/search` tab is a placeholder. Future: search for programmes by title.
- **Favourite shows** — The `/favourites` tab is a placeholder. Future: track specific show titles and surface when they are scheduled.
- **Programmes table** — Split airings into a `programmes` table (one row per show) and an `airings` table (one row per broadcast). Deferred because XMLTV lacks a stable universal programme ID; the `prog_id` column (dd_progid) is the future deduplication key when available.
- **HDHomeRun integration** — Use the device's `http://[ip]/lineup.json` for channel data instead of XMLTV.
- **Notifications** — Alert when a tracked show is about to start.
