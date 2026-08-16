# Ejina Microgrid Dispatch Service

A Go backend that coordinates **grid-forming energy storage battery cabins**,
**wind/solar arrays**, and the **microgrid master controller** for the Ejina
Banner power dispatch centre. It unifies three concurrent operational modes:
daily inspection, external-grid-loss black start, and routine dispatch.

## Business Rules

| # | Rule | Implementation |
|---|------|----------------|
| 1 | Cabin SOC < 15% → alarm + priority charge | `battery.Fleet.Update` / `rebalanceLocked` |
| 2 | Black start & auto-reconnect concurrent → black start first, reconnect queued | `grid.Machine.BlackStart` / `RequestReconnect` |
| 3 | Anomaly order open > 4 h → auto-escalate to station chief | `workorder.Book.CheckEscalations` |
| 4 | Cabin offline → remaining cabins share load by remaining capacity | `battery.Fleet.SetOffline` / `rebalanceLocked` |
| 5 | Master/backup heartbeat loss → backup takes over within 10 s, replays last snapshot | `controller.Pair` + `app.App.CheckControllerHealth` |

During black start the master controller **locks the reconnect circuit** until
black start completes; a queued reconnect then proceeds automatically. On master
failure the backup controller replays the latest complete state snapshot so
supply is not interrupted.

## Architecture

```
main.go                      Entry point — loads config, starts HTTP server + background jobs
internal/
  config/                    JSON + env-var configuration
  domain/
    battery/                 Cabin model + fleet rules (Rules 1 & 4)
    workorder/               Anomaly work order lifecycle (Rule 3)
    grid/                    Grid state machine (Rule 2, reconnect lock)
    controller/              Master/backup heartbeat + failover (Rule 5)
  store/                     Atomic file-based JSON persistence
  app/                       Orchestration core — single mutation boundary, snapshots
  server/                    REST HTTP API (Go 1.26 method routing)
  job/                       Background tasks: escalation, health, snapshot, grid monitor
```

## Quick Start

Requires **Go 1.26** (matches the `golang:1.26-bookworm` Docker build stage).

```bash
# Build and run locally
go build -o ejina-microgrid .
./ejina-microgrid
```

The service listens on **:49495** by default.

## Main HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/health` | Health check |
| `POST` | `/api/cabins` | Register a battery cabin |
| `GET`  | `/api/cabins` | List all cabins |
| `PATCH`| `/api/cabins/{id}/sensors` | Update SOC/temperature/insulation |
| `POST` | `/api/cabins/{id}/offline` | Take cabin offline (Rule 4) |
| `POST` | `/api/cabins/{id}/online` | Bring cabin online |
| `POST` | `/api/cabins/{id}/inspect` | Inspect for anomalies |
| `POST` | `/api/orders` | Create anomaly work order |
| `GET`  | `/api/orders` | List work orders |
| `POST` | `/api/orders/{id}/accept` | Accept a work order |
| `POST` | `/api/orders/{id}/resolve` | Resolve a work order |
| `POST` | `/api/grid/lose` | Lose external grid → off-grid |
| `POST` | `/api/grid/blackstart` | Issue black start (Rule 2) |
| `POST` | `/api/grid/reconnect` | Request auto-reconnect |
| `POST` | `/api/grid/complete-blackstart` | Complete black start |
| `POST` | `/api/grid/complete-sync` | Complete grid sync |
| `GET`  | `/api/grid/state` | Current grid state |
| `POST` | `/api/controller/heartbeat` | Controller heartbeat |
| `POST` | `/api/controller/failover` | Trigger failover (Rule 5) |
| `GET`  | `/api/controller/state` | Controller pair state |
| `POST` | `/api/fleet/demand` | Set fleet power demand |
| `GET`  | `/api/snapshot` | Get full state snapshot |
| `POST` | `/api/snapshot/save` | Save snapshot to disk |

### Example: black start with queued reconnect

```bash
# 1. Lose external grid
curl -X POST http://localhost:49495/api/grid/lose

# 2. Black start (locks reconnect circuit)
curl -X POST http://localhost:49495/api/grid/blackstart -d '{"id":"bs-001"}'

# 3. Reconnect arrives during black start → queued
curl -X POST http://localhost:49495/api/grid/reconnect
# {"status":"queued"}

# 4. Complete black start → queued reconnect proceeds to syncing
curl -X POST http://localhost:49495/api/grid/complete-blackstart -d '{"id":"bs-001"}'

# 5. Complete sync
curl -X POST http://localhost:49495/api/grid/complete-sync
```

## Docker

```bash
# Build (multi-stage; build stage uses golang:1.26-bookworm)
docker build -t ejina-microgrid .

# Run
docker run -p 49495:49495 ejina-microgrid
```

For a specific architecture:

```bash
docker build --platform linux/arm64 -t ejina-microgrid:arm64 .
docker buildx build --platform linux/amd64,linux/arm64 -t ejina-microgrid:multi .
```

## Testing

```bash
go test -timeout=120s -count=1 ./...
```

Tests cover normal paths, error paths, state transitions, concurrency, and
failure recovery (controller failover with snapshot replay). No external
services are required.
