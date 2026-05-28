# LOG

Running record of significant decisions and progress on jacktasks. Entries are added at phase boundaries and for genuine architectural decisions, not for every code change.

---

## 2026-05-27 — Fix: in-app sync failing with "database is locked" (v1.8.1)

Background sync consistently failed when ending a session in the TUI, while
`jacktasks sync` standalone always worked. Root cause: concurrency, not the
sync code (both paths call the same `syncclient.Sync`).

On `sessionSavedMsg` the TUI fires `runSyncCmd` (DB writes) and
`loadCategoryProgressCmd` (DB reads) as separate Bubble Tea commands, each in
its own goroutine. With the default `database/sql` connection pool and
`journal_mode=DELETE`, those two goroutines race on different pooled
connections, and with no `busy_timeout` the loser gets `SQLITE_BUSY` immediately
— surfacing as "✗ sync failed". The standalone CLI never collides because it's
the only thing touching the DB. The startup auto-sync could fail the same way.

**Fix:** `db.SetMaxOpenConns(1)` in `store.Open`. One connection serializes all
access at the Go layer, so SQLite never sees concurrency. The sync's long HTTP
calls happen between DB calls and don't hold the connection, so no stalls. Bonus:
it also makes the per-connection `PRAGMA foreign_keys=ON` apply to every query
instead of just whichever pooled connection ran it (a latent FK-enforcement gap).

Also surfaced the underlying error for background sync failures in
`model.go` (`syncDoneMsg`): previously `m.errMsg` was only set for manual syncs,
so a failed background sync showed "sync failed" with no detail.

## 2026-05-27 — Dailies/Weeklies panel on the start screen (v1.8.0)

The recurring-target progress HUD previously only appeared during an active
session, for the one category being worked. Now the start screen surfaces
progress for *every* targeted category at a glance.

**Layout:** two columns. The existing inbox + menu sit on the left; a new
"Dailies / Weeklies" panel sits on the right, joined via
`lipgloss.JoinHorizontal`. Below a width of 72 columns (or when there are no
targeted categories) the panel stacks beneath the menu instead. Each line reuses
the in-session progress text with a compact streak suffix — `Keybr: 12/30 min
today · 🔥 4d`, `Exercise: 0/45 min this week`, `Reading: done today · 🔥 9w`.

**Implementation:**
- `store.ListCategoriesWithTarget` — new query returning all live categories with
  `target_period` set, sorted by name (`categories.go` + test).
- Extracted `periodSeconds(...)` helper (day/week window → `SumCategorySecondsBetween`)
  shared by the existing `loadCategoryProgressCmd` and the new `loadDailiesCmd`,
  which computes period seconds + `CategoryStreak` for each targeted category.
- `dailiesLoadedMsg` + `dailies`/`dailiesLoaded` Model fields; the command fires
  from `Init` only when the start screen (`uiExtraStart`) is shown.
- Extracted `progressText(cat, periodSec)` from `categoryProgressLine` so the HUD
  and the panel share the "N/M min today" formatting; the panel adds its own
  compact `🔥 Nd`/`🔥 Nw` suffix via `dailyPanelLine`.
- `renderStartScreen` rebuilt to compose a left menu block and join it with
  `renderDailiesPanel()`; new `writeIndented` helper applies the screen margin to
  multi-line joined blocks.

Cursor/numeric navigation and all existing status lines (Sessions today, sync
status/summary) are unchanged. Streak computation adds one `CategoryStreak` query
per targeted category on startup — fine at personal scale.

**Also in v1.8.0 — session-count targets.** Targets could previously be a minute
goal (`30/day`) or presence-only (`/day`). Added a third measure: a count of
logged sessions of any length, syntax `3x/day` / `3x/day MTWTF` / `2x/week` (an
`x` suffix on the number). A session counts if its row exists in the period;
cancelled sessions write no row, so they don't count.

- Schema: new nullable `target_sessions INTEGER` on categories, added to the
  existing `migrateCategoryTargets` column loop (idempotent ADD COLUMN). It joins
  the categories sync wire format (column list in `sync.go` + `upsertCategory`
  INSERT/conflict). **All three binaries (both Macs + the sync server) need
  reinstalling**, like any sync-touching change.
- `target.Parse`/`Format` gained a `sessions *int` return/param; the number token
  with an `x` suffix parses as a session count. minutes and sessions are mutually
  exclusive (the grammar can't express both). `SetCategoryTarget` gained a
  `sessions` param.
- `store.CountCategorySessionsBetween` (new) backs both the streak check
  (`periodMet` now switches sessions → minutes → presence) and the progress
  display. `periodProgress` (model) returns either a second-count or a
  session-count depending on the target type.
- Display: session-count progress reads `Name: 2/3 today` (no unit word, per the
  agreed format); minute/presence forms unchanged. List annotation reads
  `(3 sessions/day)`.

New tests: parse/format for `Nx/day|week` incl. error cases, store round-trip for
`target_sessions`, `CountCategorySessionsBetween`, and a session-count daily
streak (met/under-target).

**Also in v1.8.0 — configurable display/bucketing timezone.** Sessions are
stored in UTC epoch seconds (unchanged, correct). What was implicit was the
*display* timezone: the local app bucketed days/weeks by `time.Local`, and the
server's web view rendered `StartedAt.Local()` — which on the UTC Ubuntu box
showed everything in UTC. Both are now configurable.

- **Local app:** new `timezone` key in `config.toml` (IANA name, e.g.
  `America/Denver`). `config.Load` resolves it to a `*time.Location` (now exposed
  as `Config.Location`, defaulting to `time.Local`); an invalid name is a hard
  error, matching the existing parse-error-and-exit policy. The Model carries
  `loc`, and `m.now` is now `time.Now().In(loc)` (set at construction, on every
  tick, and for the today-session count). Because all period/streak code already
  keys off `now.Location()`, that single change flows the configured tz through
  `periodBounds`, `CountTodaySessions`, and `CategoryStreak` with no other edits.
- **Server:** new optional `JACKTASKS_SYNC_TZ` env var. `cmd/jacktasks-sync`
  resolves it (fatal on bad name) and passes a `*time.Location` into
  `syncserver.NewMux`, threaded to `groupByDay`/`handleSessions`, which now bucket
  and format in that tz instead of `.Local()`. Defaults to the server's local tz.
- Docs/deploy: `deploy/env.template` and `DEPLOY.md` document `JACKTASKS_SYNC_TZ`;
  README gains a Configuration section covering both `config.toml` and the env var.
- Tests: config timezone parse/validate/default; a web-view test asserting a
  02:00-UTC session buckets on the previous calendar day under `America/Denver`
  vs the same day under UTC (skips if host `tzdata` is missing).

Version bumped 1.7.0 → 1.8.0 (all three additions land in the same unreleased
bump). Requires `make install` on each Mac **and** a server redeploy. Full suite
green.

## 2026-05-27 — Document Dailies & Weeklies in README

Added a "Dailies & Weeklies" subsection to `README.md` under "Using it": the
target syntax table (`30/day`, `/day`, `30/day MTWTF`, `30/week`, `none`),
positional `MTWTFSS` weekday letters, the progress HUD example, and streak
behavior (off-day skipping, in-progress period never breaks). Sourced from the
v1.6.0 LOG entry. Docs-only — no code change, no version bump.

## 2026-05-27 — Add README

Wrote a top-level `README.md`: high-level overview of what jacktasks is (the two binaries, the three ADHD-driven problems it solves), usage instructions (install, session flow, the in-session command table, sync), and a condensed deploy section pointing at `deploy/DEPLOY.md`. Sourced from `PROJECT.md` and the `Makefile`. Docs-only — no code change, no version bump.

## 2026-05-26 — Read-only web session view on the sync server (v1.7.0)

Adds a browsable list of logged sessions, served by the existing `jacktasks-sync` server on the ThinkCentre. The master DB already holds every session from both Macs, so the view needs no new data plumbing — just a render. No new binary on the Mac (the TUI is unchanged); requires a sync-server redeploy (`make build-sync-linux` → scp → restart). Version still bumped to 1.7.0 to track the feature, even though it lands in the sync binary rather than the TUI.

This is a deliberate, minimal take on "View Past Sessions UI," which PROJECT.md lists under "deliberately out of V1." Read-only, no analytics, no editing — just viewing, which is what the user asked for before going daily-driver.

**Store:** `ListSessionViews(ctx, limit)` in `sessions.go` — sessions newest-first LEFT JOINed to projects and categories for display names (`SessionView` embeds `Session` + `ProjectName`/`CategoryName`). Dedicated inline scan rather than touching the shared `scanSession`. No-project sessions yield empty `ProjectName`. Test covers the join, no-project case, and newest-first ordering.

**Server (`internal/syncserver/`):** new `web.go` renders an `html/template` page (no new deps) grouped by calendar day in the server's local timezone, with inline end notes and a done/early status badge; Tokyo Night palette to match the TUI. Capped at 500 sessions. Registered as `GET /{$}` in `NewMux`. Auth: the page is intentionally unauthenticated — `authMiddleware` now consults a `publicPaths` set (`/healthz` + `/`); the server binds only to the Tailscale interface, so reachability is the access control (the user's explicit choice). The sync API (`/push`, `/pull`) still requires the bearer token — covered by a regression test. Four new web tests (renders without auth, empty state, early badge, `/pull` still 401s).

**Deploy:** after redeploying, the view is at `http://<thinkcentre-tailscale-ip>:8484/` in any browser on the tailnet. DEPLOY.md updated with this note.

## 2026-05-26 — Dailies & Weeklies: recurring category targets + streaks (v1.6.0)

Adds optional recurring targets to categories (daily or weekly, with minute goal or presence-only, with optional weekday schedule for dailies), plus query-time streak computation and an in-session HUD.

