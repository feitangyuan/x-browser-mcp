# x-browser-mcp

[English](./README.md) | [简体中文](./README.zh-CN.md)

`x-browser-mcp` lets local agents read X (Twitter) from a real logged-in browser session.

It is a browser-backed MCP server for workflows where Claude Code, Codex, and other MCP-capable local agents need fresh X signals without paying for the official X API.

## What It Does

- Checks whether the local X session is ready
- Starts an interactive login flow when needed
- Reads the X home timeline
- Searches recent X discussions
- Exposes the same capability over MCP and plain HTTP

## How It Works

`x-browser-mcp` is built around an isolated browser profile.

- The MCP/HTTP service can stay running in the background
- Browsers are launched only when a request actually needs X access
- Manual login always opens a visible browser window
- Normal reads can run in headless mode against the same isolated profile

This keeps the service available to local agents without forcing a permanent browser process to stay alive.

## Why This Exists

The official X API is expensive for many personal workflows and often less aligned with what you can see directly in a real browser session.

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

## Login Model

The login flow is intentionally simple:

1. Call `start_login`
2. A visible browser window opens on the isolated profile
3. Complete X login in that window
4. Later requests reuse the same profile

If the session is already valid, the service can keep using the stored profile without asking for login again.

## Quick Start

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

## Client Integration

`x-browser-mcp` works with MCP clients that can talk to a streamable HTTP MCP endpoint.

That includes:

- Claude Code
- Codex
- OpenClaw ACP/acpx-backed sessions

### Claude Code / Codex

Point your MCP client at:

```text
http://127.0.0.1:18110/mcp
```

### OpenClaw

OpenClaw can attach to this server through ACPX with `mcp-remote`.

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

## Operational Notes

- This project is meant for local, personal use
- It depends on a real browser session and local browser state
- It uses an isolated browser profile instead of your normal daily browser profile
- It does not rely on the official X API

## Repository Scope

This repository contains the MCP server only.

It does not include:

- browser profile data
- local cookies
- launchd user agents
- personal Claude / Codex / OpenClaw config files
