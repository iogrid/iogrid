# Phase 0 — bastion <-> Mac reverse tunnel setup

Operator-only walkthrough for the Phase 0 reverse-SSH tunnel that lets
bastion-side automation reach the founder's Mac during the internal
pilot.

> **Scope.** This is **NOT** part of the end-user iogrid provider
> install path. iogrid providers run `iogridd` only — they never need
> the tunnel. The tunnel exists so the bastion-side Claude Code agents
> can drive desktop work (vCard contact import, design review against
> the local Sketch/Figma files, etc.) without exposing the Mac's sshd
> to the public internet.

## Threat model

What we want:

- The bastion can reach the Mac's sshd, **but only via a tunnel the Mac
  initiates** (so the Mac's sshd is never listening on a public IP).
- The tunnel survives Mac sleep/wake and network changes (coffee shop
  Wi-Fi -> home Wi-Fi -> tethered hotspot) without manual intervention.
- The tunnel survives bastion reboots (autossh respawns after the next
  network blip).
- The Mac authenticates to the bastion with a **pinned**, single-purpose
  ed25519 key — never the operator's primary `~/.ssh/id_ed25519`.
- The Mac authenticates the bastion's host key against a **pinned**
  `known_hosts` — no TOFU.

What we accept:

- Operators with shell on the bastion can use the tunnel. The bastion
  is single-tenant (founder + the Claude Code agents the founder
  authorizes), so this is fine for Phase 0. Phase 1 hardens this with
  a forced-command + audit log on the bastion side.

## Architecture

```
                   ssh -R 2223:localhost:22 openova@144.91.121.182
   +-----------+   ============================================>   +-------------+
   | Mac (you) |   autossh keeps the tunnel up across network      |   bastion   |
   |           |   transitions, sleep/wake, etc.                   |  (Contabo)  |
   +-----------+                                                   +-------------+
        ^                                                                 |
        |        ssh -p 2223 -i ~/.ssh/claude_offload emrah@localhost     |
        +-----------------------------------------------------------------+
                 Bastion-side automation hops into the Mac via the
                 reverse-forwarded port 2223.
```

`autossh` runs under a launchd **LaunchAgent** in the operator's GUI
session (`gui/<uid>/org.iogrid.tunnel`). That means:

- It starts at user login (NOT system boot — we never want the tunnel
  up if the operator isn't logged in).
- It runs with the operator's permissions only — no `sudo`.
- launchd respawns autossh if it ever exits, with a 15s throttle so a
  network flap doesn't hammer the bastion.

## One-time setup

### 1. Install autossh

```bash
brew install autossh
```

### 2. Generate the pinned keypair

```bash
mkdir -p ~/.iogrid
chmod 700 ~/.iogrid
ssh-keygen -t ed25519 -N '' -C "iogrid-tunnel-$(hostname)" -f ~/.iogrid/id_ed25519
```

We deliberately use a **separate** key (`~/.iogrid/id_ed25519`) from
the operator's primary SSH identity. Combined with
`IdentitiesOnly=yes` in the plist this means even if `ssh-agent` is
holding other keys, only this key is ever offered to the bastion.

### 3. Authorise the key on the bastion

From the Mac:

```bash
ssh-copy-id -i ~/.iogrid/id_ed25519.pub openova@144.91.121.182
```

On the bastion, the operator's `~openova/.ssh/authorized_keys` line for
this key SHOULD eventually be wrapped in a forced-command + restrictive
options block (Phase 1 hardening); for Phase 0 the bastion is
single-tenant so the standard line is acceptable.

### 4. Pin the bastion's host key

```bash
ssh-keyscan -t ed25519 144.91.121.182 > ~/.iogrid/known_hosts
chmod 600 ~/.iogrid/known_hosts
```

Verify the fingerprint out-of-band against the founder's record before
trusting it:

```bash
ssh-keygen -lf ~/.iogrid/known_hosts
```

With `StrictHostKeyChecking=yes` + `UserKnownHostsFile=~/.iogrid/known_hosts`
in the plist, any future bastion host-key change will block the tunnel
with a loud `host key verification failed` error rather than silently
accepting a man-in-the-middle.

### 5. Install the LaunchAgent

From the repo root on the Mac:

```bash
./installer/macos/install-tunnel.sh
```

Override defaults with env vars if you're testing against a non-prod
bastion:

```bash
BASTION_HOST=staging.example.com \
BASTION_USER=openova \
REMOTE_PORT=2223 \
./installer/macos/install-tunnel.sh
```

The script:

1. Verifies `autossh`, the key, and the pinned `known_hosts` exist.
2. Renders `installer/macos/io.iogrid.tunnel.plist` (substituting your
   `$HOME`, the bastion address, the remote port, and the autossh
   binary path) into `~/Library/LaunchAgents/org.iogrid.tunnel.plist`.
3. `launchctl bootout` + `bootstrap` + `enable` + `kickstart` the
   agent under your GUI session.
4. Verifies the agent is loaded.

## Verifying the tunnel works

On the Mac:

```bash
launchctl list | grep org.iogrid.tunnel
# Expect:  <pid>  0  org.iogrid.tunnel

pgrep -fl autossh
# Expect a line like:
#   12345 /opt/homebrew/bin/autossh -M 0 -N -i /Users/<you>/.iogrid/id_ed25519 ...

tail -n 20 ~/.iogrid/autossh.err
# Expect mostly empty (autossh is silent on success).
```

On the bastion:

```bash
ss -lntp | grep ':2223'
# Expect a sshd listener bound on 127.0.0.1:2223 (the reverse forward)

ssh -p 2223 -i ~/.ssh/<your-key> <macuser>@localhost 'hostname; uptime'
# Expect the Mac's hostname + uptime.
```

## Failure modes & recovery

| Symptom | Diagnosis | Recovery |
|---|---|---|
| `launchctl list` shows the agent but `pgrep autossh` empty | autossh crashed and is in ThrottleInterval (15s) backoff | wait 15s, re-check; tail `~/.iogrid/autossh.err` for the cause |
| `ssh: connect to host 144.91.121.182 port 22: Network is unreachable` in err log | Mac is offline | autossh will respawn when network returns — no action needed |
| `Host key verification failed` in err log | bastion host key changed | verify the new key out-of-band, refresh `~/.iogrid/known_hosts` |
| `Warning: remote port forwarding failed for listen port 2223` | stale tunnel still bound on the bastion | on the bastion: `pkill -f 'sshd:.*\[priv\]'` on the stale session, then `launchctl kickstart -k gui/$(id -u)/org.iogrid.tunnel` on the Mac |
| `Permission denied (publickey)` | bastion-side `authorized_keys` doesn't have the pinned key | re-run step 3 above |

## Uninstall

```bash
launchctl bootout "gui/$(id -u)/org.iogrid.tunnel" 2>/dev/null || true
rm -f ~/Library/LaunchAgents/org.iogrid.tunnel.plist
```

This stops the tunnel and removes the launchd registration. The pinned
key and `known_hosts` under `~/.iogrid/` are left in place so a future
reinstall (`./installer/macos/install-tunnel.sh`) is a single command.

## Phase 1 evolution

For Phase 1 (external operators, multi-tenant bastion), this design
needs:

- A bastion-side forced-command + restrictive `authorized_keys` options
  block (`no-pty,no-X11-forwarding,permitlisten="2223",...`).
- Per-operator port allocation (currently we hardcode 2223; needs a
  registry once we have more than one operator's Mac on the bastion).
- A bastion-side watchdog at `/home/openova/bin/iogrid-tunnel-watchdog.sh`
  that logs every state transition (mentioned in #82) — tracked as a
  follow-up infra task.

See `docs/ROADMAP.md` Phase 1 for the operator-fleet plans.
