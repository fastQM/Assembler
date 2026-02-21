# ClawdCity

ClawdCity is the core service layer for OpenClaw-style decentralized systems.

This repository only keeps platform capabilities:

- Execution Layer: sandboxed service runtime
- Control Layer: install/start/stop/invoke/health lifecycle APIs
- Market Layer: publish/discover/subscribe app listings
- Network Layer: memory/libp2p pubsub transport

Game logic and app UIs are hosted in `ClawdCity-Apps`.

## Run

```bash
cd ClawdCity
GO111MODULE=on go run ./cmd/server -addr :8080
```

## Run with libp2p

```bash
GO111MODULE=on go run ./cmd/server \
  -addr :8080 \
  -transport libp2p \
  -p2p-listen /ip4/0.0.0.0/tcp/40001
```

## API quick reference

- `GET /api/health`
- `GET /api/clawdcity/market/apps?kind=&tag=`
- `POST /api/clawdcity/market/apps`
- `GET /api/clawdcity/market/stream` (SSE)
- `GET /api/clawdcity/control/installed`
- `POST /api/clawdcity/control/install`
- `POST /api/clawdcity/control/apps/{app_id}/start`
- `POST /api/clawdcity/control/apps/{app_id}/stop`
- `GET /api/clawdcity/control/apps/{app_id}/health`
- `POST /api/clawdcity/control/apps/{app_id}/invoke`
- `GET /api/clawdcity/node`

## Local RPC for app p2p access

ClawdCity also exposes a local UNIX-socket RPC server so apps can publish/subscribe
without owning their own libp2p host.

- Default socket: `data/clawdcity-p2p.sock`
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
cd ClawdCity
GO111MODULE=on go test ./...
```
