# Assembler

Assembler is a P2P-network-based application platform for building decentralized applications that can be assisted or controlled by agents.

This repository only keeps platform capabilities:

- Execution Layer: sandboxed service runtime
- Control Layer: install/start/stop/invoke/health lifecycle APIs
- Market Layer: publish/discover/subscribe app listings
- Network Layer: memory/libp2p pubsub transport

Game logic and app UIs are now hosted under `apps/` in this repository.

## Run (Daemon Mode Only)

Use `assemblerctl` as the only runtime entrypoint.
It starts `cmd/assemblerd` in background, manages PID/log files, and provides lifecycle commands.

## Daemon Control (`assemblerctl`)

```bash
cd Assembler
./assemblerctl start
./assemblerctl status
./assemblerctl rpc status
./assemblerctl logs --lines 200 --follow=false
./assemblerctl stop
```

Log file is managed by daemon mode (default: `data/run/assembler.log`).
If you run `cmd/assemblerd` directly in foreground for debugging, logs go to terminal instead of that file.

Config template: `docs/assembler.example.json` (copy to `data/assembler.json` and adjust paths if needed).
`assemblerctl start` now launches `cmd/assemblerd` (RPC-only daemon path).
Discovery is configurable:
- `p2p_mdns=true|false` (LAN discovery)
- `p2p_kad_dht=true|false` (DHT discovery)
- `p2p_kad_apps=["social", ...]` (app-level DHT discovery allowlist)

## Interface Mode

Assembler now runs in RPC-only mode in this repository.
The legacy HTTP gateway and web console have been removed.

## Apps Runtime (`apps/`)

Application gateway/runtime code is under `apps/` and runs as a same-process plugin host.
Current modules are mounted with namespaced API routes (for example `social`):

- plugin route: `/api/apps/social/v1/*`
- compatibility route: `/api/social/v1/*`

Run apps gateway:

```bash
cd Assembler/apps
go run ./cmd/apps-web -addr :8090 -social-rpc-sock ../data/assembler-p2p.sock
```

## Local RPC for app p2p access

Assembler also exposes a local UNIX-socket RPC server so apps can publish/subscribe
without owning their own libp2p host.

- Default socket: `data/assembler-p2p.sock`
- Enable flag: `-local-rpc-enable=true`
- Store files:
  - `-local-rpc-records data/p2p_messages.jsonl`
  - `-local-rpc-cursors data/p2p_cursors.json`
- Topic ACL: app `X` can only use topics under `app.X` or `app.X.*`

RPC service name is `P2P` (Go `net/rpc`):

- `P2P.Publish(PublishArgs) -> PublishReply`
- `P2P.Subscribe(SubscribeArgs) -> SubscribeReply`
- `P2P.Pull(PullArgs) -> PullReply` (long-poll style delivery)
- `P2P.Ack(AckArgs) -> AckReply`
- `P2P.FetchHistory(HistoryArgs) -> HistoryReply`
- `P2P.GetStatus(StatusArgs) -> StatusReply`

## Test

```bash
cd Assembler
go test ./...
```
