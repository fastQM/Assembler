# Assembler Skill

## Purpose
Run Assembler as a local P2P runtime daemon and provide a stable local RPC endpoint for OpenClaw and app runtimes.

This skill is focused on:
- starting/stopping the Assembler daemon
- checking node/runtime status
- exposing local RPC for app communication
- collecting logs for troubleshooting

## Prerequisites
- Go 1.25.7+
- Local filesystem write access to `data/`
- No process currently binding the same Unix socket path

## Default Paths
- Data dir: `./data`
- RPC socket: `./data/assembler-p2p.sock`
- Node key: `./data/p2p_identity.key`
- Recent peers: `./data/recent_peers.json`
- RPC records: `./data/p2p_messages.jsonl`
- RPC cursors: `./data/p2p_cursors.json`
- PID file (ctl design): `./data/run/assembler.pid`
- Log file (ctl design): `./data/run/assembler.log`
- Example config: `./docs/assembler.example.json`

## Runtime Start
Use `assemblerctl` for daemon lifecycle management:

```bash
GO111MODULE=on go run ./cmd/assemblerctl start
GO111MODULE=on go run ./cmd/assemblerctl start --config ./data/assembler.json
```

## Runtime Stop

```bash
GO111MODULE=on go run ./cmd/assemblerctl stop
```

## Runtime Status

```bash
GO111MODULE=on go run ./cmd/assemblerctl status
GO111MODULE=on go run ./cmd/assemblerctl status --json
```

## RPC Status

```bash
GO111MODULE=on go run ./cmd/assemblerctl rpc status
```

## Logs

```bash
GO111MODULE=on go run ./cmd/assemblerctl logs --lines 200
GO111MODULE=on go run ./cmd/assemblerctl logs --follow=false
```

## Local RPC Contract
RPC service name: `P2P`

Available methods:
- `P2P.Publish`
- `P2P.Subscribe`
- `P2P.FetchHistory`
- `P2P.GetStatus`
- `P2P.SendDirect`

Realtime delivery uses stream socket `<rpc_socket>.stream` with JSON events.

## Health Check Workflow
1. Verify process is running.
2. Verify Unix socket exists.
3. Call `P2P.GetStatus`.
4. Confirm `transport=libp2p` and `peer_id` is non-empty.

## Security Baseline
- Keep RPC socket local-only (Unix socket).
- File permissions:
  - data directories: `0700` (recommended)
  - key files: `0600`
  - socket file: `0600`
- Never commit key material under `data/`.

## `assemblerctl` Command Set
- `start`
- `stop`
- `status`
- `logs`
- `rpc status`

For command behavior, flags, and exit codes, see `docs/assemblerctl-design.md`.

## Phase Progress
- Phase 1 (done): `assemblerctl` lifecycle wrapper and config-driven startup.
- Phase 2 (done): dedicated `cmd/assemblerd` daemon (RPC only, no HTTP).
- Phase 3 (done): richer RPC status metrics (peer/listen details, counters, subscriptions, started time).
