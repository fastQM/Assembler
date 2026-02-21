# lazylessctl Command Design

## Goals
- Run Lazyless in daemon mode with deterministic lifecycle.
- Provide one operator entrypoint for start/stop/status/logs.
- Keep Lazyless core as headless runtime (RPC-first).
- Keep local operations simple for OpenClaw skill integration.

## Non-goals
- Managing remote nodes.
- Exposing RPC over TCP.
- Multi-tenant auth in v1.

## Binary Layout
- Runtime daemon: `cmd/lazylessd` (RPC-only daemon, default launch target).
- Control CLI: `lazylessctl`.

Phase 1 implementation is available in `cmd/lazylessctl`.
Phase 2 implementation is available in `cmd/lazylessd`.

## Command Surface

Current implementation:
- `start`
- `stop`
- `status`
- `logs`
- `rpc status`

### `lazylessctl start`
Start daemon if not running.

Flags:
- `--config <path>` default `./data/lazyless.json`
- `--daemon` default `true`
- `--workdir <path>` default `.`
- `--daemon-bin <path>` optional, use prebuilt daemon binary
- `--server-bin <path>` deprecated alias of `--daemon-bin`
- `--log-file <path>` default `./data/run/lazyless.log`
- `--pid-file <path>` default `./data/run/lazyless.pid`
- `--wait <duration>` default `8s`

Behavior:
1. Ensure run dir exists.
2. If PID file exists and process alive, return already running.
3. Spawn daemon with configured args.
4. Write PID file.
5. Poll local RPC `P2P.GetStatus` until healthy or timeout.
6. Print node summary (pid, peer_id, socket).

Exit codes:
- `0` started
- `10` already running
- `11` start timeout
- `12` spawn failure

### `lazylessctl stop`
Stop daemon gracefully.

Flags:
- `--pid-file <path>` default `./data/run/lazyless.pid`
- `--timeout <duration>` default `10s`
- `--force` default `false`

Behavior:
1. Read pid file.
2. Send `SIGTERM`.
3. Wait until process exits.
4. If timeout and `--force`, send `SIGKILL`.
5. Remove stale PID file.

Exit codes:
- `0` stopped
- `20` not running
- `21` stop timeout

### `lazylessctl status`
Show runtime status.

Flags:
- `--pid-file <path>` default `./data/run/lazyless.pid`
- `--rpc-sock <path>` default `./data/lazyless-p2p.sock`
- `--json` default `false`

Output fields:
- process: `running|stopped|stale_pid`
- pid
- rpc_socket_exists
- transport
- peer_id
- connected_peers
- uptime (best effort)

Data source priority:
1. PID/process check
2. RPC `P2P.GetStatus`

Exit codes:
- `0` running and healthy
- `30` stopped
- `31` running but rpc unhealthy

### `lazylessctl logs`
Tail daemon log file.

Flags:
- `--log-file <path>` default `./data/run/lazyless.log`
- `--follow` default `true`
- `--lines <n>` default `200`

Behavior:
- Read last N lines.
- Follow mode tails appended logs.

### `lazylessctl rpc status`
Directly query local RPC and print node info.

Flags:
- `--rpc-sock <path>` default `./data/lazyless-p2p.sock`
- `--json` default `false`

Uses:
- `P2P.GetStatus`

## Config Model (v1)
Single JSON file at `./data/lazyless.json`:

```json
{
  "transport": "libp2p",
  "http_addr": ":8080",
  "p2p_listen": ["/ip4/0.0.0.0/tcp/0"],
  "p2p_bootstrap": [
    "/ip4/3.65.204.231/tcp/40001/p2p/12D3KooWAaYG182TYGF5GTfWu5CZpiWbf5r6GJwfuSsYRsErA5YL"
  ],
  "p2p_mdns": true,
  "p2p_rendezvous": "Lazyless",
  "p2p_identity_key": "./data/p2p_identity.key",
  "p2p_recent_peers": "./data/recent_peers.json",
  "local_rpc_enable": true,
  "local_rpc_sock": "./data/lazyless-p2p.sock",
  "local_rpc_records": "./data/p2p_messages.jsonl",
  "local_rpc_cursors": "./data/p2p_cursors.json",
  "run_pid_file": "./data/run/lazyless.pid",
  "run_log_file": "./data/run/lazyless.log"
}
```

Precedence:
1. CLI flags
2. Config file
3. Hardcoded defaults

## Daemonization Strategy
Preferred order:
1. Native daemon mode in Go process (detach + stdio redirection).
2. Fallback: shell background with PID capture.

Minimum runtime artifacts:
- PID file
- log file
- lock file (optional in v1)

## Failure Handling
- Startup health timeout: daemon process is killed, PID file removed.
- Stale PID file: ignored after process liveness check.
- RPC socket exists but dead: remove stale socket and restart.

## Security
- Unix socket permission `0600`.
- Key file permission `0600`.
- `data/` and `data/run/` recommended `0700`.
- No RPC TCP exposure in v1.

## OpenClaw Skill Integration
The skill should call only `lazylessctl`:
1. `lazylessctl start`
2. `lazylessctl status --json`
3. `lazylessctl rpc status --json`
4. `lazylessctl logs --lines 200`
5. `lazylessctl stop`

This keeps the integration stable even if internal daemon args evolve.

## Phased Delivery
- Phase 1: implemented `lazylessctl` lifecycle commands.
- Phase 2: implemented `cmd/lazylessd` (RPC-only daemon path).
- Phase 3: implemented richer `P2P.GetStatus` metrics:
  - listen addrs, connected peer ids/addrs
  - active subscriptions
  - message counters (published/network/stream/fanout/direct)
  - daemon started timestamp