**Schema:** Three nullable columns added to `categories` via `migrateCategoryTargets` in `store.go`: `target_minutes INTEGER`, `target_period TEXT` (`'day'`/`'week'`), `schedule_mask INTEGER` (7-bit weekday bitmask, bit 0 = Monday). Columns also added to `schema.sql` for fresh DBs. Migration is idempotent (PRAGMA table_info check per column).

**Store (`internal/store/`):**
- `Category` struct extended with `TargetMinutes *int`, `TargetPeriod string`, `ScheduleMask *int`, and `HasTarget() bool`. All four `scanCategory` call sites updated.
- `SetCategoryTarget(ctx, id, minutes, period, mask)` — UPDATE + `updated_at` bump; syncs via existing categories LWW sync.
- `SumCategorySecondsBetween` and `CategoryActiveBetween` — progress queries for the current period.
- `streak.go` — `CategoryStreak(ctx, store, cat, now)` walks periods backward (daily: day-by-day respecting `schedule_mask`, skipping off-days without breaking; weekly: ISO Mon–Sun weeks). Current in-progress period never breaks streak. Capped at 366 days / 53 weeks. Uses exported `StartOfWeekMonday`.
- Sync: `tableColumns["categories"]` and `upsertCategory` updated with the three new columns. LWW unchanged.

**Parser (`internal/target/`):** new pure package.
- `Parse(s)` — handles `none`, `30/day`, `/day`, `30/day MTWTF`, `/week`, `30/week`. Returns typed error on bad input.
- `Format(minutes, period, mask)` — inverse for UI annotations.
- `DayScheduled(mask, weekday)` — used by streak walker.
- Weekday tokens use positional MTWTFSS matching (handles ambiguous T and S letters).

**TUI (`cmd/jacktasks/model.go`):**
- New `uiExtraTargetEdit` state. Press `t` on a highlighted category in the category-selection screen to open a compact target editor (Escape to cancel). On save, updates DB and in-memory category list; cursor restored to the edited row.
- Category list annotations: each category with a target shows a dim `(30 min/day, weekdays)` suffix.
- HUD progress line on Active, Paused, and WhatNext screens: `Keybr: 12/30 min today · 🔥 4-day streak` (or weekly variant). Loaded async via `loadCategoryProgressCmd` when entering Active and after each session save. Zero `catPeriodSec`/`catStreak` on new session start.
- Footer hint updated: `t set target` shown on category-selection screen; overlay hints shown during target edit.

15 new tests (target: 4 suites; store: SetCategoryTarget round-trip, SumCategorySecondsBetween, CategoryActiveBetween, streak: 7 cases). 123 tests pass total.

---

## 2026-05-26 — TOML config foundation + daily_session_target (v1.5.0)

Introduces `~/.config/jacktasks/config.toml` as the user-editable config surface, backed by `github.com/BurntSushi/toml`. New `internal/config/` package: `Config` struct + `Load(path)`. Missing file = fine (defaults everywhere). Parse error = print to stderr and exit non-zero — no silent fallback.

`ConfigPath()` added to `internal/paths/` to locate the file (`$HOME/.config/jacktasks/config.toml`). Config is loaded in `main.go` before the TUI starts and passed into `newModel` as an `appCfg config.Config` argument.

Consumer — `daily_session_target = N`:
- New `Store.CountTodaySessions(ctx, now)` in the sessions DAL: counts rows whose `started_at` falls within the calendar day of `now` (local timezone), using midnight boundaries computed from `time.Date`.
- `Model` gains `dailyTarget int` (from config) and `todaySessions int` (DB count, refreshed on startup via `initStartup` and after each session save via `sessionSavedMsg`).
- A `Sessions today: N/M` dim line appears on both the start screen and the WhatNext screen when `daily_session_target > 0`. When unset/zero, nothing is shown.

5 new tests (4 config: missing, valid, malformed, explicit-zero; 1 store: CountTodaySessions today vs yesterday). 108 tests pass.

---

## 2026-05-26 — Per-project Reminders list (v1.4.0)

Associates a named Apple Reminders list with each project. During session setup, the category-selection screen shows a second section — "From \<list\>:" — containing incomplete items from the project's list alongside the existing project categories. Selecting a reminder sets `doContextText` and `pendingReminderID`, routing into the normal Do machinery: the reminder title pre-fills the new-category-name input (editable), and the end-of-session dispo prompt offers to mark it complete.

Schema: `projects.reminders_list_name TEXT` (NULL = no list). Added via `migrateRemindersListName` in `store.go`, following the established `ALTER TABLE ADD COLUMN` pattern. The column joins the `projects` sync wire row (LWW on `updated_at`); no new sync logic was needed.

Reminders client: `Lists(ctx) ([]string, error)` and `ListItems(ctx, listName) ([]Reminder, error)` added to the `Client` interface and implemented in both `eventkitClient` (real, via `c.Lists()` and `c.Reminders(WithList(name), WithCompleted(false))`) and `Fake` (new `AllLists`, `ItemsByList` fields). `ListInbox` on the real client simplified to delegate to `ListItems(ctx, InboxListName)`.

TUI:
- Project selection screen: projects with an associated list show a dim `[ListName]` tag. Pressing `l` on a highlighted project opens a `uiExtraRemListPicker` overlay showing all available lists (loaded async) plus `0) None (clear)`. Selection calls `SetProjectRemindersList` and updates the in-place project entry.
- Category selection screen: when the selected project has a `reminders_list_name` and items are available, a dim `From <list>:` section header separates the reminder items from the existing categories. Items show as `• Title` and are cursor-navigable. `listLen()` and `cursorVal()` updated to span both sections; `handleCategoryInput` handles `rem:N` selections.
- Footer hint on the project screen updated to show `l set reminders list` when EventKit is available.

103 tests pass (30 new: 3 store, 5 reminders, rest indirectly exercised).

---

## 2026-05-26 — Cancel session (v1.3.0)

Added a `cancel` command on the Active and Paused screens. Typing `cancel` discards the in-progress session (no DB row written, in-flight captures dropped) and returns to the start screen.

Implementation:
- `Machine.Cancel(now)` in `internal/session/session.go`: valid from Active or Paused, resets the machine to `StateIdle` via a zero-value struct assignment (same pattern as `NewSession`). Returns `ErrWrongState` from any other state.
- 3 new tests in `session_test.go`: cancel from Active (verifies captures are discarded and all fields cleared), cancel from Paused, cancel from wrong state.
- TUI: `cancel` case in `handleActiveCommand` mirrors the crash-sentinel "n" discard path — clears the sentinel, calls `initStartup()`, reloads inbox or projects depending on what `initStartup` resolved. Footer hints on Active and Paused screens updated to include `cancel`.

No schema changes. 73 tests pass.

---

## 2026-05-25 — Auto-sync on startup and after session save (v1.2.0)

After a few days of real use, "Auto-sync" was removed from the out-of-V1 list and implemented. Two trigger points:

1. **On TUI startup.** `Init()` fires a background sync when `JACKTASKS_SYNC_URL` and `JACKTASKS_SYNC_TOKEN` are set. Fire-and-forget — the start screen and inbox render normally; the sync result arrives as a `syncDoneMsg` and updates a status indicator.
2. **After session save.** When `sessionSavedMsg` lands (session is on local disk), kick off the same background sync. Skipped if another sync is already in flight.

The session is always saved locally first — sync failure never blocks the flow. The manual `s) Sync now` start-screen action and the `jacktasks sync` CLI subcommand remain as escape hatches.

Visual cue: a small status line — `⟳ syncing…`, `✓ synced <age>`, or `✗ sync failed <age>` — rendered on the start screen (below the menu) and on the WhatNext screen (below the actions). Manual sync still takes over the start screen with the existing "Syncing…" spinner; auto-sync is intentionally non-intrusive.

Implementation notes:
- `runSyncCmd` now takes a `manual bool` and stamps it onto `syncDoneMsg.manual`. The handler treats manual and auto differently: manual resets cursor/placeholder and surfaces errors as `errMsg`; auto silently updates `lastSyncAt` / `lastSyncOK` and refreshes `syncSummary` only when the user is still on the start screen.
- `newModel` pre-sets `m.syncing = true` when sync is configured so that an "s) Sync now" press immediately after launch doesn't race the startup auto-sync. (Init returns only a Cmd, so it can't mutate the model itself.)

Server-side "latest version" check (so a stale binary on the Mac Mini shows an "update available" hint) is deliberately deferred — it'd need a wire-protocol change plus a publish step. Will revisit.

Bumped to v1.2.0 (new sync behavior in the TUI; manual sync path preserved).

---

## 2026-05-25 — Tab to extend from end-notes (v1.1.0)

Added a shortcut on the post-session note screen: pressing **Tab** reverses the End and extends the live session by 5 minutes, returning to Active. Motivation: when a timer expires while the user is still mid-flow, they want to keep going as one continuous session — not split into a saved-then-continued pair. This approximates the existing `ext` command for the case where they missed it before the timer hit zero. Once back in Active, they can `ext <n>` for further extensions.

Design choice debated and revised: an earlier attempt jumped from Tab into the existing "Continue session" path (save + new session with same project/category). User pushed back: they want it logged as one continuous session, not two adjacent ones. The current implementation reverses End, so no save happens until the user actually ends for real later.

Implementation:
- New `Machine.ResumeFromEndingNotes(now)` in `internal/session/session.go`. Requires `StateEndingNotes`, clears `endedAt`/`status`/`endNotes`, and resets `targetEnd` to `now` if it had already passed (so a follow-up `Extend` produces meaningful remaining time). Returns to `StateActive`.
- `handleEndingNotesExtend` in `cmd/jacktasks/model.go` chains `ResumeFromEndingNotes` + `Extend(5, now)` and re-focuses the textinput on the active command line. Any text already typed into the note textarea is discarded — the session is continuing, so there are no end notes yet.
- Tab is intercepted in `updateKey` before the textarea consumes it, so Enter and free-text still work as before.
- Banner text on the end-notes screen now reads `Enter to skip · Tab for +5m`.
- Sentinel is rewritten after the extend so crash recovery sees the new targetEnd.
- If the session was started from an inbox reminder, the pending reminder disposition is left intact — it'll fire on the next real End, as expected.
- 5-min default lives in `tabExtendMinutes` (single constant in `model.go`).

