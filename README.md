# Assembler

Assembler is a P2P-network-based application platform for building decentralized applications that can be assisted or controlled by agents.

This repository only keeps platform capabilities:

- Execution Layer: sandboxed service runtime
- Control Layer: install/start/stop/invoke/health lifecycle APIs
- Market Layer: publish/discover/subscribe app listings
- Network Layer: memory/libp2p pubsub transport

Game logic and app UIs are hosted in `Assembler-Apps`.

## Run

```bash
cd Assembler
GO111MODULE=on go run ./cmd/assemblerd
```

## Run with libp2p

```bash
GO111MODULE=on go run ./cmd/assemblerd \
  -transport libp2p \
  -p2p-listen /ip4/0.0.0.0/tcp/40001
```

## Daemon Control (`assemblerctl`)

```bash
cd Assembler
GO111MODULE=on go run ./cmd/assemblerctl start
GO111MODULE=on go run ./cmd/assemblerctl status
GO111MODULE=on go run ./cmd/assemblerctl rpc status
GO111MODULE=on go run ./cmd/assemblerctl logs --lines 200 --follow=false
GO111MODULE=on go run ./cmd/assemblerctl stop
```

Config template: `docs/assembler.example.json` (copy to `data/assembler.json` and adjust paths if needed).
`assemblerctl start` now launches `cmd/assemblerd` (RPC-only daemon path).
Discovery is configurable:
- `p2p_mdns=true|false` (LAN discovery)
- `p2p_kad_dht=true|false` (DHT discovery)
- `p2p_kad_apps=["social", ...]` (app-level DHT discovery allowlist)

## Interface Mode

Assembler now runs in RPC-only mode in this repository.
The legacy HTTP gateway and web console have been removed.

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
GO111MODULE=on go test ./...
```
