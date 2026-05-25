# ROADMAP

Post-V1 candidate features. Not a committed sequence — items are organized by approximate difficulty and design-readiness so future-you can scan and decide what to pick up. Reorder, demote, and delete freely. When work actually starts on an item, promote the design into a phase plan in `PROJECT.md` and append the usual entry to `LOG.md`.

`PROJECT.md` is "what is true now." `ROADMAP.md` is "what might come next." `LOG.md` is the record of what happened. Don't confuse them.

---

## Near-term — ready to spec

These have clear design, small scope, and unlock or enable later work. Each is a single focused session.

### Cancel session

A `cancel` command on Active/Paused that ends the session with no DB record, no resume eligibility, and discards in-flight captures. Crash sentinel is cleared. Returns to the start screen.

Design notes:
- In-flight captures are dropped intentionally. They live in memory only at this point; "cancel" semantics imply "this didn't happen." If real loss-aversion shows up in trial use, offer a one-line "discard N captures?" confirmation, but don't pre-build it.
- No schema work. Pure machine-state + TUI change.

### Daily HUD: today's totals, session count, intra-day streak

Persistent display in the header or near the timer: today's total *working* minutes, total *break* minutes, session count, and current uninterrupted-session streak. Cheap query (group sessions by day). No schema change.

Design notes:
- Streak-within-today definition: consecutive sessions without a long gap. Define "long gap" as ≥30 min between session end and next session start (or app quit). Tunable later in TOML if it doesn't feel right.
- HUD lives in the TUI. Historical stats live in the web UI (see below). Different time horizons, different surfaces — keep them separate or the TUI grows into territory that's deliberately on the "out of V1" list.

### Shortcuts hooks on session transitions

User-configured macOS Shortcuts run on session-start, break-start, break-end, and session-end. Primary use case: toggling focus modes. Implementation is roughly `exec.Command("shortcuts", "run", name)` driven by TOML config.

Design notes:
- A single in-app flag `hooksEnabled` gates all transition hooks. Toggled from anywhere via `h`. Header displays current state (`hooks: on` / `hooks: off`).
- Default for `hooksEnabled` at app launch is configurable in TOML (default true if any hooks are configured; functionally off if no hooks are set).
- All-or-nothing toggle, not per-transition. Add granular control only if a real need surfaces.
- Failures are non-fatal and logged to stderr; the session continues. Same pattern as EventKit failure handling.

### TOML config foundation

Introduce `~/.config/jacktasks/config.toml` (path already declared in PROJECT.md as "planned, not yet needed"). First real consumers: shortcuts hooks above, session-count target below.

Design notes:
- Single-pass load on app start. No hot-reload. Restart to apply changes — this is a TUI, not a daemon.
- Missing file is fine — defaults everywhere. Don't write a default file on first run; leave the user to create it.
- Validation: parse errors print to stderr and exit non-zero rather than silently falling back. The user wants to know.

---

## Medium — design done, scope clear

Larger surface than the items above, but the design is settled. Each is one focused session of work, or maybe two.

### Dailies and Weeklies (recurring category targets)

Categories can carry an optional recurring target — daily or weekly — with an optional minute goal and (for dailies) an optional weekday schedule. Sessions accumulate against the target naturally because they're already category-scoped.

Examples:
- "Keybr — 30 min/day, weekdays only" (daily)
- "Reading — 20 min/day, every day" (daily)
- "Weekly review — 30 min/week" (weekly)
- "Publish a blog post — once/week, no minute target" (weekly, presence-only)

Schema:
- `categories.target_minutes INTEGER` — NULL = no minute goal (any session this period counts as completion).
- `categories.target_period TEXT` — NULL = no recurrence; otherwise `day` or `week`.
- `categories.schedule_mask INTEGER` — only meaningful when `target_period = 'day'`; 7-bit field (bit 0 = Mon, bit 6 = Sun). NULL = every day. `0b0011111` = weekdays.

