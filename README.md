# x-browser-mcp

`x-browser-mcp` is a browser-backed MCP server for reading X (Twitter) from a local logged-in session.

It exposes a small set of MCP tools and HTTP endpoints for:

- checking whether the local X session is ready
- starting an interactive login flow when needed
- reading the X home timeline
- searching recent X discussions

Instead of using the official X API, it reuses an isolated local browser profile and a background headless browser. This makes it useful for workflows where you want real-time X signals inside local coding agents such as Claude Code, Codex, or OpenClaw.

## What it does

- Reuses a local X login session from an isolated browser profile
- Runs a background headless browser for steady day-to-day use
- Switches to a visible browser window for manual login when the session is missing
- Returns summaries, representative posts, timestamps, and engagement metrics
- Exposes the same capability over MCP and plain HTTP

## Why this exists

The official X API is expensive for many personal workflows and often does not match the freshness of what you can see directly in the browser.

This project takes a different route:

- browser-backed instead of API-backed
- local session reuse instead of developer API credentials
- MCP-native so local agents can call it directly

## Tools

The MCP server exposes these tools:

- `check_login_status`
- `start_login`
- `read_home_timeline`
- `search_x`

## HTTP API

- `GET /health`
- `GET /api/v1/login/status`
- `POST /api/v1/login/start`
- `GET /api/v1/home?limit=10`
- `POST /api/v1/search`
- `POST /mcp`

## How login works

The service is designed around an isolated browser profile.

Normal operation:

- a dedicated headless browser stays alive in the background
- the MCP server attaches to that browser and reads X from the existing session

When login is required:

- the service detects that the session is not ready
- a visible browser window is opened on the same isolated profile
- after login is confirmed, the service switches back to the background headless browser

## Quick start

### 1. Build

```bash
go build -o x-browser-mcp .
```

### 2. Run the server

```bash
./x-browser-mcp -port :18110
```

### 3. Start login if needed

```bash
curl -X POST http://127.0.0.1:18110/api/v1/login/start
```

### 4. Check status

```bash
curl http://127.0.0.1:18110/api/v1/login/status
```

### 5. Search X

```bash
curl -X POST http://127.0.0.1:18110/api/v1/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"model context protocol","mode":"latest","limit":5}'
```

### 6. Read the home timeline

```bash
curl 'http://127.0.0.1:18110/api/v1/home?limit=10'
```

## Client integration

### Claude Code / Codex

Point your MCP client at:

```text
http://127.0.0.1:18110/mcp
```

### OpenClaw

OpenClaw can attach to this server through ACPX with `mcp-remote`.

Example runtime config:

```json
{
  "plugins": {
    "entries": {
      "acpx": {
        "enabled": true,
        "config": {
          "mcpServers": {
            "x-browser-mcp": {
              "command": "npx",
              "args": [
                "-y",
                "mcp-remote@latest",
                "http://127.0.0.1:18110/mcp"
              ]
            }
          }
        }
      }
    }
  }
}
```

## Operational notes

- This project is meant for local, personal use.
- It depends on a real browser session and local browser state.
- It uses conservative caching and rate limiting to avoid hammering X during normal use.
- It does not rely on the official X API.

## Repository scope

This repository contains the MCP server only.

It does not include:

- browser profile data
- local cookies
- launchd user agents
- personal OpenClaw or Claude configuration files
