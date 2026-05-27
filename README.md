# jacktasks

A personal CLI/TUI task tracker built around a modified Pomodoro flow, designed
for ADHD-friendly capture-and-defer workflows, with cross-device sync via a
self-hosted backend.

## What it is

jacktasks tracks time spent on **projects** and their **categories** of work,
encouraging structured work blocks with optional breaks. It's built around three
real problems:

1. **Forgetting tasks mid-session.** The `upn` command captures a stray thought
   and defers it to session end, so you don't break flow to act on it.
2. **Capturing on phone, acting on a laptop.** Apple Reminders is the phone-side
   capture surface; jacktasks pulls from a dedicated `jacktasks-inbox` list when
   you start a session on a Mac.
3. **Improvisational project work.** Projects can be pre-populated or added
   ad-hoc at session start.

It's two binaries:

- **`jacktasks`** — the Bubble Tea TUI you run on each Mac. Stores data in a
  local SQLite file.
- **`jacktasks-sync`** — a small HTTP service running on a home server that holds
  the master SQLite store and reconciles data between machines (behind
  Tailscale).

Sync between Macs is handled by `jacktasks-sync`; the Reminders inbox syncs to
your phone separately via iCloud.

See `PROJECT.md` for the full architecture, schema, session state machine, and
sync protocol.

## Requirements

- Go 1.24+
- macOS for the TUI client (uses Apple EventKit for Reminders integration)
- A Linux/Ubuntu host + Tailscale for the optional sync server

## Install (the TUI client)

```bash
make install
```

This builds and installs `jacktasks` to `~/.local/bin` (no sudo). Make sure that
directory is on your `PATH` — add this to `~/.zshrc` once:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

To install elsewhere, override `PREFIX`:

```bash
make install PREFIX=/usr/local/bin
```

To run from source without installing:

```bash
go run ./cmd/jacktasks
```

## Using it

Launch the TUI:

```bash
jacktasks
```

You'll land on the start screen (logo + menu), which offers any inbox items
pulled from Apple Reminders, a resume option for an unfinished session, and a way
to start a new one.

**Starting a session** walks through: pick/create a project → pick/create a
category → choose a duration. The timer then runs.

**During an active (or paused) session:**

| Command      | What it does                                                        |
|--------------|---------------------------------------------------------------------|
| `upn <text>` | Capture a thought, deferred to session end. Doesn't interrupt flow. |
| `ext <n>`    | Extend the target end time by `n` minutes.                          |
| `pause`      | Pause the timer.                                                    |
| `resume`     | Resume from paused.                                                 |
| `end`        | End early; routes to the end-notes screen, then saves the session.  |
| `cancel`     | Abandon the session with no DB record and no resume eligibility.    |

When the planned time is reached the session auto-ends. The **What-Next** screen
then shows the captures from the just-ended session, where each can be cleared,
sent to Apple Reminders, or turned into its own new session (`Do`). From there
you can continue with the same settings, start fresh, take a 5-minute break, or
quit.

List screens support arrow keys, `j`/`k`, and numeric shortcuts.

### Dailies & Weeklies

Any category can carry an optional **recurring target** — a habit goal you want
to hit on a daily or weekly cadence. Targets drive two things: a progress HUD
while you work, and a streak count that rewards consistency.

**Setting a target.** On the category-selection screen, highlight a category and
press `t` to open a compact target editor. The target is written in a short
syntax:

| Input          | Meaning                                                        |
|----------------|----------------------------------------------------------------|
| `30/day`       | 30 minutes every day.                                          |
| `3x/day`       | 3 sessions of any length every day.                           |
| `/day`         | Presence-only — any time logged that day counts (no minute goal). |
| `30/day MTWTF` | 30 minutes on weekdays only; weekends are off-days.            |
| `3x/day MTWTF` | 3 sessions on weekdays only.                                   |
| `30/week`      | 30 minutes over the calendar week (Monday–Sunday).             |
| `2x/week`      | 2 sessions over the calendar week.                            |
| `/week`        | Presence-only, once per week.                                  |
| `none`         | Clear the target.                                              |