UI:
- Inline edit on the existing category selection screen. Cursor highlights a category; press `t` to open a small input for period + minute target + (if daily) schedule. No new screen, no management UI.
- HUD shows progress for the active session's category against its current period: "Keybr: 12/30 min today" or "Weekly review: 15/30 min this week."
- Streak per recurring target is computed at query time from `sessions`, not stored. Days outside `schedule_mask` (for dailies) keep the daily streak alive; weeks where the target was met keep the weekly streak alive.

Design notes:
- Dailies and Weeklies share schema and UI — one feature, two scheduling modes. Could be shipped separately (daily first) but the design is unified.
- Category-scoped, not project-scoped. Project-level targets aren't needed for current use cases and would muddy the model.
- The MMO framing implies "quests." Resist the urge to add a Quest entity — it duplicates categories. Dailies and Weeklies *are* categories with recurring targets.
- Week boundary: Monday-to-Sunday by default (ISO 8601). Configurable in TOML if Sunday-start ever becomes a real preference.
- NULL `target_minutes` with non-NULL `target_period` = "any session this period counts as done." Useful for tasks where consistency matters but minutes don't ("ship a weekly post").
- These are personal data and sync naturally via existing categories sync (LWW on `updated_at`). No new sync work.
- Migration follows the established pattern (ALTER TABLE ADD COLUMN, same as `captures.updated_at` and `arrived_at`).

### Session-count target (TOML)

Single TOML setting: `daily_session_target = N`. HUD shows progress against it. Doesn't sync — per-device preference, not data.

Design notes:
- Promote to a table later if per-weekday targets or per-device differences become real. Don't pre-build it.

---

## Larger — real design surface

These have substantive design work ahead. Spec before estimating.

### Web UI on the ThinkCentre

A separate Go binary (`jacktasks-web` or similar) running on the ThinkCentre alongside `jacktasks-sync`. Reads the master DB read-only. Serves an HTML/JS interface with charts and historical views.

Open design questions:
- **Stack:** server-rendered Go templates + a chart library (e.g. Chart.js loaded as a CDN script) vs. an SPA. Lean server-rendered for V1 — simpler, no build pipeline, no JS framework lock-in.
- **Read-only vs. read-write:** V1 read-only. Editing past sessions or categories from the web stays out unless it becomes painful.
- **TUI integration:** a menu option on the start screen that prints the URL (or opens it via `open`). No deep integration.
- **Auth:** same bearer token as `jacktasks-sync`, or a separate session-cookie scheme for the browser? Probably the latter — bearer tokens in URLs are awkward.

Design notes:
- The TUI explicitly does *not* grow a stats screen. The web UI is where reflection happens.
- PWA + notifications is a much later evolution. Don't anchor V1 web-UI design decisions to it.
- Sibling service, not folded into `jacktasks-sync`. Keeps the sync binary small and lets the web UI evolve independently.

### Enhanced Reminders integration

Currently jacktasks reads from a single `jacktasks-inbox` list. The `rem` documentation suggests richer queries are possible — overdue, due today, multi-list — and `go-eventkit` likely supports them too.

Open design questions:
- Does the startup screen merge sources (overdue first, today next, inbox last) or pick one at launch?
- Is the source list user-configurable in TOML (named lists, filter expressions)?
- Does "Do" on an overdue or today reminder complete it in EventKit the same way inbox items are?
- Does the user want to *see* the source classification (overdue vs. today vs. inbox) in the picker, or a flat ordered list?

Design notes:
- Don't pick a UX here yet. The trial period should produce real demand signals — "I wanted to see today's stuff and couldn't" is a useful complaint that hasn't been surfaced yet.

### Hierarchical projects with file-based authoring

A project can be defined with an ordered sequence of steps. Sessions against such a project select a step instead of a free category. After end notes, the user is prompted whether the step is complete; marking complete advances the project's cursor.

Source of truth model: an external file (markdown, probably) defines the project structure. `jacktasks import <file>` ingests it. Post-import, the DB owns the state and syncs normally. To modify: edit the file and re-import.

