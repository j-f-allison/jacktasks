# CLAUDE.md

Instructions for Claude (chat or Code) when working on this project. Read `PROJECT.md` first for context and `LOG.md` for the running history of decisions.

## Current handoff state (2026-05-26)

Post-V1 feature development in progress. 103 tests pass. Three new features shipped in rapid succession (see LOG.md for details):
- v1.3.0: Cancel session (`cancel` command on Active/Paused)
- v1.4.0: Per-project Reminders list (schema migration + EventKit generalization + TUI)
- Phase 9 (TOML + daily_session_target) is next — not yet started.

**Next pickup:**
- Phase 9 (TOML config foundation + `daily_session_target` consumer). Requires a TOML dep (`github.com/BurntSushi/toml`) — confirm with user before adding.
- No known bugs or blockers.

**Do not** add new features beyond the three phases agreed upon without asking.

## Communication style

These are the user's general preferences. They apply unless overridden in the moment.

- **Calibrated honesty, not flattery.** State actual assessments. If something is wrong, say so plainly without softening preamble.
- **Question premises, not just conclusions.** If the user is asking the wrong question or working from a bad assumption, say so before answering inside the frame they gave.
- **Disagree confidently.** Don't hedge real objections as "things to consider." Don't pad responses with manufactured concerns to appear balanced.
- **Hold the line on push-back.** Don't reverse a position because the user pushed back. If they actually changed your mind with a real argument, say what changed it.
- **Distinguish genuine problems from minor notes.** Signal which is which. Not every observation deserves equal weight.
- **Don't manufacture criticism to seem rigorous.** If the work is good, say it's good.

## Versioning

The app uses SemVer (`MAJOR.MINOR.PATCH`). The single source of truth is `VERSION` in `Makefile`. The Go var `Version` in `cmd/jacktasks/version.go` holds the same value as a default for `go run`.

**When to bump:**
- Any change that requires the user to run `make install` (new binary on disk) gets a version bump.
- PATCH: bug fixes, polish, no new commands or screens.
- MINOR: new commands, new screens, new sync behavior, new config options.
- MAJOR: breaking schema changes requiring migration, complete rewrites of core flows.

**How to bump:** edit `VERSION` in `Makefile` and `Version` in `cmd/jacktasks/version.go` to match. Do both in the same commit.

## Workflow

- **Tests must pass before moving on.** After any code change, run `go test ./...` and confirm green before claiming a chunk is done.
- **Build cleanly.** `go build ./...` and `go vet ./...` should both be clean.
- **One table or feature at a time.** When implementing the DAL pattern was established, each table got its own file + test file, added one at a time. Continue that pattern.
- **Don't expand scope without asking.** V1 has a known feature set (see PROJECT.md). New features go in the "out of V1" list, not into the codebase.
- **Don't add dependencies without asking.** Current deps: `modernc.org/sqlite`, `github.com/google/uuid`, `github.com/BRO3886/go-eventkit` (planned). New deps need a real reason.

## Established Go patterns in this codebase

These patterns are consistent across the existing code. New code should match.

- **Error wrapping.** Use `fmt.Errorf("context: %w", err)`. Callers compare with `errors.Is`. Sentinel errors like `ErrNotFound` are exported from the package that owns the relevant data.
- **Unix epoch seconds for time storage.** SQLite stores `INTEGER`. Go layer converts to `time.Time` at the boundary. Don't introduce mixed-precision timestamps without a reason.
- **UUID strings as primary keys.** `uuid.NewString()` in the Go layer, `TEXT PRIMARY KEY` in SQL. No autoincrement integers — they break cross-device sync.
- **`scanX` helper per table.** Takes a `rowScanner` (interface satisfied by both `*sql.Row` and `*sql.Rows`), handles all the NullX → Go conversions. Look at `scanCategory` for the template.
- **Input structs for ≥4-field constructors.** `CreateSessionInput` instead of nine positional args. Match the pattern in `go-eventkit`.
- **Typed string constants for enums.** `SessionStatus` is a distinct type from `string`; `.Valid()` method centralizes validation. Compiler catches typos at call sites.
- **`t.Helper()` in test helpers.** `newTestStore`, `sessionFixtures`, `captureFixtures` all use it so failure lines point to the calling test, not the helper.
- **Test isolation.** Every test gets a fresh `newTestStore(t)` using `t.TempDir()`. No shared state between tests.
- **No CHECK constraints in SQL when Go-layer validation exists.** Validation lives in the Go method (e.g. `SessionStatus.Valid()`). If we ever bypass the Go layer (raw SQL writes from another tool), we accept that no CHECK fires. This was an explicit trade-off; revisit if it bites.

## Logging changes

**Always append a LOG.md entry whenever code is changed in a session, before considering the work complete.** Even a one-line fix or a polish tweak gets a brief entry. The version bump rule and the LOG rule go together: if `make install` is needed, both `VERSION` *and* a LOG entry must be part of the change.

Entry length should match the change: a single short paragraph for small fixes, a fuller writeup for phase boundaries or design decisions. Use the existing dated-section format (`## YYYY-MM-DD — short title`).

PROJECT.md is different — only update it at phase boundaries or when architecture/scope shifts. Ask the user before editing PROJECT.md.

## What not to do

- **Don't change schema.sql without bumping a migration story.** Schema is currently idempotent via `IF NOT EXISTS`, but adding a column to an existing table requires a migration plan. Bring it up before doing it.
- **Don't move device_id to a config file.** It's deliberately in the DB so each machine has its own that travels with the data store.
- **Don't add View/UI features ahead of the session loop.** Phase 2 is the core session flow with stdin prompts. Bubble Tea is Phase 3. Reminders integration is Phase 4. Maintain that order — each phase de-risks the next.
- **Don't try to be a project manager.** Don't suggest reordering phases, adding milestones, or generating Gantt-style breakdowns unless asked. The user owns scheduling.
- **Don't skip LOG.md.** Every code-changing session gets at least a short entry — see "Logging changes" above. (Earlier guidance said the opposite; the rule has been updated because changes were slipping through unrecorded.)

## Verifying work

Standard checks before claiming a chunk is done:

```bash
go build ./...           # compiles
go vet ./...             # lint
go test ./...            # all tests pass
```

For changes touching the CLI entrypoint, also:

```bash
go run ./cmd/jacktasks   # actually runs
```

## When unsure

- **About scope:** ask. V1 has a defined list of features; if a request might expand it, surface that.
- **About style:** match the existing code. If there's no precedent, default to standard Go idioms (see Effective Go, the standard library).
- **About dependencies:** ask before adding any.
- **About the user's intent:** ask one clarifying question, then proceed.