Tests: added `TestResumeFromEndingNotesAfterTimerExpiry`, `TestResumeFromEndingNotesEarlyEnd`, `TestResumeFromEndingNotesWrongState`. All 73 tests pass.

Bumped to v1.1.0 (new interaction path / new machine method).

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

---

## 2026-05-23 — Phase 2: Core session loop

Goal: pure session state machine + thin stdin driver. No Bubble Tea yet.

**Done:**
- Renamed `SessionAbandoned` → `SessionEndedEarly` (`"ended_early"`) in the store. "Abandoned" implied discarded data; "ended early" is a normal intentional stop and supports resume-on-restart. This was a correctness fix — the resume feature checks for this status by value.
- Added `LatestSession` to the store (fetches most-recent session by `started_at`).
- `internal/session/` package: pure state machine, no I/O. `Machine` struct tracks all in-memory session state. `BeginSetup` → `SetCategory` → `SetProject` → `SetDuration` → `Active` ↔ `Paused` → `EndingNotes` → `WhatNext` → (Continue / New / Break / End / Idle). All methods take explicit `now time.Time`.
- Duration accounting: `actual = (ended_at − started_at) − sum(pause intervals)`. Target end shifts forward on resume so the session still aims for the same working time.
- `ToStoreSessionInput` converts completed in-memory session to `store.CreateSessionInput`. Only valid after `End` has been called.
- `cmd/jacktasks/main.go` replaced with a stdin driver: resume-on-restart offer, category/project selection with inline creation, active command loop (`upn`, `ext`, `pause`, `resume`, `end`), end notes, store write on session end, what-next screen.
- Bug caught during smoke test: `Machine{}` zero value starts in `StateIdle`, and the run loop returned immediately. Fixed by adding `BeginSetup()` as an explicit Idle→SetupCategory transition, keeping Idle as a clean terminal state.

**Result:** 39 passing tests across all packages. Full session flow exercised manually.

**Trade-offs explicitly accepted:**
- Resume creates a new session row; the `ended_early` row is left as-is. Simple and append-only, consistent with the sync model.
- stdin driver is intentionally throwaway. It exercises the session package but will be replaced wholesale by Bubble Tea in Phase 3. No effort spent polishing the prompts.

## Phase plan (remaining)

| Phase | Goal | Status |
|---|---|---|
| 0 | Spike: prove go-eventkit + Tailscale | ✅ closed |
| 1 | Data layer with tests | ✅ closed |
| 2 | Core session loop with stdin prompts | ✅ closed |
| 3 | Bubble Tea TUI replacing prompts | ✅ closed |
| 4 | Reminders integration | ✅ closed |
| — | Design fix: invert project ↔ category | ✅ closed |
| 5 | Crash recovery / state persistence | ✅ closed |
| 5.5 | TUI polish (Lipgloss + Bubbles components) | ⬜ next |
| 6a | Sync protocol design + server skeleton | ⬜ |
| 6b | Client `jacktasks sync` subcommand | ⬜ |
| 6c | Deploy ThinkCentre + verify cross-Mac | ⬜ |

Time estimate: Phase 5 + 5.5 in today's session to enable a daily-driver trial on the MacBook. Phase 6 split across 3 mid-week sessions once real data exists to validate against.

---

## 2026-05-23 — Phase 3: Bubble Tea TUI

Goal: replace the throwaway stdin driver with a proper Bubble Tea TUI. `internal/session/` unchanged.

**Done:**
- Added dependencies: `charmbracelet/bubbletea v1.3.10`, `charmbracelet/lipgloss v1.1.0`, `charmbracelet/bubbles v1.0.0`.
- `cmd/jacktasks/main.go` reduced to store open + `tea.NewProgram(m, tea.WithAltScreen())`.
- `cmd/jacktasks/model.go`: full Bubble Tea model. One `textinput.Model` component, reconfigured per screen. `uiExtra` enum handles sub-states that don't map to `session.Machine` states (entering a new category/project name; entering duration for "continue session").
- All screens implemented: resume offer, category selection (list + inline create), project selection (list + inline create), duration, active command loop, paused, end notes, what-next, break countdown.
- Timer auto-ends the session when it hits zero (via `tickMsg` handler). Break auto-ends after 5 minutes (tracked as `breakEnd time.Time` in the model, not in the session package).
- `saveSessionCmd` snapshots `CreateSessionInput` and captures in the main goroutine before dispatching, so machine state changes mid-flight can't corrupt the write.
- 39 tests passing (all in `internal/`; no TUI tests — correct, the session package covers the logic).

**Trade-offs explicitly accepted:**
- No TUI-layer tests. The session package covers all logic; the TUI is glue. Manual smoke testing covers the rest.
- Break duration (5 min) is hardcoded in the TUI model, not the session package. Session package tracks `breakStart` but has no concept of a break target — that's fine since break duration is purely a display concern.
- Session data is written to the store while the user is on the WhatNext screen. There is a sub-second window where End/New Session could race the write, but the snapshot approach makes this safe.
- Alt screen enabled (`tea.WithAltScreen()`). Restores terminal on quit cleanly.

---

## 2026-05-23 — Pre-Phase 4 design decisions

**Decided: nullable `project_id` in sessions.**

A session belongs to a category and optionally a project. The current schema enforces NOT NULL on `project_id`, which breaks down for one-off captures ("email Sarah", "look up that thing") that fit a category but no ongoing project. Making it nullable is the honest data model.

Rejected alternative: a per-category "misc" sentinel project. Avoids the schema change but pollutes the project list and is semantically wrong.

Migration approach: recreate the sessions table with `project_id TEXT REFERENCES projects(id)` (no NOT NULL), copy existing rows, drop old table, rename. All queries that join on `project_id` become LEFT JOINs. Go layer: NULL ↔ empty string at the scan boundary, same pattern as `end_notes`.

TUI impact: project selection screen gets a permanent "no project" option. Active screen shows "Category / —" when no project is set.

**Decided: "Do" action on captures routes to normal session setup, not a pre-filled project name.**

On the What-Next capture disposition screen, "Do" marks the capture cleared and starts a new session setup flow (category → optional project → duration). The capture text is shown as context but doesn't pre-fill any field — user picks category, then picks a project, creates one, or skips ("no project"). This keeps the setup flow consistent and avoids the UX confusion of auto-naming a project from freeform capture text.

---

## 2026-05-23 — Phase 4: Reminders integration

Goal: nullable `project_id`, capture disposition, Reminders push, inbox pull on launch.

**Done:**
- **Schema migration:** `sessions.project_id` made nullable. `migrateSessionsProjectIDNullable` in `store.go` detects the old NOT NULL schema via `PRAGMA table_info` and rebuilds the table in a single-connection transaction (required to keep `PRAGMA foreign_keys=OFF` in scope). Migration is idempotent. New test `TestMigrateSessionsProjectIDNullable` exercises the full old-schema → migration → new-schema path.
- **Go layer:** `CreateSession` and `scanSession` updated to treat empty string as NULL for `project_id` (same pattern as `end_notes`). `CreateCapture` signature changed to accept an explicit `id` parameter so session-layer UUIDs align with DB IDs — needed for capture disposition lookups.
- **Session machine:** `SetProject("")` now valid (no-project is a legitimate choice, not an error).
- **TUI — "no project":** Project selection screen shows `0) — no project`. Active/paused display and resume offer show `—` when project is empty. `checkResume` handles no-project sessions without calling `GetProject`.
- **`internal/reminders/`:** New package. `Client` interface with `ListInbox`, `Add`, `Complete`. `eventkitClient` wraps `go-eventkit`. `Fake` for tests. 4 tests.
- **Capture disposition on WhatNext:** `c<n>` (clear), `r<n>` (send to Reminders), `d<n>` (do/start session for capture). Optimistic UI — captures disappear from display immediately, reappear on error. `capturesDisposed` map tracks state per WhatNext visit. `d<n>` saves capture text to `doContextText`, shown as dim context during the subsequent setup flow.
- **Startup screen:** `uiExtraStart` replaces the old resume y/N prompt. Async inbox fetch via `loadInboxCmd` (fires in `Init`). While loading: "Checking inbox…". On load: numbered inbox items, resume option (if applicable), `n` for new session, `q` to quit. If inbox loads empty and no resume: skips directly to category setup. EventKit failure non-fatal — logs to stderr, proceeds without inbox option. `completeInboxItemCmd` marks selected inbox item done in EventKit.
- 48 tests passing.

**Trade-offs explicitly accepted:**
- `PRAGMA foreign_keys=OFF` during migration: the rebuild runs inside a transaction on a dedicated `*sql.Conn` to keep the pragma in scope. The `database/sql` pool would scatter it across connections otherwise.
- Capture disposition writes to the DB asynchronously after the user acts. There's a narrow window where a force-quit could leave a capture undisposed in the DB (cleared in the TUI but not marked in the store). Phase 5 crash recovery will close this gap.
- Inbox item completion (`completeInboxItemCmd`) is fire-and-forget; failures are silently discarded. If EventKit rejects the complete, the item stays in Reminders. Acceptable for V1.
- `sendToRemindersCmd` creates the Reminders item first, then marks the DB flag. If the DB write fails after a successful Reminders write, the item is in Reminders but not flagged. Unlikely in practice; logged via error rollback on the TUI.
- `doContextText` persists in the model until the next session starts; it doesn't flow into the session record itself, only into the display. This is intentional — the capture text is context for the user, not a field on the session.