A bare number is a **minute** goal; a number with an `x` suffix is a
**session-count** goal (sessions of any length, counted by how many you log —
cancelled sessions don't count). The two are mutually exclusive. The weekday
letters are positional `MTWTFSS` (Mon–Sun), so `MTWTF` is weekdays and `SS` is
the weekend. Categories with a target show a dim annotation in the list, e.g.
`(30 min/day, weekdays)` or `(3 sessions/day)`.

**Progress HUD.** While a session is active (or paused, or on the What-Next
screen), a line shows your progress against the current period plus the streak:

```
Keybr: 12/30 min today · 🔥 4-day streak
```

The **start screen** also shows progress for *every* targeted category at once,
in a "Dailies / Weeklies" panel beside the inbox/menu (it stacks below on narrow
terminals):

```
Inbox                  Dailies / Weeklies
1) Reply to Sam        Keybr: 12/30 min today · 🔥 4d
n) New session         Standup: 2/3 today · 🔥 6d
q) Quit                Exercise: 0/45 min this week
```

(Session-count targets read `2/3 today`; minute targets read `12/30 min today`.)

**Streaks.** The streak counts consecutive periods where the target was met.
For dailies, scheduled off-days (e.g. weekends on an `MTWTF` target) are skipped
without breaking the run. The current in-progress period never breaks a
streak — it only extends it once met. Weekly streaks count ISO Monday–Sunday
weeks.

### Configuration

Optional per-device settings live in `~/.config/jacktasks/config.toml`. The file
is optional — defaults apply when it's missing. A malformed file (or an invalid
timezone) is a hard error: the app prints the problem and exits.

```toml
# Sessions you aim to complete each day. Shows "Sessions today: N/M" on the
# start and What-Next screens. Omit or set 0 for no target.
daily_session_target = 6

# IANA timezone used to bucket sessions into days/weeks (Dailies/Weeklies) and
# to display times. Omit to use the machine's local timezone. Sessions are
# always stored in UTC epoch seconds — this only affects display and the
# day/week boundaries for streaks.
timezone = "America/Denver"
```

The sync server has its own equivalent for its web view — set
`JACKTASKS_SYNC_TZ` (e.g. `America/Denver`) in its env file; see
[Deployment](#deployment-sync-server).

### Sync

If a sync server is configured (see below), data syncs automatically in the
background on startup and after each session save. You can also sync manually:

- the `s) Sync now` option on the start screen, or
- the CLI: `jacktasks sync`

Both require these to be set in your shell:

```bash
export JACKTASKS_SYNC_URL=http://<server-tailscale-ip>:8484
export JACKTASKS_SYNC_TOKEN=<shared token>
```

### Where data lives

```
~/Library/Application Support/jacktasks/jacktasks.db     # local SQLite store
~/Library/Application Support/jacktasks/active.json      # crash-recovery sentinel
```

The DB is plain SQLite — inspect it with `sqlite3` or DBeaver:

```bash
sqlite3 ~/Library/Application\ Support/jacktasks/jacktasks.db ".tables"
```

## Development

```bash
make check     # build + vet + test — run this before committing
make build     # compile all binaries
make test      # run the test suite
make vet       # go vet
```

## Deployment (sync server)

The `jacktasks-sync` server runs on a home Linux box (e.g. an Ubuntu host),
reachable over Tailscale. Full step-by-step instructions — including the
cross-Mac convergence check — live in `deploy/DEPLOY.md`. The short version:

**1. Cross-compile and ship the binary (from a Mac):**

```bash
make build-sync-linux
scp jacktasks-sync-linux <server>:/tmp/jacktasks-sync
```

**2. First-time setup on the server:**

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin jacktasks
sudo mv /tmp/jacktasks-sync /usr/local/bin/jacktasks-sync
sudo chmod 755 /usr/local/bin/jacktasks-sync
sudo mkdir -p /var/lib/jacktasks-sync && sudo chown jacktasks:jacktasks /var/lib/jacktasks-sync
sudo mkdir -p /etc/jacktasks-sync
# copy deploy/env.template → /etc/jacktasks-sync/env, fill in the token + Tailscale IP
sudo cp deploy/jacktasks-sync.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now jacktasks-sync
curl http://<tailscale-ip>:8484/healthz    # → {"ok":true}
```

**3. On each Mac**, export `JACKTASKS_SYNC_URL` and `JACKTASKS_SYNC_TOKEN` (see
[Sync](#sync) above), then run `jacktasks sync`.

The server also serves a read-only, day-grouped browse view of logged sessions at
its root path (`http://<tailscale-ip>:8484/`). By default it renders times in the
server's local timezone (often UTC); set `JACKTASKS_SYNC_TZ` in the env file
(e.g. `America/Denver`) to render in your timezone instead.

To deploy a new server version, repeat step 1, then replace the binary and
restart the service (`chmod 755` again after replacing) — see `deploy/DEPLOY.md`.
