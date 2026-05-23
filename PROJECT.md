# jacktasks

A personal CLI/TUI task tracker built around a modified Pomodoro flow, designed for ADHD-friendly capture-and-defer workflows, with cross-device sync via a self-hosted backend.

## What it is

jacktasks tracks time spent on categories of work and sub-projects, encouraging structured work blocks with optional breaks. It's designed around three real ADHD-driven problems:

1. **Forgetting tasks during a work session.** The `upn` command captures a thought and defers it to session end (see "Session loop" below), so the user doesn't break flow to act on it immediately.
2. **Capturing on phone, acting on a laptop.** Apple Reminders is the phone-side capture surface; jacktasks pulls from a dedicated list (`jacktasks-inbox`) when starting sessions on a Mac.
3. **Improvisational project work.** Projects can be pre-populated or added ad-hoc at session start.

## Architecture

Two binaries, two stores, two sync layers — deliberately separated.

```
┌───────────┐   ┌───────────┐
│ MacBook   │   │ Mac Mini  │
│ jacktasks │   │ jacktasks │
│ + SQLite  │   │ + SQLite  │
└─────┬─────┘   └─────┬─────┘
      │  manual sync  │
      │  (Tailscale)  │
      ▼               ▼
   ┌─────────────────────┐
   │ Home Ubuntu server  │
   │ jacktasks-sync      │
   │ + SQLite (master)   │
   └─────────────────────┘

      ┌──────────────────────────────┐
      │ Apple Reminders              │
      │ (list: jacktasks-inbox)      │
      │ synced by Apple iCloud       │
      └──────────────────────────────┘
              ▲          ▲
              │          │
           iPhone    Macs (via go-eventkit)
```

**Sync layer 1 — Reminders → both Macs:** handled by iCloud. jacktasks reads from the dedicated `jacktasks-inbox` list at project-selection time. No custom sync code involved.

**Sync layer 2 — jacktasks session data across Macs:** handled by a custom Go HTTP service on the home Ubuntu server. Manual `jacktasks sync` command, last-write-wins for category/project edits, pure-append for sessions/captures.

## Tech stack

| Layer | Choice | Why |
|---|---|---|
| Language | Go 1.24+ | Single-binary distribution, static typing, idiomatic LLM-generated code, great TUI ecosystem |
| TUI (planned) | Bubble Tea + Lipgloss + Bubbles | Purpose-built for stateful TUIs with persistent components |
| Local DB | SQLite via `modernc.org/sqlite` (pure Go) | Limits cgo to EventKit; analyzable in pandas/DBeaver |
| Apple Reminders | `github.com/BRO3886/go-eventkit` | Direct in-process EventKit via cgo |
| Cross-device sync | Custom Go HTTP service on Ubuntu home server, behind Tailscale | Self-hosted, minimal protocol |
| Sync server DB | SQLite | Same engine, simpler than Postgres for V1 scale |
| Deploy | Build on Mac, `scp` binary to NAS | No GitHub credentials on the NAS; smaller attack surface |

## Filesystem layout

| Path | Contents |
|---|---|
| `/usr/local/bin/jacktasks` | Mac binary |
| `~/Library/Application Support/jacktasks/jacktasks.db` | local SQLite |
| `~/Library/Application Support/jacktasks/active.json` | crash-recovery sentinel (Phase 5) |
| `~/.config/jacktasks/config.toml` | user-editable config (planned; not yet needed) |

## Schema

All tables use UUID primary keys (TEXT) for sync-friendliness. Timestamps are Unix epoch seconds. `journal_mode=DELETE` (rollback journal, not WAL). Foreign keys enforced via `PRAGMA foreign_keys=ON`.

```sql
projects    (id, name, created_at, updated_at, deleted_at?, archived)
categories  (id, name, project_id?→projects, created_at, updated_at, deleted_at?, archived)
sessions    (id, project_id?→projects, category_id→categories NOT NULL,
             planned_duration_min, actual_duration_sec, started_at, ended_at,
             end_notes?, status, created_at, device_id)
captures    (id, session_id→sessions, text, captured_at,
             cleared, sent_to_reminders, created_at)
sync_state  (table_name, last_pull_at?, last_push_at?)
config      (key, value)
```

Sessions are written once on session end — never edited. Captures get two single-flag updates (cleared, sent_to_reminders) but no other mutations.

**Projects** are the top-level grouping. **Categories** are per-project sub-labels (e.g. "Coding", "Planning") scoped to a specific project. A category's `project_id` is nullable; NULL means it was created on a no-project session and is not surfaced in any list — it's stored for analytics but the user never picks from a no-project category list.

`project_id` on sessions is nullable. The project selection screen always offers a "no project" option. Sessions without a project display as "— / Category" in the TUI. The Go layer maps NULL ↔ empty string at the scan boundary, consistent with how `end_notes` is handled.

