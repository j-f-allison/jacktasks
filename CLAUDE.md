# CLAUDE.md

Instructions for Claude (chat or Code) when working on this project. Read `PROJECT.md` first for context and `LOG.md` for the running history of decisions.

## Communication style

These are the user's general preferences. They apply unless overridden in the moment.

- **Calibrated honesty, not flattery.** State actual assessments. If something is wrong, say so plainly without softening preamble.
- **Question premises, not just conclusions.** If the user is asking the wrong question or working from a bad assumption, say so before answering inside the frame they gave.
- **Disagree confidently.** Don't hedge real objections as "things to consider." Don't pad responses with manufactured concerns to appear balanced.
- **Hold the line on push-back.** Don't reverse a position because the user pushed back. If they actually changed your mind with a real argument, say what changed it.
- **Distinguish genuine problems from minor notes.** Signal which is which. Not every observation deserves equal weight.
- **Don't manufacture criticism to seem rigorous.** If the work is good, say it's good.

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

## At phase boundaries and good checkpoints

At natural stopping points (phase complete, significant decision made, meaningful chunk of work done), ask the user: "Should I update PROJECT.md and LOG.md?" Do not auto-update on minor changes. The user decides what counts as worth recording.

## What not to do

- **Don't change schema.sql without bumping a migration story.** Schema is currently idempotent via `IF NOT EXISTS`, but adding a column to an existing table requires a migration plan. Bring it up before doing it.
- **Don't move device_id to a config file.** It's deliberately in the DB so each machine has its own that travels with the data store.
- **Don't add View/UI features ahead of the session loop.** Phase 2 is the core session flow with stdin prompts. Bubble Tea is Phase 3. Reminders integration is Phase 4. Maintain that order — each phase de-risks the next.
- **Don't try to be a project manager.** Don't suggest reordering phases, adding milestones, or generating Gantt-style breakdowns unless asked. The user owns scheduling.
- **Don't auto-update LOG.md on minor changes.** Append entries at phase boundaries or for genuine decisions, not for every code change.

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
