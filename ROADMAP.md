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

### Dailies (category-level targets)

Categories can carry an optional daily target in minutes and an optional weekday schedule. Examples: "Keybr — 30 min, weekdays only," "Reading — 20 min, every day." Sessions accumulate against the target naturally because they're already category-scoped.

Schema:
- `categories.target_minutes INTEGER` — NULL = no target.
- `categories.schedule_mask INTEGER` — NULL = every day; otherwise a 7-bit field (bit 0 = Mon, bit 6 = Sun). `0b0011111` = weekdays.

UI:
- Inline edit on the existing category selection screen. Cursor highlights a category; press `t` to open a small input for target + schedule. No new screen, no management UI.
- HUD shows progress toward today's relevant Daily during an active session ("Keybr: 12/30 min today").
- Streak per Daily is computed at query time from `sessions`, not stored. Days outside `schedule_mask` don't break the streak.

Design notes:
- Category-scoped, not project-scoped. Project-level targets ("30 min on jacktasks in any category") aren't needed for the keybr-style use case and would muddy the model. Add later only if a real need surfaces.
- The MMO framing implies "quests." Resist the urge to add a Quest entity — it duplicates categories. Dailies *are* categories with targets.
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