`category_id` on sessions is always required (NOT NULL). The category screen is never skipped. When a project is selected, the category screen shows that project's categories; when no project is selected, the category screen shows a free-text input with dedup against existing no-project categories by name.

See `internal/store/schema.sql` for the full DDL with indexes.

## Session model

A session moves through a state machine. The domain logic lives in `internal/session/` as a pure package with no I/O — the Phase 2 stdin driver and the Phase 3 Bubble Tea TUI both sit on top of it. Methods take `now time.Time` explicitly so the package is testable with a fake clock.

States:
Idle → SetupProject → SetupCategory → SetupDuration → Active
Active ↔ Paused
Active → EndingNotes        (on `end` or target reached)
Paused → EndingNotes        (on `end`)
EndingNotes → WhatNext      (session row INSERTed here)
WhatNext → Active           (Continue: new session, same settings)
WhatNext → SetupProject     (New Session)
WhatNext → Break → WhatNext (5-min break)
WhatNext → Idle             (End)

In-memory session value holds: `id`, `category_id`, `project_id`, `planned_duration_min`, `started_at`, `pauses[]`, current target end time, `captures[]`. Written to the DB only on session end, in one INSERT. Everything mutable during the active phase lives in memory.

**Commands during Active / Paused:**

| Command | Behavior |
|---|---|
| `upn <text>` | Record a capture, deferred to session end. Allowed in both Active and Paused; does not change state. |
| `ext <n>` | Extend the target end time by `n` minutes. Does not write to `actual_duration_sec`. Allowed in both Active and Paused. |
| `pause` | Pause the timer. If already paused, echoes a reminder to use `resume`. |
| `resume` | Resume from Paused. |
| `end` | End the session early; transitions to EndingNotes. |

**Duration accounting:**
actual_duration_sec = (ended_at - started_at) - sum(pause intervals)

The target end time also shifts forward by pause duration on resume, so the session aims for the same amount of *working* time across pauses.

**Session status values:**

- `completed` — reached or exceeded planned duration
- `ended_early` — ended via `end` before planned duration

**Resume on restart:** if the most recent session is `ended_early`, the start screen offers `Resume <category>/<project> with N minutes remaining` as an option. Selecting it creates a new session with the same category/project and `planned_duration_min = remaining` (previous planned minus previous actual). The previous `ended_early` row stays as-is — resume creates a fresh row, never edits.

**What-Next screen:** shows the captures from the just-ended session at the top, then action choices: `Continue Session` (new session, same settings), `New Session` (back to SetupCategory), `Break` (5-minute break, returns here), `End`. Capture disposition is deferred to Phase 4.

**Capture disposition (Phase 4):** each capture on the What-Next screen gets three actions:
- `Clear` — mark done; stays in DB for history.
- `Send to Reminders` — write to `jacktasks-inbox` via EventKit; stays in DB, `sent_to_reminders` flagged.
- `Do` — start a new session for this capture. Marks it cleared and routes into normal session setup (project → category → duration). The capture text is shown as context and pre-fills the category name input; user picks or creates a project first (or skips to "no project"), then picks or creates a category.

## Directory structure

```
jacktasks/
├── go.mod
├── go.sum
├── cmd/
│   ├── jacktasks/
│   │   ├── main.go            # entrypoint: open store, run tea.Program
│   │   ├── model.go           # Bubble Tea model (Init/Update/View + handlers)
│   │   └── styles.go          # Lipgloss palette, key maps
│   └── jacktasks-sync/
│       └── main.go            # sync server entrypoint (env-configured)
├── internal/
│   ├── paths/                 # filesystem paths (DataDir, DBPath)
│   ├── recovery/              # active.json sentinel (crash recovery)
│   ├── reminders/             # Apple Reminders client + fake
│   ├── session/               # pure session state machine (no I/O)
│   │   ├── session.go
│   │   └── session_test.go
│   ├── store/                 # SQLite layer
│   │   ├── schema.sql
│   │   ├── store.go           # Open, Close, pragmas, migrations
│   │   ├── sync.go            # PullSince, UpsertFromSync (used by server + client)
│   │   ├── syncstate.go       # GetSyncState, SetLastPushAt, SetLastPullAt
│   │   ├── categories.go
│   │   ├── projects.go
│   │   ├── sessions.go
│   │   ├── captures.go
│   │   ├── config.go          # Get/Set + DeviceID lazy init
│   │   └── *_test.go
│   ├── syncclient/            # client-side push-pull logic (jacktasks sync)
│   │   ├── client.go
│   │   └── client_test.go
│   ├── syncproto/             # shared wire types (PushRequest, PullResponse, etc.)
│   └── syncserver/            # HTTP handler logic for jacktasks-sync
│       ├── server.go
│       └── server_test.go
├── deploy/
│   ├── DEPLOY.md              # step-by-step ThinkCentre deploy instructions
│   ├── jacktasks-sync.service # systemd unit file
│   └── env.template           # env file template (copy → /etc/jacktasks-sync/env)
├── Makefile                   # check, install, build-sync-linux targets
├── PROJECT.md                 # this file
├── CLAUDE.md                  # AI handoff instructions
└── LOG.md                     # running record of decisions
```

