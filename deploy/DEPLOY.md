# Deploying jacktasks-sync to the ThinkCentre

These steps assume:
- The ThinkCentre runs Ubuntu and is reachable via Tailscale.
- You have SSH access as a user with `sudo`.
- Tailscale is installed and connected on the ThinkCentre.

---

## 1. Build the server binary (run on MacBook)

```bash
make build-sync-linux
# produces: jacktasks-sync-linux in the repo root
```

## 2. Copy the binary and config files to the ThinkCentre

Run from the repo root on your MacBook:

```bash
scp jacktasks-sync-linux <thinkcentre>:/tmp/jacktasks-sync
scp deploy/env.template <thinkcentre>:/tmp/jacktasks-sync.env
scp deploy/jacktasks-sync.service <thinkcentre>:/tmp/jacktasks-sync.service
```

Replace `<thinkcentre>` with your Tailscale hostname or IP.

## 3. First-time server setup (run on ThinkCentre via SSH)

```bash
# Create a dedicated service user (no login shell, no home dir)
sudo useradd --system --no-create-home --shell /usr/sbin/nologin jacktasks

# Install the binary
sudo mv /tmp/jacktasks-sync /usr/local/bin/jacktasks-sync
sudo chmod 755 /usr/local/bin/jacktasks-sync

# Create the data directory
sudo mkdir -p /var/lib/jacktasks-sync
sudo chown jacktasks:jacktasks /var/lib/jacktasks-sync

# Create the config directory and env file
sudo mkdir -p /etc/jacktasks-sync
sudo mv /tmp/jacktasks-sync.env /etc/jacktasks-sync/env
sudo chmod 640 /etc/jacktasks-sync/env
sudo chown root:jacktasks /etc/jacktasks-sync/env
```

Edit `/etc/jacktasks-sync/env` and fill in the three values:

```bash
sudo nano /etc/jacktasks-sync/env
```

- `JACKTASKS_SYNC_TOKEN` — generate with `openssl rand -hex 32`
- `JACKTASKS_SYNC_DB` — leave as `/var/lib/jacktasks-sync/master.db`
- `JACKTASKS_SYNC_ADDR` — `<tailscale-ip>:8484` (run `tailscale ip -4` separately to get the IP; **do not** put `$(...)` in the env file — systemd does not expand shell substitutions in `EnvironmentFile`)

## 4. Install and start the systemd unit

```bash
sudo mv /tmp/jacktasks-sync.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now jacktasks-sync
sudo systemctl status jacktasks-sync
```

Verify it's listening:

```bash
curl http://$(tailscale ip -4):8484/healthz
# expected: {"ok":true}
```

## 5. On each Mac — configure the client

Add to your shell profile (`~/.zshrc` or equivalent):

```bash
export JACKTASKS_SYNC_URL=http://<thinkcentre-tailscale-ip>:8484
export JACKTASKS_SYNC_TOKEN=<same token as in /etc/jacktasks-sync/env>
```

Reload: `source ~/.zshrc`

## 6. First sync from MacBook

```bash
jacktasks sync
```

Expected output (row counts will vary):

```
projects:    pushed 3, pulled 0
categories:  pushed 5, pulled 0
sessions:    pushed 12, pulled 0
captures:    pushed 8, pulled 0
```

Verify the master DB has the rows:

```bash
ssh <thinkcentre> sqlite3 /var/lib/jacktasks-sync/master.db "SELECT COUNT(*) FROM sessions;"
```

## 7. First sync from Mac Mini

On the Mac Mini, run `jacktasks` once to bootstrap the local DB (generates device_id, applies schema). Then:

```bash
jacktasks sync
```

Expected:

```
projects:    pushed 0, pulled 3
categories:  pushed 0, pulled 5
sessions:    pushed 0, pulled 12
captures:    pushed 0, pulled 8
```

Verify the Mac Mini has the MacBook's projects: run `jacktasks` and confirm they appear on the project-selection screen.

## 8. Cross-device convergence check

1. Run a session on the Mac Mini, end it.
2. `jacktasks sync` on Mac Mini (pushes the new session).
3. `jacktasks sync` on MacBook (pulls the Mac Mini session).
4. Run a session on MacBook, end it.
5. `jacktasks sync` on MacBook (pushes).
6. `jacktasks sync` on Mac Mini (pulls).
7. Verify both machines see each other's sessions in the DB:

```bash
sqlite3 ~/Library/Application\ Support/jacktasks/jacktasks.db \
  "SELECT device_id, COUNT(*) FROM sessions GROUP BY device_id;"
```

Both `mac-a` and `mac-b` device_ids should appear on each machine.

---

## Updating the server binary

```bash
# On MacBook
make build-sync-linux
scp jacktasks-sync-linux <thinkcentre>:/tmp/jacktasks-sync

# On ThinkCentre
sudo systemctl stop jacktasks-sync
sudo mv /tmp/jacktasks-sync /usr/local/bin/jacktasks-sync
sudo chmod 755 /usr/local/bin/jacktasks-sync
sudo systemctl start jacktasks-sync
sudo systemctl status jacktasks-sync
```

## Logs

```bash
sudo journalctl -u jacktasks-sync -f
```
