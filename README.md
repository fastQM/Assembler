# ClawdCity

Go + Web MVP for decentralized-game architecture on top of OpenClaw concepts.

## Repository split

- Service layer (this repo): `ClawdCity`
- Application layer (new repo): `ClawdCity-Apps`

`ClawdCity` keeps networking/runtime/control/market APIs and room coordination.
Game UI/app packages are now moved to `ClawdCity-Apps`.

## What is implemented

- Transport/core layer: `internal/core/network` (in-memory pubsub)
- Generic runtime layer: `internal/runtime` (session engine + adapter interface)
- Game plugin layer: `internal/games/poker` (Texas Hold'em MVP with commit-reveal)
- Web API: `internal/api`
- Web UI: `web/index.html`

This code intentionally separates communication/runtime from game rules so more games can be added as adapters later.

## Run

```bash
cd ClawdCity
GO111MODULE=on go run ./cmd/server -addr :8080
```

Open `http://localhost:8080`.

### Run with libp2p transport

Node A:

```bash
GO111MODULE=on go run ./cmd/server \
  -addr :8080 \
  -transport libp2p \
  -p2p-listen /ip4/0.0.0.0/tcp/4001
```

Node B (bootstrap to A):

```bash
GO111MODULE=on go run ./cmd/server \
  -addr :8080 \
  -transport libp2p \
  -p2p-listen /ip4/0.0.0.0/tcp/4001 \
  -p2p-bootstrap /ip4/<IP-1>/tcp/4001/p2p/<PEER_ID_OF_A>
```

Flags:

- `-transport memory|libp2p` (default `memory`)
- `-p2p-listen` comma-separated multiaddrs
- `-p2p-bootstrap` comma-separated `/p2p/` multiaddrs
- `-p2p-rendezvous` mDNS service tag
- `-p2p-mdns` enable/disable mDNS discovery

## Test

```bash
cd ClawdCity
GO111MODULE=on go test ./...
```

## Smoke test (cross-platform)

After server is running:

```bash
cd ClawdCity
GO111MODULE=on go run ./cmd/smoke -base http://127.0.0.1:8080
```

## API quick reference

- `GET /api/health`
- `POST /api/hash` (`{"seed":"..."}`)
- `GET /api/sessions`
- `POST /api/sessions`
- `POST /api/sessions/{id}/actions`
- `GET /api/sessions/{id}/view?player_id=alice`
- `GET /api/sessions/{id}/events`
- `GET /api/sessions/{id}/stream` (SSE)

## ClawdCity (Execution / Control / Market)

New APIs:

- `GET /api/clawdcity/market/apps?kind=&tag=`
- `POST /api/clawdcity/market/apps`
- `GET /api/clawdcity/market/stream` (SSE)
- `GET /api/clawdcity/control/installed`
- `POST /api/clawdcity/control/install` (`{"app_id":"counter-game"}`)
- `POST /api/clawdcity/control/apps/{app_id}/start`
- `POST /api/clawdcity/control/apps/{app_id}/stop`
- `GET /api/clawdcity/control/apps/{app_id}/health`
- `POST /api/clawdcity/control/apps/{app_id}/invoke`

Quick run-through:

```bash
curl -s http://127.0.0.1:8080/api/clawdcity/market/apps | jq
curl -s -X POST http://127.0.0.1:8080/api/clawdcity/control/install -H 'content-type: application/json' -d '{"app_id":"counter-game"}'
curl -s -X POST http://127.0.0.1:8080/api/clawdcity/control/apps/counter-game/start
curl -s -X POST http://127.0.0.1:8080/api/clawdcity/control/apps/counter-game/invoke -H 'content-type: application/json' -d '{"method":"inc","params":{"player":"alice"}}'
curl -s http://127.0.0.1:8080/api/clawdcity/control/apps/counter-game/health | jq
```

## Tetris Realtime Room APIs

- `POST /api/tetris/register` (`player_id`, `app_id`, `version`)
- `POST /api/tetris/ready` (`player_id`, `ping_ms`)
- `GET /api/tetris/player/{player_id}`
- `GET /api/tetris/room/{room_id}`
- `GET /api/tetris/room/{room_id}/stream` (SSE)
- `POST /api/tetris/room/{room_id}/control` (`player_id`, `to_mode=human|agent`, `agent_id`)
- `POST /api/tetris/room/{room_id}/input` (`player_id`, `source=human|agent`, `action`, `payload`, `tick`)

## Multi-machine test plan

See `TEST_PLAN_2026-02-16.md`.

## Tetris web app (moved)

Start app-layer server:

```bash
cd ClawdCity-Apps
GO111MODULE=on go run ./cmd/apps-web -addr :8090
```

Open:

- `http://127.0.0.1:8090/apps/tetris-web/web/tetris.html?apiBase=http://127.0.0.1:8080`

`http://<host>:8080/tetris.html` now shows migration instructions.

## Smart contract draft

- Contract file: `contracts/OpenClawGamePoints.sol`
- Notes: `contracts/README.md`

Design intent:

1. Daily faucet claim (e.g. 1000 points / 24h).
2. Non-transferable and non-redeemable points.
3. Operator-driven on-chain settlement (`settleBatch`) for off-chain game results.

## Next steps (recommended)

1. Replace in-memory pubsub with libp2p transport.
2. Add wallet-bound auth and signed actions.
3. Add dispute-friendly hand transcript hashing.
4. Expand poker evaluator (full hand ranking + side pots).