---

## 2026-05-23 — Pre-Phase-5 design fix: invert project ↔ category

After hands-on testing of the Phase 4 build, the user identified that the project/category relationship is backwards. Categories were modeled as global (a top-level taxonomy), with projects as children of a category. The intended model is the opposite: **projects are the top-level grouping; categories are per-project sub-labels.** "Coding" or "Planning" only makes sense in the context of a specific project.

This decision predates implementation — the cleanup happens before Phase 5 (crash recovery) starts. Local dev DB will be dropped and recreated; no migration code needed.

**New model:**

```sql
projects    (id, name, created_at, updated_at, deleted_at?, archived)
                                                      -- no category_id
categories  (id, name, project_id→projects, created_at, updated_at, deleted_at?, archived)
                                                      -- project_id NULLABLE; NULL = no-project category
sessions    (id, project_id→projects, category_id→categories NOT NULL, ...)
                                                      -- project_id nullable, category_id always set
```

**The "no project" / category rules** (subtle — read carefully):

- **"No project" is a valid, permanent option** on the project selection screen. Some sessions are genuinely project-less.
- **Category is always required**, regardless of whether a project was selected. There is no "no category" path.
- **When a project is selected**: the category screen shows the pre-populated list of existing categories *for that project* plus an `n` option to create a new one. Categories are scoped to their project — picking project "jacktasks" does not surface categories from project "homelab".
- **When "no project" is selected**: the category screen does **not** show a list. The user always types a name (or it's prefilled from a capture/reminder text — see below). The category row is stored with `project_id IS NULL`. These no-project categories are never offered as a pickable list on subsequent runs; each no-project session re-enters its category fresh.
  - Backend dedup: when a no-project category name is entered, do a lookup on `(name, project_id IS NULL)` — reuse the existing row if found, otherwise insert a new one. Keeps the data clean for later analytics without surfacing a list in the UI.
- **Prefill from capture/reminder**: when the session was started via "Do" on a capture or by selecting an inbox reminder, the capture/reminder text prefills the category name input on the category screen. User can accept it as-is or edit before submitting. This applies in both the project-selected and no-project paths, but in the project-selected path the prefill goes into the "new category" input that appears after pressing `n` (or you can extend it to prefill on the list screen too — implementer's call, flag if uncertain).

**Setup flow:** `Idle → SetupProject → SetupCategory → SetupDuration → Active`. SetupCategory is **never skipped**. What differs between the project-selected and no-project paths is the *UI on the SetupCategory screen* (list vs. free-text-only), not the state machine.

**Plan of work** (for the implementing session):

1. **Schema (`internal/store/schema.sql`)** — rewrite to the shape above. Drop `projects.category_id`. Add `categories.project_id NOT NULL REFERENCES projects(id)`. Make `sessions.category_id` nullable. Drop `idx_projects_category`; add `idx_categories_project`. Delete the Phase-4 `migrateSessionsProjectIDNullable` block in `store.go` — the new schema starts nullable, no migration needed.
2. **DAL**:
   - `projects.go`: `CreateProject(ctx, name)` drops `categoryID`. Replace `ListProjectsByCategory` with `ListProjects(ctx)`. `scanProject` drops `category_id`. `store.Project` loses `CategoryID`.
   - `categories.go`: `CreateCategory(ctx, name, projectID string)` — `projectID` may be empty for no-project categories (maps to NULL via the existing empty-string-↔-NULL pattern). Replace `ListCategories` with `ListCategoriesByProject(ctx, projectID string)` returning categories where `project_id = ?`. Add `FindCategoryByNameNoProject(ctx, name)` (or fold into a `CreateOrGetCategory` helper) for the no-project dedup-on-insert path. `scanCategory` adds `project_id` (nullable). `store.Category` gains `ProjectID` (empty string when NULL).
   - `sessions.go`: `category_id` stays `NOT NULL` in SQL and required non-empty in `CreateSessionInput`. `project_id` keeps its existing NULL↔"" mapping. Drop the Phase-4 nullable-`project_id` migration block.
3. **Session machine (`internal/session/session.go`)**:
   - Reorder states: `BeginSetup()` lands in `StateSetupProject`. `SetProject(id, now)` always advances to `StateSetupCategory` (empty id is allowed and means "no project"). `SetCategory(id, now)` requires non-empty id, advances to `StateSetupDuration`.
   - `Reset()` returns to `StateSetupProject`.
   - End-of-session snapshot: `category_id` always non-empty, `project_id` may be empty. Assert that in `End()`.
4. **TUI (`cmd/jacktasks/model.go`)**:
   - On entering setup, load **projects** first. Project selection screen keeps the `0) — no project` option.
   - **Project selected path**: fire `loadCategoriesCmd()` (now `ListCategoriesByProject(projectID)`), then show the category screen with the pre-populated list + `n` to create a new category. New-name creation: `CreateCategory(ctx, name, machine.ProjectID())`.
   - **No-project path**: do not load a category list. Show the category screen with just an input prompt (no numbered list). The submitted name resolves via `CreateOrGetCategory(ctx, name, "")` — dedup against existing no-project categories by name; otherwise insert a new row with `project_id IS NULL`.
   - **Capture/reminder prefill**: when `doContextText` is non-empty, prefill the category input with it on the SetupCategory screen. In the project-selected path, this means the new-category input (after pressing `n`) starts pre-populated; in the no-project path, the input itself is pre-populated. User can edit before submit. Clear `doContextText` once consumed.
   - Active screen displays `<project or —> / <category>`. Category is never `—` (always set).
   - `resumeInfo`: `categoryName` always present; `projectName` may be empty (renders `—`).
   - "Do" / inbox flow: after selecting a capture or inbox item, enters `StateSetupProject` (was Category), with `doContextText` carrying the text forward to prefill the category later.
   - View copy: project screen says "Select a project"; category screen says "Select a category for <project name>" in the project path, or "Enter a category" in the no-project path.
5. **Tests**: rewire fixtures (`internal/store/*_test.go`) so categories are created with a project (or with empty project for the no-project case), projects no longer take a category. Add a test for `CreateOrGetCategory` dedup on the no-project path. `internal/session/session_test.go`: reorder setup tests and add tests for (a) project-selected → category-from-list flow, (b) no-project → category-typed flow, (c) `End()` invariant that `category_id` is always non-empty. Existing 48 tests are mostly mechanical name/order swaps.
6. **Docs**: update `PROJECT.md` (Schema section, Session model state diagram, Phase-4 description — `project_id` was nullable from the start of the new schema, so the "Phase 4 migration" sentence about nullable `project_id` should be removed). Append to this LOG when done.
7. **Local data**: user runs `rm "~/Library/Application Support/jacktasks/jacktasks.db"` before first run on the new schema.

**Order of work:** schema + DAL → session machine → TUI → docs. After each step: `go build ./... && go vet ./... && go test ./...` green before moving on.

**Explicitly *not* doing:**
- No `UNIQUE(project_id, name)` constraint on categories. Name uniqueness stays a UI nicety, not a DB invariant, matching how project names are handled today.
- No auto-created "general" category when a new project is made. The user creates the first category via `n`, same flow as today. Reconsider if it becomes annoying in real use.

---

## 2026-05-23 — Pre-Phase-5 design fix: invert project ↔ category (implemented)

Implemented the design documented above. All changes were made in one pass; tests stayed green throughout.

**What changed:**

- **Schema rewritten.** `projects` is now the root table (no `category_id`). `categories` gains `project_id TEXT REFERENCES projects(id)` (nullable). `sessions.category_id` is NOT NULL; `sessions.project_id` remains nullable. Local dev DB dropped and recreated — no migration code needed or written. The Phase-4 `migrateSessionsProjectIDNullable` migration and its test were deleted.
- **DAL.** `Project` struct drops `CategoryID`. `CreateProject(ctx, name)` — no category arg. `ListProjectsByCategory` replaced by `ListProjects`. `Category` gains `ProjectID`. `CreateCategory(ctx, name, projectID)` — projectID empty = no-project category stored as NULL. `ListCategories` replaced by `ListCategoriesByProject(ctx, projectID)`. Added `CreateOrGetCategoryByName(ctx, name, "")` for no-project dedup: looks up by `(name, project_id IS NULL)`, reuses if found, otherwise inserts.
- **Session machine.** `BeginSetup()` → `StateSetupProject`. `SetProject` (valid from SetupProject) advances to `StateSetupCategory`. `SetCategory` requires non-empty ID, advances to `StateSetupDuration`. `NewSession()` resets to `StateSetupProject`.
- **TUI.** Project selection is now the first setup screen. After project selection (or "no project"), category screen appears. For a selected project: shows that project's categories with "n" for new. For no-project: shows a free-text input; `doContextText` pre-populates it if set; submitting calls `CreateOrGetCategoryByName`. Active/paused display reordered to `project / category`. Resume, "New session", "Do" capture, and inbox item selection all route to `loadProjectsCmd` first.
- **Tests.** `sessionFixtures` creates project then category-under-project. `projectsFixtures` no longer needs a parent category. Added `TestCreateOrGetCategoryByName`. Session machine tests reordered to match new state flow. 48 tests passing.

**Trade-offs accepted:**
- No-project categories are stored but never shown in a list — they accumulate silently. The dedup-by-name keeps the count reasonable, and the data is useful for analytics later. Revisit if the table grows unexpectedly.
- `CreateOrGetCategoryByName` does a SELECT then INSERT (not an upsert). Two concurrent calls with the same name on the same device are impossible (single-user TUI), so the race window doesn't matter.

---

## 2026-05-23 — Plan for Phases 5, 5.5, and 6

Goal: get jacktasks to a daily-driver state on the MacBook by EOD so the user can trial it for a week. Sync (Phase 6) intentionally deferred to mid-week so it can be validated against real session data.

Order is fixed: **5 → 5.5 → (trial period) → 6a → 6b → 6c**. Do not reorder without surfacing it.

### Phase 5 — Crash recovery / state persistence

**Problem:** In-flight session state (started_at, pauses, captures, target end) lives only in `session.Machine`. A crash, terminal close, or accidental `q` during Active/Paused loses the session entirely — nothing is written to SQLite until `End()`.

**Solution:** An `active.json` sentinel file in the data dir, written on every meaningful state transition. On startup, if it exists and references a session UUID not yet in the DB, offer to recover.

**Filesystem path:** `~/Library/Application Support/jacktasks/active.json` (already listed in PROJECT.md's filesystem table).

**Sentinel contents** (JSON):
```json
{
  "session_id":          "uuid",
  "project_id":          "uuid or empty",
  "project_name":        "display only, denormalized",
  "category_id":         "uuid",
  "category_name":       "display only, denormalized",
  "planned_duration_min": 25,
  "started_at":          1716480000,
  "target_end_at":       1716481500,
  "pauses":              [{"start": 1716480300, "end": 1716480360}, ...],
  "current_pause_start": 1716480400,  // present only if state == Paused
  "captures":            [{"id": "uuid", "text": "...", "captured_at": 1716480200}, ...],
  "state":               "active" | "paused",
  "written_at":          1716480450
}
```

Denormalized names so the recover prompt can render without DB lookups (project/category may have been edited or soft-deleted).

**New package: `internal/recovery/`**
- `recovery.go`: `type Sentinel struct { ... }` matching the JSON shape. `Write(dir string, s Sentinel) error` (atomic write: temp file + rename). `Read(dir string) (*Sentinel, error)` (returns nil, nil if file absent — not an error). `Clear(dir string) error` (idempotent — no error if already gone).
- `recovery_test.go`: round-trip write/read, atomic write doesn't corrupt on partial state, Clear-when-absent is no-op, Read-when-malformed returns explicit error.

**Session machine changes (`internal/session/session.go`):**
- Add `Snapshot() Sentinel` method on `Machine` — pure, builds the sentinel from current state. Only valid in `StateActive` or `StatePaused`; returns zero value + error otherwise.
- Add `Hydrate(s Sentinel, now time.Time) (*Machine, error)` constructor — rebuilds a `Machine` in `StateActive` or `StatePaused`. Validates that started_at ≤ now and that current_pause_start (if set) is consistent with `state == "paused"`.
- Do **not** auto-persist from inside the session package — it stays pure I/O-free. The TUI is responsible for calling `Snapshot()` and writing it via the recovery package.

**TUI changes (`cmd/jacktasks/model.go`):**
- After every transition into or within Active/Paused, and after each `upn`/`ext`/`pause`/`resume`, call `recovery.Write(dataDir, m.machine.Snapshot())` as a `tea.Cmd`. Errors logged to stderr but non-fatal — recovery is best-effort.
- On clean session end (after the DB write in `saveSessionCmd` succeeds), call `recovery.Clear(dataDir)`.
- On startup, before the existing inbox/resume start screen logic: call `recovery.Read(dataDir)`. If non-nil **and** the session UUID is not found in the DB (use `GetSession` returning `ErrNotFound`), show a new pre-start screen:
  ```
  Recover unfinished session?

  <project> / <category> — started 14m ago, N captures
  Planned: 25m   Elapsed (working): 12m

  y) Resume        n) Discard
  ```
  - `y` → hydrate the machine, jump straight to `StateActive` (or `StatePaused` if that's what the sentinel said). The recover-on-restart resume-from-`ended_early` feature is unrelated and unaffected.
  - `n` → call `recovery.Clear`, proceed to normal startup.
- If `Read` returns a sentinel whose UUID **is** in the DB, the previous run completed cleanly but crashed before `Clear` — silently clear the file and proceed normally.

**Tests:**
- `recovery` package: round-trip, atomic-write, clear-when-absent, malformed-read.
- `session` package: `Snapshot()` round-trip via `Hydrate()` (build machine → snapshot → hydrate → verify state equal). Snapshot-while-Idle returns error. Hydrate with `state="paused"` but no `current_pause_start` returns error.
- No TUI tests (consistent with Phase 3 — TUI is glue).

**Out of scope for Phase 5:**
- Capture disposition crash recovery (the narrow window noted in the Phase 4 trade-offs). Captures live in the DB once the session ends; only the disposition flags can be lost. Acceptable for V1.
- Sentinel versioning. Single field `"version": 1` added but no migration code. Bump only matters once we ship.

### Phase 5.5 — TUI polish (bounded)

**Goal:** make the daily-driver experience feel like a real Charm app, not `fmt.Sprintf` lines. No new screens, no flow changes — same state machine, prettier surface.

**Allowed dependencies:** the `charmbracelet/bubbles` components already pulled in transitively (`list`, `progress`, `spinner`, `help`, `key`). No new third-party deps.

**Concrete deliverables:**

1. **`cmd/jacktasks/styles.go`** — new file. Single Lipgloss palette + named styles:
   ```go
   var (
     StyleTitle    = lipgloss.NewStyle()...
     StyleSubtitle = ...
     StyleDim      = ...
     StyleAccent   = ...
     StyleError    = ...
     StyleKeyHint  = ...
     StyleSelected = ...
     StyleHeader   = ...
     StyleFooter   = ...
   )
   ```
   One place to retune colors. Use `lipgloss.AdaptiveColor` for light/dark terminal support.

2. **Persistent header + footer** (rendered in every `View()`):
   - Header: app name on the left, current screen name in the middle, when in Active/Paused: `<project> / <category> — MM:SS / planned`.
   - Footer: context-sensitive key hints rendered via `bubbles/help` (`?` toggles short/full). Each screen exposes a `keyMap` for help to render.

3. **`bubbles/list` for selection screens:**
   - Project select, category select (project-selected path only), what-next actions, capture disposition list.
   - Keep numeric shortcuts working (`1`-`9` jumps to that item). Add arrow nav, filter-as-you-type, proper selection highlight.
   - The no-project category screen stays free-text (no list to swap).
   - The startup screen (inbox + resume + new) also becomes a list.

4. **`bubbles/progress` for timers:**
   - Active timer: progress bar from 0 → planned duration, with `MM:SS / MM:SS` overlay. Shifts beyond 100% when over-time (use a different style past 100%).
   - Break countdown: same, 0 → 5 min.

5. **`bubbles/spinner` for async ops:**
   - "Checking inbox…" on startup.
   - "Saving session…" briefly on what-next entry (the async DB write window).

6. **`bubbles/help` for `?` toggle:**
   - Short help in footer by default. `?` toggles full help (multi-line). Each screen's `keyMap` declares its bindings.

**What does NOT change in Phase 5.5:**
- State machine. Same states, same transitions.
- Session package. No changes.
- DAL. No changes.
- Screen flow / order. Same.

**Tests:** none added. TUI stays untested by design.

### Phase 6 — Sync (mid-week, after a few days of trial use)

Split into three sub-phases so each is one session.

**Phase 6a — Protocol design + server skeleton**

**New repo dir:** `cmd/jacktasks-sync/` (server binary). **Or** new repo entirely — flag for user when starting 6a. Default to same repo, sub-command of the same module.

**Storage:** `/var/lib/jacktasks-sync/master.db` on the ThinkCentre. Same schema as the client DB (reuse `internal/store/schema.sql`).

**Auth:** shared bearer token via `JACKTASKS_SYNC_TOKEN` env var. Bind to Tailscale interface only (`tailscale0` or the Tailscale IP).

**Protocol (REST, JSON):**

```
GET  /healthz                              → 200 OK
POST /push?table=<name>                    → body: {"rows": [...]}
                                             returns: {"accepted": N, "rejected": [...]}
GET  /pull?table=<name>&since=<unix_sec>   → returns: {"rows": [...], "as_of": <unix_sec>}
```

Tables synced: `projects`, `categories`, `sessions`, `captures`. Not synced: `config` (per-device device_id), `sync_state` (per-device bookkeeping).

**Conflict rules:**
- `sessions`, `captures`: pure append. Insert-or-ignore by UUID on both sides.
- `projects`, `categories`: last-write-wins on `updated_at`. `deleted_at` tombstone wins over any update with earlier `updated_at`. On push: server compares incoming `updated_at` to its row; takes newer. On pull: client does the same.
- Capture mutable flags (`cleared`, `sent_to_reminders`): treat as last-write-wins on a new `updated_at` column on captures. **Schema change needed** — add `captures.updated_at INTEGER NOT NULL DEFAULT 0` via a real migration (first migration since Phase 4). Surface this when 6a starts.

**Wire format (per row):** flat JSON object matching the table columns. Timestamps as Unix seconds. NULLs as JSON null (not empty string — wire format is stricter than the Go ↔ SQL boundary).

**Deliverables for 6a:**
- `cmd/jacktasks-sync/main.go`: HTTP server, auth middleware, the four routes.
- `internal/syncproto/`: shared types (`PushRequest`, `PushResponse`, `PullResponse`, per-table row structs) used by both server and client.
- `internal/syncserver/`: handler logic, conflict resolution, idempotent inserts.
- Captures `updated_at` migration + DAL update + test.
- Document the wire format in a new `## Sync protocol` section of `PROJECT.md` *before* writing the code.

**Phase 6b — Client `jacktasks sync` subcommand**

- Subcommand parsing in `cmd/jacktasks/main.go`: `jacktasks` (TUI, default) vs `jacktasks sync` (one-shot).
- Reads `JACKTASKS_SYNC_URL` and `JACKTASKS_SYNC_TOKEN` from env. Errors clearly if missing.
- For each table: read `sync_state.last_push_at`, push rows with `updated_at > last_push_at` (or `created_at` for append-only tables). On success, update `last_push_at`. Then read `last_pull_at`, GET `/pull?since=last_pull_at`, apply rows per conflict rules, update `last_pull_at` to server's `as_of`.
- Push before pull (so local changes are visible to remote on next iteration).
- Output: a short summary per table — `projects: pushed 2, pulled 0`. Errors are fatal — partial sync is fine (the state is updated as each table completes).
- Tests: in-memory HTTP server (`httptest`), drive a push/pull/conflict scenario per table.

**Phase 6c — Deploy + cross-Mac verification**

- Build server binary on MacBook for `linux/amd64`.
- `scp` to ThinkCentre. systemd unit at `/etc/systemd/system/jacktasks-sync.service`. Env file with the token. `systemctl enable --now`.
- From MacBook: `jacktasks sync` → check master DB has the rows.
- From Mac Mini (first run): bootstrap the local DB, then `jacktasks sync` → confirm pull lands MacBook's data.
- Run a session on Mac Mini, sync, run a session on MacBook, sync, verify both sides converge.
- Document the deploy steps in `PROJECT.md` under a new `## Deployment` section.

### What goes into V1.0 vs. V1.1

V1 ships after Phase 6c. The "out of V1" list in `PROJECT.md` stays out. If anything from that list bites during the week of trial, log it but don't implement.

### Implementation handoff notes

- Work strictly in phase order: 5 → 5.5 → (stop, hand back for trial) → 6.
- After each phase: `go build ./... && go vet ./... && go test ./...` green. Then ask whether to update `PROJECT.md` / append a `LOG.md` entry (per `CLAUDE.md`).
- Match existing patterns documented in `CLAUDE.md` — error wrapping, UUIDs, epoch seconds, `scanX` helpers, input structs, `t.Helper()` in test helpers.
- Do not auto-update `LOG.md` for sub-phase progress. Append once Phase 5 closes; again once 5.5 closes; etc.
- The user wants daily-driver readiness by EOD on Phase 5 + 5.5 only. Stop and hand back after 5.5 — do not start Phase 6 in the same session.

---

## 2026-05-23 — Phase 5: Crash recovery

Goal: persist in-flight session state so a crash, terminal close, or accidental quit doesn't lose the session.

**Done:**

- **`internal/recovery/`** — new package. `Sentinel` struct (JSON, epoch seconds, denormalized names). `Write` (atomic temp+rename), `Read` (nil,nil if absent), `Clear` (idempotent). 6 tests: round-trip, read-absent, clear-when-absent, clear-removes-file, malformed-read, no-stray-tmp.
- **`internal/paths/`** — added `DBPathFromDir(dir string) string` so `main.go` can call `DataDir()` once and derive the DB path without a second call.
- **`internal/session/`** — `Snapshot(now, projName, catName)` serializes Active/Paused state into a `recovery.Sentinel`; errors on any other state. `Hydrate(sentinel, now)` reconstructs a `Machine`; validates paused-state has `CurrentPauseStart`, validates `started_at < now`. 6 new tests: active round-trip, paused round-trip, snapshot-while-idle errors, hydrate-paused-without-pause-start errors, hydrate-invalid-state, snapshot-with-completed-pause.
- **`cmd/jacktasks/model.go`** — `dataDir` field on Model. Recovery check in `newModel` (sync: file read + DB lookup): if sentinel found and UUID not in DB, show `uiExtraRecover` screen; if UUID in DB, silently clear. `uiExtraRecover` screen shows project/category, started-N-min-ago, capture count, planned duration; y/n input. `y` hydrates and jumps to Active/Paused. `n` clears sentinel and runs normal startup (resume check + start screen or project selection). `writeSentinelCmd()` called after: `SetDuration` (enters Active), `upn`, `ext`, `pause`, `resume`, `ContinueSession`, ended_early resume (start screen `r`), crash-recovery `y`. `clearSentinelCmd()` called in `sessionSavedMsg` handler after successful DB write. Both are best-effort (errors → stderr, not shown in TUI).

**Result:** all 5 packages pass (paths, recovery, reminders, session, store). Build and vet clean.

**Trade-offs accepted:**
- Sentinel write is async (tea.Cmd), so there's a brief window between a state change and the write completing. This is a best-effort feature — it closes the large crash window (multi-minute sessions) not the microsecond one.
- `recovery.Read` errors in `newModel` are logged to stderr. The TUI is not yet running so we can't show them in-band; they're non-fatal regardless.
- Sentinel is not versioned beyond `"version": 1`. No migration code exists. A future format change would need a version check in `Read`; deferred until needed.

---

## 2026-05-23 — Phase 5.5: TUI polish

Goal: make the daily-driver experience feel like a real Charm app. No state-machine or flow changes.

**Done:**

- **`cmd/jacktasks/styles.go`** — new file. Lipgloss palette using `AdaptiveColor` (light/dark terminal support). Named styles: `StyleTitle`, `StyleAccent`, `StyleDim`, `StyleError`, `StyleSelected`, `StyleCursor`, `StyleHeader`, `StyleFooter`, `StyleTimer`, `StyleActive`, `StylePaused`, `StyleBorder`. Key bindings and per-screen `screenKeyMap` structs implementing `help.KeyMap`.
- **Persistent header** — rendered on every screen. Three-column layout: `jacktasks` (left) / screen name (center) / `project/category MM:SS/planned` (right, Active/Paused only). Separated from content by a `─` rule sized to terminal width.
- **Persistent footer** — `─` rule + key hints. Active/Paused screens render plain-text command hints (`upn <text>  ext <n>  pause  end`) because those commands are free-typed and `bubbles/help` skips bindings with no key triggers. All other screens use `bubbles/help` (arrow nav + Enter + ^C).
- **Arrow-key cursor on list screens** — project select, category select (project path), what-next actions, start screen (inbox + resume + new + quit). `↑/↓` or `k/j` moves a `▶` cursor. Pressing Enter with an empty text input submits the cursor item. Numeric shortcuts preserved.
- **`bubbles/progress` bar** — Active (0→planned, smooth gradient), Paused (same, static while paused), Break (0→5min). Animated via `progress.FrameMsg`. Width tracks terminal width.
- **`bubbles/spinner`** — "Checking inbox..." during startup inbox fetch; "Saving session..." briefly on WhatNext while the DB write is in flight. `savingSession bool` flag on the model gates display and spinner ticks.
- **New indirect dep** — `github.com/charmbracelet/harmonica v0.2.0`, required by `bubbles/progress`. Added to `go.sum`; no new direct `go.mod` entry.

**Result:** all 5 packages pass. Build and vet clean. TUI manually verified via tmux: project/category selection with cursor nav, active session with progress bar and footer hints, break countdown, what-next screen, end flow.

**Trade-offs accepted:**
- No `bubbles/list` component. The plan specified it but the numeric-plus-cursor approach achieves the same UX with less structural change and keeps the existing handler logic intact. The cursor gives visual selection feedback; arrows give keyboard nav; numbers give power-user shortcuts. `bubbles/list` would have required replacing the entire input model per screen — more churn than value.
- `bubbles/help` is not used for Active/Paused footers because the component skips bindings that have no key triggers (our commands are free-typed strings, not key bindings). Plain-text footer is functionally equivalent and cleaner.
- Progress bar shows 0% at session start even though one second has elapsed. The `SetPercent` call fires on the first tick; the sub-second gap is imperceptible.

---

## 2026-05-23 — Phase 6a: Sync protocol + server skeleton

Goal: schema migration for LWW on capture flags, shared wire types, HTTP sync server.

**Done:**

- **`captures.updated_at` migration** — `migrateCapturesUpdatedAt` in `store.go` detects the column via `PRAGMA table_info`, adds it via `ALTER TABLE ADD COLUMN`, backfills `updated_at = created_at` for existing rows, then creates `idx_captures_updated`. Index creation moved out of `schema.sql` (where it would fail on old DBs lacking the column) and into the migration function, which always runs it idempotently. `MarkCaptureCleared` and `MarkCaptureSentToReminders` now stamp `updated_at = now`. `Capture` struct gains `UpdatedAt`. `TestMigrateCapturesUpdatedAt` exercises old-schema → migration → new-schema path including backfill.

- **`internal/syncproto/`** — new package. `PushRequest`, `PushResponse`, `PullResponse`, `HealthResponse` wire types. `SyncedTables` ordered slice (projects → categories → sessions → captures; FK-safe insertion order). Table name constants. Used by both server and (upcoming) client.

- **`internal/store/sync.go`** — `PullSince(ctx, table, sinceUnix)` generic pull using `tableColumns` and `pullColumn` maps; returns `[]map[string]any` with raw SQLite types ready for JSON. `UpsertFromSync(ctx, table, rows)` dispatches to per-table functions:
  - `upsertProject` / `upsertCategory`: `INSERT ... ON CONFLICT(id) DO UPDATE SET ... WHERE excluded.updated_at > <table>.updated_at` (last-write-wins)
  - `upsertSession`: `INSERT OR IGNORE` (pure append, immutable)
  - `upsertCapture`: `INSERT ... ON CONFLICT(id) DO UPDATE SET cleared, sent_to_reminders, updated_at WHERE excluded.updated_at > captures.updated_at` (LWW on flags only)

- **`cmd/jacktasks-sync/main.go`** — server binary. Reads `JACKTASKS_SYNC_TOKEN`, `JACKTASKS_SYNC_DB`, `JACKTASKS_SYNC_ADDR` from env (all required). Opens store, wires mux, calls `http.ListenAndServe`.

- **`internal/syncserver/server.go`** — `NewMux` builds the handler. Auth middleware checks `Authorization: Bearer <token>` on all routes except `/healthz`. `handleHealthz` returns `{"ok":true}`. `handlePush` decodes body, calls `UpsertFromSync`, returns accepted/rejected counts. `handlePull` reads `since` query param (defaults 0), calls `PullSince`, returns rows + `as_of` timestamp. Empty pulls return `[]` not `null`.

- **`internal/syncserver/server_test.go`** — 8 tests via `httptest.Server`: healthz no-auth, auth rejection (4 variants), unknown table 400, push/pull round-trip for projects, projects LWW (newer wins, stale loses), sessions append-only dedup, captures flag LWW, empty-pull returns array, missing-ID rejection.

**Result:** all packages pass (57 tests). Build and vet clean. `cmd/jacktasks-sync` compiles; no new dependencies.

**Trade-offs accepted:**
- `map[string]any` wire format means JSON decode produces `float64` for all numbers on the receiving end. The upsert SQL handles this correctly (SQLite converts `float64(1.0)` to INTEGER 1 under INTEGER affinity). Test assertions on pull results must use `.(float64)`, not `.(int64)`. Documented here so Phase 6b client code doesn't make the same mistake.
- `UpsertFromSync` counts a row as "accepted" if no SQL error occurred, even when the LWW WHERE clause prevented an update. "Accepted" means "processed without error", not "row changed". This is the right semantic — the client doesn't need to know which rows were suppressed by LWW.
- Server binary is in the same repo. Build on Mac, scp to ThinkCentre — no GitHub credentials on the server. Phase 6c covers the deploy steps.

---

## 2026-05-23 — Phase 6b: Client `jacktasks sync` subcommand

Goal: push-before-pull sync loop from the Mac client, subcommand dispatch, `sync_state` bookkeeping.

**Done:**

- **`internal/store/syncstate.go`** — `SyncState` struct (LastPullAt, LastPushAt int64; both 0 = never synced). `GetSyncState` returns a zero struct for tables with no row (not an error). `SetLastPushAt` and `SetLastPullAt` each upsert only their own column via `ON CONFLICT DO UPDATE` — they do not clobber each other. 4 tests.

- **`internal/store/projects.go`** — `UpdateProject(ctx, id, name)` added. Was deferred from Phase 1; needed now for the LWW convergence test and real use. Bumps `updated_at = now`.

- **`internal/syncclient/client.go`** — `Config{URL, Token}`. `Sync(ctx, store, cfg, out)` iterates `syncproto.SyncedTables` in FK-safe order and calls `syncTable` for each. `syncTable` flow:
  1. Read `last_push_at` from `sync_state`.
  2. Snapshot `pushAt = now` before reading local rows (so rows created during the HTTP call are caught next sync).
  3. `PullSince(ctx, table, last_push_at)` → POST rows to server.
  4. `SetLastPushAt(pushAt)` on success.
  5. Re-read `sync_state` for `last_pull_at`.
  6. GET `/pull?since=last_pull_at` from server.
  7. `UpsertFromSync(ctx, table, rows)` locally.
  8. `SetLastPullAt(as_of)` on success.
  - Partial sync is safe: bookmarks are updated per-table before moving on.
  - Output: one formatted line per table (`projects:    pushed 2, pulled 0`).

- **`cmd/jacktasks/main.go`** — refactored into `runSync` and `runTUI`. `os.Args[1] == "sync"` dispatches to `runSync`; all other invocations launch the TUI. `runSync` reads `JACKTASKS_SYNC_URL` + `JACKTASKS_SYNC_TOKEN` from env, errors clearly if either is missing.

- **`internal/syncclient/client_test.go`** — 5 tests via `httptest.Server` + `syncserver.NewMux`:
  - `TestSyncRoundTrip`: Mac A creates project/category/session/capture, syncs up; Mac B syncs down and verifies all four entities.
  - `TestSyncIdempotent`: first sync pushes non-zero rows; second sync (no new data) pushes 0 on every table.
  - `TestSyncLWWConvergence`: Mac A updates a project and syncs, then Mac B makes a newer competing update and syncs; Mac A's final sync pulls Mac B's winning version. (2-second sleep to guarantee distinct updated_at values; test structured so Mac A's lastPullAt is set before Mac B's competing update.)
  - `TestSyncBadToken`: wrong token returns an error on the first table.
  - `TestSyncMissingConfig`: empty URL or token rejected before any network call.

**Result:** 68 tests across all packages. Build and vet clean. `jacktasks sync` dispatches correctly; `go run ./cmd/jacktasks sync` with missing env vars prints a clear error and exits 1.

**Trade-offs accepted:**
- Mac A pulling its own data back on the first sync (when `lastPullAt = 0`) is expected and harmless — `UpsertFromSync` no-ops via LWW when the incoming row is not newer. The bookmark advances after the first pull so subsequent syncs don't re-pull.
- `doPush` treats any server rejection as a fatal error for that table. If the server rejects a row (e.g., FK violation because a parent row hasn't synced yet), the whole table sync fails. In practice this shouldn't happen because `SyncedTables` is ordered projects → categories → sessions → captures (parents before children), so FKs are always satisfied on the server before children arrive.
- `UpdateProject` added to DAL here rather than in Phase 1 because it was only needed at sync time. No schema change.

---

## 2026-05-23 — Phase 6c: Deploy artifacts + cross-Mac verification prep

Goal: produce everything needed to deploy the sync server on the ThinkCentre and verify cross-Mac convergence.

**Done:**

- **`Makefile`** — `make check` (build + vet + test; pre-commit gate), `make install` (TUI to `/usr/local/bin`), `make build-sync-linux` (cross-compile `jacktasks-sync` for `linux/amd64`). Cross-compilation verified: statically linked ELF, no libc dependencies, will run on ThinkCentre without any runtime install.

- **`deploy/jacktasks-sync.service`** — systemd unit. Runs as dedicated `jacktasks` system user (no login shell). Reads env from `EnvironmentFile=/etc/jacktasks-sync/env`. `Restart=on-failure` with 5s back-off.

- **`deploy/env.template`** — template for `/etc/jacktasks-sync/env` with the three required vars: `JACKTASKS_SYNC_TOKEN` (generate with `openssl rand -hex 32`), `JACKTASKS_SYNC_DB` (`/var/lib/jacktasks-sync/master.db`), `JACKTASKS_SYNC_ADDR` (ThinkCentre Tailscale IP + port).

- **`deploy/DEPLOY.md`** — step-by-step deploy guide. Covers: cross-compile + scp, first-time service user + directory setup, env file, systemd unit install, healthz smoke test, Mac client env vars, first sync from MacBook, first sync from Mac Mini (bootstraps its local DB then pulls MacBook's data), convergence check (sessions from both devices visible on both machines via `GROUP BY device_id`), binary update procedure, log tailing.

- **`PROJECT.md`** — `## Deployment` section added with summary commands; `## Build, test, run` updated to reference `make`; directory structure updated to include `deploy/` and `Makefile`.

**Result:** all code complete. 68 tests passing. The remaining work is operational (run on hardware) and is fully documented in `deploy/DEPLOY.md`.

**Notes for the operational phase:**
- Generate the token once (`openssl rand -hex 32`), add it to `/etc/jacktasks-sync/env` on the ThinkCentre and to `~/.zshrc` on both Macs before running the first sync.
- The ThinkCentre's Tailscale IP goes in `JACKTASKS_SYNC_ADDR` (server) and `JACKTASKS_SYNC_URL` (clients). Run `tailscale ip -4` on the ThinkCentre to get it.
- On the Mac Mini's first run, launch `jacktasks` once (not `jacktasks sync`) to bootstrap the local DB and accept the Reminders TCC permission prompt. Then `jacktasks sync` to pull MacBook's data.

---

## 2026-05-23 — Phase 6c: Actual deploy (MacBook side complete)

Goal: stand up `jacktasks-sync` on the ThinkCentre and verify the MacBook can push/pull.

**Done:**

- ThinkCentre Tailscale IP: `100.70.19.55`. Service user `jacktasks` created. Binary at `/usr/local/bin/jacktasks-sync`. Data dir `/var/lib/jacktasks-sync/`. Env file `/etc/jacktasks-sync/env` (0640, root:jacktasks). systemd unit installed and `enable --now`'d. `curl http://100.70.19.55:8484/healthz` returns `{"ok":true}`.
- MacBook: `sudo make install` to `/usr/local/bin/jacktasks`. `JACKTASKS_SYNC_URL` + `JACKTASKS_SYNC_TOKEN` exported in `~/.zshrc`. `jacktasks sync` confirmed working — pushed local data to the server.

**Issues hit during deploy (all documented and fixed in DEPLOY.md):**

1. **DEPLOY.md had `/path/to/env.template` placeholders.** The repo isn't on the ThinkCentre, so step 3/4 had nowhere to copy from. Fixed: step 2 now scp's the binary + env template + service file together to `/tmp/`; step 3/4 `mv` them into final locations.
2. **`$(tailscale ip -4):8484` in the env file.** systemd's `EnvironmentFile` does **not** expand shell command substitutions. Server logs showed `serve: listen tcp: lookup $(tailscale ip -4): no such host`. Fixed: env file now requires a literal IP; DEPLOY.md updated with explicit warning.
3. **`make install` failed with permission denied on macOS.** `/usr/local/bin/` needs sudo on macOS (unlike many Linux distros). Fixed: Makefile now honors `PREFIX` (default `/usr/local/bin`); user can `sudo make install` or `make install PREFIX=$HOME/.local/bin`.

**Pending (next session, away from Mac Mini):**

- Run `jacktasks` (TUI) on Mac Mini once to bootstrap the local DB and accept the Reminders TCC permission prompt.
- Export the same `JACKTASKS_SYNC_URL` + `JACKTASKS_SYNC_TOKEN` in Mac Mini's `~/.zshrc`.
- `jacktasks sync` on Mac Mini → expect "pushed 0, pulled N" for every table (initial sync down from server).
- Run a session on Mac Mini, sync. Then sync on MacBook. Verify both `device_id`s appear in `sessions` table on both machines.

---

## 2026-05-23 — Pre-trial UI polish

Two small additions before starting the trial period. No new features, no scope expansion.

**Done:**

- **ASCII logo on startup screen.** `cmd/jacktasks/logo.go` — block-letter "JackTasks" banner in `StyleTitle` purple, rendered above the inbox/resume/new menu. Self-hides when terminal width < `logoWidth + 2` (72 chars) so narrow terminals fall back to the existing layout.
- **`s) Sync now` on the startup menu.** Reads `JACKTASKS_SYNC_URL` + `JACKTASKS_SYNC_TOKEN` from env at TUI launch (`main.go`), plumbs a `syncclient.Config` through to `newModel`. When both env vars are non-empty, the menu shows `s) Sync now` between `n) New session` and `q) Quit`; selecting it runs `syncclient.Sync` in a `tea.Cmd`, shows the existing spinner with "Syncing...", then prints the per-table summary (or error) in `StyleDim` under the menu. If env vars aren't set, the option is hidden — no change in behavior. New `syncDoneMsg`, new `runSyncCmd`, `isLoading` extended to include `m.syncing`, and `listLen` / `cursorVal` updated so arrow-key navigation accounts for the optional row.

**Trade-off accepted:** sync still depends on env vars being exported in the launching shell. If the trial surfaces that as annoying, the post-trial fix is a config file or a `.env` next to the DB — not a blocker for V1.

**Build/test:** clean. 68 tests pass.

**Operational note:** client-only change, so only `sudo make install` on each Mac is needed. ThinkCentre server is untouched.

---

## 2026-05-24 — Sync bug fix: arrived_at

**Bug:** Mac Mini synced first (empty DB, `last_pull_at` set to ~now). MacBook then synced and pushed its sessions — but those sessions had `created_at` timestamps from days earlier. Mac Mini's subsequent syncs ran `WHERE created_at > last_pull_at` on the server and got zero rows. Late-arriving data was permanently invisible.

**Root cause:** The pull filter column (`pullColumn["sessions"] = "created_at"`) is a client-side timestamp reflecting when the session happened, not when it arrived at the server. Any device that has already advanced its `last_pull_at` past the data's `created_at` will never see it, no matter how many times it syncs.

**Fix:** Added `arrived_at INTEGER NOT NULL DEFAULT 0` to all four sync tables (`projects`, `categories`, `sessions`, `captures`). The server stamps `arrived_at = time.Now().Unix()` on every row it receives via `/push`. The server's `/pull` handler now calls `PullSinceArrived` (filters `WHERE arrived_at > since`) instead of `PullSince` (filters on `created_at`/`updated_at`). Client-side `PullSince` — used to gather local rows to push — is unchanged.

**Migration:** `migrateArrivedAt` in `store.go`, same pattern as `migrateCapturesUpdatedAt`. Runs on both server and client DBs via `Open`. For existing rows, backfills `arrived_at` from `updated_at` (projects, categories) or `created_at` (sessions, captures) so a fresh pull (`since=0`) can still retrieve pre-migration data. Arrived_at indexes created in the migration, not in `schema.sql`, to avoid the same ordering hazard that affected `idx_captures_updated`.

**One-time client fix:** Mac Mini's `sync_state.last_pull_at` had already advanced past the MacBook's data. Reset it to 0 on Mac Mini to force a fresh pull of all server data.

**Also fixed:** `deploy/DEPLOY.md` "Updating the server binary" procedure was missing `chmod 755` after the binary `mv`. The first-time setup had it; the update section didn't. Hit this during deploy when systemd couldn't exec the new binary.

**Result:** 68 tests pass. Wire format unchanged (`arrived_at` is server-only, never transmitted).

---

## 2026-05-24 — Post-deploy bug fixes and polish

Issues found during first real use on the Mac Mini, plus a few UX improvements.

**Start screen skip removed.** When inbox loaded empty with no resume candidate, the app jumped straight to project setup, skipping the logo and menu entirely. Removed the skip — the start screen always renders.

**Near-complete sessions auto-complete.** `End()` in `session.go` now marks a session `completed` when ≤5 min remain (`plannedSec - actualSec <= 5*60`), rather than `ended_early`. Prevents the resume offer from appearing for sessions that were essentially finished. `checkResume` also raised its suppression threshold from `remaining <= 0` to `remaining <= 5` to catch any pre-fix `ended_early` rows already in the DB. New test `TestEndNearCompleteIsCompleted` pins the boundary (5 min remaining = completed, 6 min = ended_early).

**End notes word wrap.** The End Notes screen was using a single-line `textinput`; long notes ran off the right edge. Replaced with a `textarea` component (already in `charmbracelet/bubbles`). Text wraps at terminal width, Enter still submits, no flow changes. Width tracks terminal resize via `WindowSizeMsg`.

**j/k vim navigation.** The footer key hints already showed `↑/k` and `↓/j` (declared in `styles.go`), but the keys weren't actually handled — `tea.KeyRunes` fell through to the text input. Added a `case tea.KeyRunes` branch in the list-screen navigation block so `"j"` and `"k"` move the cursor and return before the input is updated. No menu shortcuts use j or k, so no conflicts.

**Tokyo Night gradient logo.** The ASCII logo previously rendered in a flat `StyleTitle` purple. Now each rune gets its own foreground color interpolated left-to-right across three Tokyo Night stops: `#bb9af7` (purple) → `#7aa2f7` (blue) → `#7dcfff` (cyan). Color is computed in `logo.go` via a simple linear interpolation between stops; no new dependencies.

**Result:** 70 tests pass.

---

## 2026-05-24 — Versioning and install path

**Introduced SemVer versioning (v1.0.0).** The single source of truth is `VERSION := 1.0.0` in the Makefile. The binary bakes it in via `-ldflags "-X main.Version=$(VERSION)"`. `cmd/jacktasks/version.go` holds the Go-side `var Version` with the same value as a default so `go run` also shows the right version. The version is displayed on the start screen below the logo in `StyleDim`.

**Bump rule (codified in CLAUDE.md):** any change that requires the user to run `make install` gets a version bump. PATCH for bug fixes and polish; MINOR for new commands, screens, or sync behavior; MAJOR for breaking schema changes. Both `VERSION` in `Makefile` and `Version` in `version.go` are updated in the same commit.

**Install path changed from `/usr/local/bin` to `~/.local/bin`.** The only reason to use `/usr/local/bin` was that it's already in PATH — avoiding a `.zshrc` edit. Switching to `~/.local/bin` (add `export PATH="$HOME/.local/bin:$PATH"` to `~/.zshrc` once) lets `make install` run without `sudo`. Makefile `PREFIX` default changed accordingly; `mkdir -p $(PREFIX)` added to the install target so the directory is created if absent. `PROJECT.md` and `CLAUDE.md` updated.

**Tests:** 70 passing, unchanged.

---

## 2026-05-24 — Flashing end-notes banner (v1.0.1)

A solid block of end-notes text was easy to miss when a session ended — the screen looked similar to Active/Paused and the user sometimes sat in front of it not realizing the session was over.

**Done:** Added `StyleFlashOn` (white-on-red) and `StyleFlashOff` (red-on-default) in `styles.go`. New `flashOn bool` on the Model toggles on every `tickMsg` while in `StateEndingNotes` *and* `noteArea.Value() == ""` — i.e. only flashes while the user hasn't started typing. As soon as a keystroke lands in the textarea, the banner is replaced with the calmer `End notes (Enter to skip):` prompt and `flashOn` is forced false. Banner text: `▶  SESSION ENDED — add notes or press Enter to skip  ◀`.

Version bumped from 1.0.0 → 1.0.1 in `Makefile` and `cmd/jacktasks/version.go`. Requires `make install` on each Mac.

**Tests:** 70 passing, unchanged (TUI-only change).

---

## 2026-05-24 — Defer Reminders completion to end of session (v1.0.2)

**Bug:** Selecting an inbox reminder on the startup screen immediately marked it complete in Apple Reminders (via `completeInboxItemCmd` batched alongside `loadProjectsCmd`). If the user then abandoned or never finished the session, the reminder was lost from Reminders. Risky — selection ≠ completion.

**Fix:** Reminders completion is now deferred until the user has worked the session and submitted end notes. New flow:

1. Selecting an inbox item stashes `pendingReminderID` and `pendingReminderTitle` on the Model. No EventKit call is fired at selection time. `doContextText` still carries the reminder title forward into setup for the "Doing: …" hint.
2. After end notes are submitted, if `pendingReminderID != ""`, the input intercepts the save and shows a new `uiExtraReminderDispo` screen overlaying `StateEndingNotes`:
   ```
   Mark reminder complete?
   "<reminder title>"
   y) Yes, mark complete    n) No, keep it active
   ```
3. `y` fires `completeInboxItemCmd` alongside `saveSessionCmd`. `n` skips the EventKit call and just saves. Either way pending fields are cleared and the machine transitions to WhatNext.

**Implementation notes:**
- The dispo screen overlays `StateEndingNotes` rather than introducing a new session-package state — kept the pure state machine untouched. The textarea/textinput switch in `Update` and the renderer both check `m.extra != uiExtraReminderDispo` to use the y/n textinput instead of the notes textarea.
- If a session is force-quit before reaching EndingNotes, `pendingReminderID` (in-memory only) is lost. The reminder stays active in Apple Reminders, which is the desired safer default — it'll show up again on next launch.
- Sessions started via "Resume", "New Session", or a capture-`d` action never set `pendingReminderID`, so the dispo screen doesn't appear in those flows.

Version bumped from 1.0.1 → 1.0.2 in `Makefile` and `cmd/jacktasks/version.go`. Requires `make install` on each Mac.

**Also in this commit:** `CLAUDE.md` rule change — LOG.md entries are now mandatory on every code-changing session (previously ask-first). The flashing-banner entry above was backfilled because it had slipped through under the old rule.

**Tests:** 70 passing, unchanged (TUI-only change).