## Build, test, run

```bash
make check                # build + vet + test (pre-commit gate)
make install              # install jacktasks TUI to /usr/local/bin (macOS)
make build-sync-linux     # cross-compile sync server for linux/amd64

go run ./cmd/jacktasks    # run TUI from source
jacktasks sync            # one-shot sync (requires JACKTASKS_SYNC_URL + TOKEN in env)

sqlite3 ~/Library/Application\ Support/jacktasks/jacktasks.db ".tables"
```

## Deployment

The sync server (`jacktasks-sync`) runs on the ThinkCentre. Full step-by-step instructions are in `deploy/DEPLOY.md`. Summary:

**Build and ship the server binary:**
```bash
make build-sync-linux
scp jacktasks-sync-linux <thinkcentre>:/tmp/jacktasks-sync
```

**First-time server setup (on ThinkCentre):**
```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin jacktasks
sudo mv /tmp/jacktasks-sync /usr/local/bin/jacktasks-sync && sudo chmod 755 /usr/local/bin/jacktasks-sync
sudo mkdir -p /var/lib/jacktasks-sync && sudo chown jacktasks:jacktasks /var/lib/jacktasks-sync
sudo mkdir -p /etc/jacktasks-sync
# copy deploy/env.template → /etc/jacktasks-sync/env, fill in token + Tailscale IP
sudo cp deploy/jacktasks-sync.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now jacktasks-sync
curl http://<tailscale-ip>:8484/healthz   # → {"ok":true}
```

**On each Mac — add to `~/.zshrc`:**
```bash
export JACKTASKS_SYNC_URL=http://<thinkcentre-tailscale-ip>:8484
export JACKTASKS_SYNC_TOKEN=<shared token>
```

Then `jacktasks sync` to push/pull. See `deploy/DEPLOY.md` for the full cross-Mac convergence verification procedure.

## Current state

**Phase 0 — Spike (closed):** `go-eventkit` verified end-to-end on the MacBook (lists, create, complete reminders). Tailscale routing to the home NAS verified via hello-world HTTP server. Mac Mini spike deferred — will accept the permission prompt when first run.

**Phase 1 — Data layer (closed):** SQLite schema + idempotent migrations, DAL for all six tables with tests, paths package, device_id lazy-init, `cmd/jacktasks/main.go` wiring. 24 tests passing.

**Phase 2 — Core session loop (closed):** `internal/session/` is a pure state-machine package with no I/O. All states, commands (`upn`, `ext`, `pause`, `resume`, `end`), duration accounting, and resume-on-restart are implemented and tested. Sessions and captures written to store on session end only.

