# Game Agent Adapter Protocol (v1)

This document defines a common REST contract for OpenClaw agent integration across game apps.

## Goals

- One control loop for all apps: observe -> decide -> act.
- Keep app-specific rules in app-side `spec.json`, not hard-coded in OpenClaw core.
- OpenClaw runtime input should be minimal (prefer only `player_id`).

## Required assets and endpoints

1. `GET <apps_base>/apps/<app>/spec.json`
- Returns app rules, action space, schema, and loop hints.
- Must include stable `spec_version`.
- Should support cache headers when served statically:
  - `ETag`
  - `Cache-Control`
  - client uses `If-None-Match`.

2. `GET /api/<app>/player/{player_id}`
- Used to resolve active room and current control mode.

3. `POST /api/<app>/room/{room_id}/control`
- Switches a seat between `human` and `agent`.

4. `GET /api/<app>/room/{room_id}/state`
- Returns current room metadata and latest per-player observable state.

5. `POST /api/<app>/room/{room_id}/input`
- Submits one action (`source=agent` for OpenClaw actions).

## Spec contract fields

- `required_runtime_params`: minimal params OpenClaw needs to start adapter.
- `session.resolve_room`: how to resolve `room_id` from `player_id`.
- `session.watch_control`: how to detect `agent` takeover state.
- `loop`: observation/action/control endpoints + timing hints.
- `actions`: allowed action set for output validation.

## Canonical control loop

1. User notifies OpenClaw to prepare takeover for `{app_id, player_id}`.
2. OpenClaw fetches and caches app `spec.json`.
3. OpenClaw polls `session.watch_control.endpoint` and checks `mode_field`.
4. While mode is not active (`agent`), only monitor.
5. When mode becomes active:
- resolve room via `session.resolve_room`
- fetch observation from `loop.observation_endpoint`
- prompt LLM with `rules + observation + actions`
- validate output action against `spec.actions`
- submit action to `loop.action_endpoint` as `source=agent`
- repeat at `loop.suggested_tick_ms`

## Tetris mapping

- `GET /apps/tetris-web/spec.json`
- `GET /api/tetris/player/{player_id}`
- `POST /api/tetris/room/{room_id}/control`
- `GET /api/tetris/room/{room_id}/state`
- `POST /api/tetris/room/{room_id}/input`

## Error handling requirements

- Invalid action -> HTTP 400.
- Wrong source for control mode -> HTTP 400.
- Unknown room/player -> HTTP 400/404.
- Agent should fallback to `noop` on LLM parsing errors.

## Security notes

- Server remains authoritative for room/control checks.
- Do not trust client-declared mode; verify in server state.
- Add per-player action rate limits in production.
