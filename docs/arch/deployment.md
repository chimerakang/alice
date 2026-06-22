# Deployment Architecture

This document covers the local runtime split, boot order, and repository layout that matter for day-to-day development.

## Runtime Topology

### Native Go process

The bot/API process runs on the host and owns:

- REST API
- WebSocket hub
- Claude/Codex CLI subprocess calls
- SQLite persistence

### Dashboard container

The dashboard runs in Docker and owns:

- nginx serving prebuilt React assets
- nginx proxying API/WebSocket traffic back to the host bot

```text
Telegram
  |
  v
Alice native Go process
  |
  v
Dashboard container
```

## Port Contract

These ports are part of the local deployment contract:

| Port | Owner | Notes |
| --- | --- | --- |
| `8082` | Native Alice bot/API | `web_port` must stay `8082`; nginx proxies to `host.docker.internal:8082`. |
| `3939` | Docker dashboard | User-facing dashboard URL is `http://localhost:3939`. |

Do not change these without updating nginx, Docker Compose, docs, and user-facing instructions together.

## Startup Sequence

### Start the bot

```bash
go build -o alice ./cmd/alice
nohup ./alice >> alice.log 2>&1 &
```

### Start the dashboard

```bash
docker compose up -d dashboard
```

### Rebuild after frontend changes

```bash
cd frontend
npm run build
cp -r dist/* ../web/
cd ..
docker compose up -d --build dashboard
```

## Project Structure

### Runtime and app code

```text
cmd/alice/main.go        Process entry point
internal/app/            Go application code
internal/app/hermes/     Hermes planner/executor support
internal/app/engine/     Plan/execute engines
```

### Protobuf and UI assets

```text
proto/                   Protobuf definitions
gen/                     Generated Go/TS protobuf bindings
frontend/                React source
web/                     Built dashboard assets served by nginx
```

### Support files and docs

```text
deploy/                  Deployment support files
docs/                    Project documentation
```

## Configuration Notes

- `config.json` contains runtime secrets and must not be edited by agents.
- `config.example.json` is the safe place to document config shape.
- Claude Code CLI auth is external to Alice; run `claude auth` before using the default Claude backend.
- The Codex/GPT tier uses `OPENAI_API_KEY` or `multimedia.openai_api_key` when `ai_backend` is `multi`.

## Detailed Reference

See [docs/DEPLOYMENT.md](../DEPLOYMENT.md) for broader production, monitoring, Kubernetes, and security notes.