**Phase 3 — Bubble Tea TUI (closed):** The stdin driver in `cmd/jacktasks/main.go` has been replaced with a Bubble Tea TUI (`cmd/jacktasks/model.go`). `internal/session/` was unchanged. Full screen-by-screen port: resume offer, category/project selection with inline create, duration, active command loop, end notes, what-next, break countdown. Auto-ends session when timer expires; auto-ends break after 5 minutes. Session data snapshotted before async store write to avoid machine-state races. Dependencies added: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`.

**Phase 4 — Reminders integration (closed):** `internal/reminders/` package: `Client` interface, `eventkitClient` wrapping `go-eventkit`, `Fake` for tests. Capture disposition on WhatNext: `c<n>` clear, `r<n>` send to Reminders, `d<n>` do/start session. Startup screen replaces the old resume y/N prompt: shows inbox items (async fetch), resume option, new session, quit. EventKit failure is non-fatal.

**Pre-Phase-5 design fix (closed):** Inverted the project ↔ category relationship. Projects are now top-level; categories are per-project sub-labels. Schema rewritten (no migration — local DB dropped and recreated). Session setup flow reordered to Project → Category → Duration throughout the state machine and TUI. No-project path uses free-text category entry with name-based dedup (`CreateOrGetCategoryByName`). 48 tests passing.

**Phase 5 — Crash recovery (closed):** `active.json` sentinel in the data dir, written on every Active/Paused transition and after `upn`/`ext`/`pause`/`resume`/`ContinueSession`. On startup, if the sentinel exists and its session UUID isn't in the DB, a recovery prompt is shown before the normal start screen. New `internal/recovery/` package; `Snapshot()` / `Hydrate()` methods on `session.Machine`. Sentinel cleared on successful DB write. 6 new recovery tests + 6 new session tests.

**Phase 5.5 — TUI polish (closed):** `cmd/jacktasks/styles.go` — Lipgloss palette with `AdaptiveColor`, named styles, key maps. Persistent header (app name / screen name / session context) and footer (context-sensitive key hints) on every screen. Arrow-key cursor navigation on all list screens with Enter-to-select; numeric shortcuts still work. `bubbles/progress` bar on Active, Paused, and Break screens. `bubbles/spinner` for inbox load and session save. No state-machine or flow changes. One new indirect dep: `charmbracelet/harmonica` (required by `bubbles/progress`).

**Phase 6a — Sync protocol + server skeleton (closed):** `captures.updated_at` migration + backfill + index. `internal/syncproto/` shared wire types and table constants. `cmd/jacktasks-sync/` server binary (env-configured: `JACKTASKS_SYNC_TOKEN`, `JACKTASKS_SYNC_DB`, `JACKTASKS_SYNC_ADDR`). `internal/store/sync.go` — `PullSince` (generic, column-list-driven) and `UpsertFromSync` (per-table conflict strategy). `internal/syncserver/` — auth middleware, `/healthz`, `/push`, `/pull` handlers. 8 syncserver tests covering round-trip, LWW, append-only dedup, auth, empty-array response, missing-ID rejection. Wire protocol documented in PROJECT.md.

**Phase 6b — Client `jacktasks sync` subcommand (closed):** `internal/store/syncstate.go` — `GetSyncState`, `SetLastPushAt`, `SetLastPullAt` (independent upserts, neither clobbers the other). `UpdateProject` added to projects DAL. `internal/syncclient/` — `Sync` runs push-before-pull per table; bookmarks advanced per-table on success so partial sync is safe; formatted summary output. Subcommand dispatch in `cmd/jacktasks/main.go`: `jacktasks sync` (reads `JACKTASKS_SYNC_URL` + `JACKTASKS_SYNC_TOKEN`) vs TUI. 5 syncclient tests: round-trip, idempotent re-sync, LWW convergence, bad token, missing config. 68 tests total.

**Phase 6c — Deploy + verify:** `Makefile` with `check` / `install` / `build-sync-linux` targets. `deploy/` directory: `DEPLOY.md` step-by-step guide, `jacktasks-sync.service` systemd unit, `env.template`. Cross-compilation to `linux/amd64` verified (statically linked ELF, no libc dependency). Remaining steps are operational: run on ThinkCentre, first sync from each Mac, cross-device convergence check — see `deploy/DEPLOY.md`.

## Sync protocol

REST over HTTP, JSON bodies. Server binds to Tailscale interface only. Auth: `Authorization: Bearer <token>` from `JACKTASKS_SYNC_TOKEN` env var.

### Endpoints

```
GET  /healthz
POST /push?table=<name>           body: {"rows": [...]}
                                  returns: {"accepted": N, "rejected": [...]}
GET  /pull?table=<name>&since=<unix_sec>
                                  returns: {"rows": [...], "as_of": <unix_sec>}
```

Tables synced: `projects`, `categories`, `sessions`, `captures`.
Not synced: `config` (per-device device_id), `sync_state` (per-device bookkeeping).

### Wire format

Each row is a flat JSON object matching the table columns. Rules:
- Timestamps: Unix epoch seconds (integers), same as DB storage.
- NULL fields: JSON `null` (not empty string — wire is stricter than the Go↔SQL boundary).
- Boolean fields (`cleared`, `sent_to_reminders`, `archived`): JSON integers 0/1 (matches DB storage).

### Conflict rules

| Table | Strategy |
|---|---|
| `sessions` | Pure append. `INSERT OR IGNORE` by UUID on both sides. |
| `captures` | Pure append for new rows. Flag updates (`cleared`, `sent_to_reminders`) use last-write-wins on `updated_at`. |
| `projects` | Last-write-wins on `updated_at`. `deleted_at` tombstone wins over any update with older `updated_at`. |
| `categories` | Same as projects. |

### Sync flow (client, Phase 6b)

For each table: push rows newer than `sync_state.last_push_at`, then pull rows newer than `sync_state.last_pull_at`. Push before pull. Update `sync_state` per table on success. Partial sync is fine — state is updated as each table completes.

## What's deliberately out of V1

To prevent scope creep:

- View Categories management UI (rename, archive, merge)
- View Past Sessions / analytics UI
- View Reminders standalone view
- Idle / away-from-keyboard detection
- macOS notifications on session end
- Auto-sync (periodic or on launch)
- Mobile companion app
- Tags or multi-category projects
- Export to CSV / JSON
- Backfill / manual session entry

Decisions about these get easier once V1 ships and there's real data to look at.
