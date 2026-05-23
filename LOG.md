# LOG

Running record of significant decisions and progress on jacktasks. Entries are added at phase boundaries and for genuine architectural decisions, not for every code change.

---

## 2026-05-23 — Pre-implementation design

The Swift version was a false start. CLI/TUI chosen as the V2 direction. Open question: what language?

**Decided: Go.**

Initial recommendation was Python (faster iteration, user already knows it, subprocess to `BRO3886/rem` would handle Reminders). Revised after the user surfaced two things I'd underweighted:

1. **Learning is part of the goal.** With LLM-assisted coding, the iteration speed argument for Python shrinks. Go's strictness becomes an asset — the compiler is a co-teacher.
2. **Bubble Tea + Lipgloss + Bubbles is genuinely the better TUI ecosystem** for the stateful, animated, status-bar-driven UI a Pomodoro tool wants.

Also: `BRO3886/go-eventkit` lets Go talk to EventKit directly (in-process, no subprocess), cleaner than shelling to `rem`.

**Decided: homelab sync over Turso.**

Considered Turso embedded replicas (would save ~3 sessions of building sync infrastructure, free tier covers usage by orders of magnitude). User leaned homelab for ethos fit: privacy, self-hosted, bare-metal. Verified the cost: ~3 extra sessions in Phase 6, not a blocker.

**Decided: ThinkCentre, not the GeForce 3080 desktop**, for the sync server. Workload is microscopic — a Raspberry Pi Zero could handle it. No reason to burn 100W idle on a machine that should be reserved for ML/GPU work.

**Sync conflict model:**
- Sessions and captures are pure-append (session is only written on completion). No conflicts possible.
- Categories and projects use last-write-wins with `updated_at` and `deleted_at` tombstones.
- Schema uses UUID primary keys generated on each device so writes don't need server coordination.

**Storage decision:**
- iCloud Drive for SQLite is risky (file eviction, WAL files sync out of order). Rejected.
- Nextcloud also problematic for concurrent file-level sync. Rejected for that approach.
- Final approach: local SQLite on each Mac + manual sync to a homelab Go HTTP service. Local-first, async sync.

**Out-of-V1 list** locked in PROJECT.md to prevent scope creep: management UIs, analytics, idle detection, notifications, auto-sync, mobile app, tags, export, manual entry.

---

## 2026-05-23 — Phase 0: Spike

Goal: prove `go-eventkit` works on the user's hardware and that Tailscale reaches the home server.

**Done:**
- Installed Go 1.24+ (user landed on 1.26.3 via Homebrew) and Xcode CLT on MacBook.
- Created the `jacktasks-inbox` list in Apple Reminders (iCloud account).
- Ran `reminders-spike`: lists all Reminders lists, finds `jacktasks-inbox`, creates a test reminder, completes it, verifies. Spike passed.
- macOS TCC permission prompt accepted on first run.
- Stood up a hello-world Go HTTP server on the NAS. `curl http://nas:8484/ping` from the MacBook returned valid JSON. Tailscale routing confirmed.

**Deferred:**
- Mac Mini spike. The user wasn't physically at the Mac Mini and the residual risk is minimal — same OS, same library. Will surface only the macOS permission prompt when first run there.

**Notes:**
- First `go run` on `go-eventkit` spent ~10s compiling the Objective-C bridge. Subsequent runs cached.
- Permission prompt names the terminal app (Warp/iTerm2/Terminal.app), not the Go binary. Switching terminals later means re-accepting.

---

## 2026-05-23 — Phase 1: Skeleton + data layer

Goal: open SQLite, apply schema, full CRUD for all tables with tests. No UI.

**Done:**
- Module path: `github.com/j-f-allison/jacktasks` (GitHub private; no repo created yet — Go doesn't require it).
- `internal/store/` package with `Open`, schema embedded via `//go:embed`, pragmas (`journal_mode=DELETE`, `foreign_keys=ON`).
- DAL for categories, projects, sessions, captures with tests.
- `config` table helpers + `DeviceID` lazy-init that generates and persists on first call.
- `internal/paths/` package using `os.UserConfigDir()` (resolves to `~/Library/Application Support/jacktasks` on macOS).
- `cmd/jacktasks/main.go` now opens the DB, generates the device_id, prints both paths and ID.

**Result:** 24 passing tests. `go run ./cmd/jacktasks` produces a stable device_id across runs. Database visible in `~/Library/Application Support/jacktasks/jacktasks.db`.

**Patterns established** (codified in `CLAUDE.md`):
- Unix epoch seconds for time storage, converted at the Go boundary.
- UUID strings as primary keys.
- `scanX` helpers behind a small `rowScanner` interface.
- Input structs for ≥4-field constructors (`CreateSessionInput`).
- Typed string constants with `Valid()` methods for enums (`SessionStatus`).
- Error wrapping with `%w`, sentinel `ErrNotFound`, `errors.Is` at call sites.

**Trade-offs explicitly accepted:**
- No CHECK constraints on `status` — relies on Go-layer validation. Re-evaluate if raw SQL writes ever become a thing.
- `captures.captured_at` is second-precision. Two upns in the same second fall back to UUID tiebreaker (deterministic but not meaningful). Bump to nanoseconds if it ever bites.
- `paths.DataDir()` tests create the real `~/Library/Application Support/jacktasks/` directory. Idempotent and harmless; parameterize later if needed.

**Deferred to later phases:**
- `sync_state` table helpers → Phase 6.
- TOML config file for user-editable settings → defer until something needs to be user-editable.
- Update / soft-delete operations for categories and projects → only needed at sync time.

---

## Phase plan (remaining)

| Phase | Goal | Status |
|---|---|---|
| 0 | Spike: prove go-eventkit + Tailscale | ✅ closed |
| 1 | Data layer with tests | ✅ closed |
| 2 | Core session loop with stdin prompts | ⬜ next |
| 3 | Bubble Tea TUI replacing prompts | ⬜ |
| 4 | Reminders integration | ⬜ |
| 5 | Crash recovery / state persistence | ⬜ |
| 6 | Sync service + client | ⬜ |

Time estimate: ~10–14 more sessions across Phases 2–6, with Phase 3 dominating.
