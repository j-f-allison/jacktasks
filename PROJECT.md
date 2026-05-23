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
sessions    (id, category_id→categories, project_id→projects,
             planned_duration_min, actual_duration_sec, started_at, ended_at,
             end_notes?, status, created_at, device_id)
captures    (id, session_id→sessions, text, captured_at,
             cleared, sent_to_reminders, created_at)
sync_state  (table_name, last_pull_at?, last_push_at?)
config      (key, value)
```

Sessions are written once on session end — never edited. Captures get two single-flag updates (cleared, sent_to_reminders) but no other mutations.

See `internal/store/schema.sql` for the full DDL with indexes.

## Directory structure

```
jacktasks/
├── go.mod
├── go.sum
├── cmd/
│   └── jacktasks/main.go      # CLI entrypoint
├── internal/
│   ├── paths/                 # filesystem paths (DataDir, DBPath)
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

**Phase 2 — Core session loop (next):** validate the session flow with simple stdin prompts before investing in Bubble Tea. Category/project pickers with "add new", duration select with presets, active session with `upn` and `ext` commands, end-of-session notes + what-next list. Sessions written to store on completion only.

**Phases 3–6 (planned):** see `LOG.md` for the running phase plan. Briefly: Bubble Tea TUI, Reminders integration, crash recovery, sync service + client.

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