Flow:
- Current: Project → Category → Duration → Active → End notes → WhatNext
- New (stepped projects): Project (marked as stepped) → Step (ordered, uncompleted first) → Duration → Active → End notes → "Did you complete '\<step\>'? y/n" → WhatNext

Open design questions — all genuinely need answers before this can be specced:

1. **Schema shape.** Extend `categories` (add `order_index`, `completed_at`, `is_step`) vs. new `project_steps` table referencing project + category. The separate table keeps categories pure and is probably the right call.

2. **File format.** Markdown is the user-preferred starting point — easy to author, readable in any editor. Likely dialect:
   ```markdown
   # Project Name
   ## Steps
   - [ ] First step text
   - [x] Already-completed step
   - [ ] Another step
   ```
   Top-level `- [ ]` / `- [x]` lines are steps; everything else is ignored. Strict enough to parse cleanly. JSON or TOML are fallbacks if markdown identity issues prove unmanageable.

3. **Step identity across re-imports.** The hard problem. If a step is renamed in the source file, is it the same step (just retitled) or a new one? Three approaches:
   - **Stable UUIDs written back into the file:** `- [ ] {abc123} step text`. Robust against rename, slightly ugly. Probably the right answer.
   - **Fuzzy text matching at re-import:** tolerates rename but fails on similar steps and is hard to debug.
   - **Sidecar mapping file:** `project.md` plus `project.md.jacktasks-ids.json`. Clean separation but two files to keep together.
   Resolve before writing the importer.

4. **File location and sync.** Source files are not synced; the DB is. Implications: the file only needs to exist on the machine where authoring/editing happens; step definitions and completion state sync via the DB; re-import from another machine requires copying the file (or maintaining it in a git repo somewhere). Acceptable for the single-user case.

5. **Ad-hoc work within a stepped project.** Sometimes work doesn't map to a step ("fix bug found during step 3"). Either allow free category selection as an escape hatch, or require everything to be a step. The escape hatch is probably right; force is annoying.

6. **Sync of completion.** Step completion is a state change on a single row, not a new session row. Use LWW on a `completed_at` column. Sync as part of the steps table.

Design notes:
- Steps replace categories within a stepped project; they don't coexist. The step *is* the work.
- Mark-completion happens after session end, not during. Sessions log work regardless of completion status — you can do five sessions on one step before marking it done.
- Project authoring stays out of the TUI by design. Keeps TUI complexity down and means the source file is always primary.
- This is the largest feature on the roadmap. Strongly recommend running with Dailies/Weeklies and the smaller items first — real workflow data will sharpen this spec significantly.

---

## Future companion tools

These are *not* jacktasks features. They're plausibly useful adjacent tools that might integrate with jacktasks via existing surfaces (the WhatNext screen, captures, etc.) but have their own data models and runtime concerns.

### Incoming-message summaries

A separate tool that monitors message sources during sessions (iMessage via something like `imsg`, Outlook/Teams via M365 hooks) and produces a short LLM-generated summary of what arrived. Surfaced on the WhatNext screen as context.

Why this isn't a jacktasks feature:
- Different data model (messages, summaries, sources).
- Integrations are unrelated to time tracking (message sources, LLM provider).
- Runtime concerns are different (LLM calls cost money, have latency, require API keys).
- Combining them stretches the scope of jacktasks's TUI and sync model without benefit.

If pursued: a separate binary that writes summaries to a known location (JSON file? an inbox-style table in a separate DB?). jacktasks reads them passively at session boundaries. Loose coupling — either side can be replaced or disabled without breaking the other.

---

## Standing rules for this file

- Add ideas here as they surface. A one-line entry under the right section is fine — design notes come when an idea is being seriously considered.
- Reorder freely. Sections reflect current best understanding, not commitment.
- When work actually starts on an item, write a phase plan in `PROJECT.md`, append a `LOG.md` entry, and either delete the item from here or leave a one-liner pointing to the PROJECT.md section.
- Items that get tried and rejected: move to a "Considered and rejected" section at the bottom with one sentence of reasoning. Future-you will want to know why.
