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
categories  (id, name, created_at, updated_at, deleted_at?, archived)
projects    (id, name, category_id→categories, created_at, updated_at, deleted_at?, archived)
sessions    (id, category_id→categories, project_id?→projects,
             planned_duration_min, actual_duration_sec, started_at, ended_at,
             end_notes?, status, created_at, device_id)
captures    (id, session_id→sessions, text, captured_at,
             cleared, sent_to_reminders, created_at)
sync_state  (table_name, last_pull_at?, last_push_at?)
config      (key, value)
```

Sessions are written once on session end — never edited. Captures get two single-flag updates (cleared, sent_to_reminders) but no other mutations.

`project_id` is nullable. A session belongs to a category and optionally a project. The project selection screen always offers a "no project" option. Sessions without a project display as "Category / —" in the TUI. The Phase 4 migration recreates the sessions table with `project_id TEXT REFERENCES projects(id)` (dropping NOT NULL); existing rows are copied as-is. The Go layer maps NULL ↔ empty string at the scan boundary, consistent with how `end_notes` is handled.

See `internal/store/schema.sql` for the full DDL with indexes.

## Session model

A session moves through a state machine. The domain logic lives in `internal/session/` as a pure package with no I/O — the Phase 2 stdin driver and the Phase 3 Bubble Tea TUI both sit on top of it. Methods take `now time.Time` explicitly so the package is testable with a fake clock.

States:
Idle → SetupCategory → SetupProject → SetupDuration → Active
Active ↔ Paused
Active → EndingNotes        (on `end` or target reached)
Paused → EndingNotes        (on `end`)
EndingNotes → WhatNext      (session row INSERTed here)
WhatNext → Active           (Continue: new session, same settings)
WhatNext → SetupCategory    (New Session)
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
- `Do` — start a new session for this capture. Marks it cleared and routes into normal session setup (category → optional project → duration). The capture text is shown as context but does not pre-fill any field; user picks category, then picks an existing project, creates a new one, or skips project entirely ("no project").

## Directory structure

```
jacktasks/
├── go.mod
├── go.sum
├── cmd/
│   └── jacktasks/
│       ├── main.go            # entrypoint: open store, run tea.Program
│       └── model.go           # Bubble Tea model (Init/Update/View + handlers)
├── internal/
│   ├── paths/                 # filesystem paths (DataDir, DBPath)
│   ├── session/               # pure session state machine (no I/O)
│   │   ├── session.go
│   │   └── session_test.go
│   └── store/                 # SQLite layer
│       ├── schema.sql
│       ├── store.go           # Open, Close, pragmas, migrations
│       ├── categories.go
│       ├── projects.go
│       ├── sessions.go
│       ├── captures.go
│       ├── config.go          # Get/Set + DeviceID lazy init
│       └── *_test.go
├── PROJECT.md                 # this file
├── CLAUDE.md                  # AI handoff instructions
└── LOG.md                     # running record of decisions
```

## Build, test, run

```bash
# all tests
go test ./...

# verbose (per-test detail)
go test -v ./...

# run the CLI (currently just opens DB + prints device_id)
go run ./cmd/jacktasks

# inspect the live database
sqlite3 ~/Library/Application\ Support/jacktasks/jacktasks.db ".tables"
```

## Current state

**Phase 0 — Spike (closed):** `go-eventkit` verified end-to-end on the MacBook (lists, create, complete reminders). Tailscale routing to the home NAS verified via hello-world HTTP server. Mac Mini spike deferred — will accept the permission prompt when first run.

**Phase 1 — Data layer (closed):** SQLite schema + idempotent migrations, DAL for all six tables with tests, paths package, device_id lazy-init, `cmd/jacktasks/main.go` wiring. 24 tests passing.

**Phase 2 — Core session loop (closed):** `internal/session/` is a pure state-machine package with no I/O. All states, commands (`upn`, `ext`, `pause`, `resume`, `end`), duration accounting, and resume-on-restart are implemented and tested. Sessions and captures written to store on session end only.

**Phase 3 — Bubble Tea TUI (closed):** The stdin driver in `cmd/jacktasks/main.go` has been replaced with a Bubble Tea TUI (`cmd/jacktasks/model.go`). `internal/session/` was unchanged. Full screen-by-screen port: resume offer, category/project selection with inline create, duration, active command loop, end notes, what-next, break countdown. Auto-ends session when timer expires; auto-ends break after 5 minutes. Session data snapshotted before async store write to avoid machine-state races. Dependencies added: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, `charmbracelet/bubbles`.

**Phases 4–6 (planned):** see `LOG.md` for the running phase plan. Briefly: Reminders integration, crash recovery, sync service + client.

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
