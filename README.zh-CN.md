# x-browser-mcp

[English](./README.md) | [简体中文](./README.zh-CN.md)

`x-browser-mcp` 用来让本地 agent 基于真实登录的浏览器会话读取 X（Twitter）。

它是一个 browser-backed 的 MCP 服务，适合 Claude Code、Codex 以及其他支持 MCP 的本地 agent 在不接官方 X API 的情况下获取最新 X 内容。

## 它能做什么

- 检查本地 X 会话是否可用
- 在需要时启动交互式登录
- 读取 X 首页时间线
- 搜索最近的 X 讨论
- 同时提供 MCP 和 HTTP 两种调用方式

## 运行方式

`x-browser-mcp` 围绕一套独立浏览器 profile 工作。

- MCP/HTTP 服务可以常驻
- 只有真正需要访问 X 时才会启动浏览器
- 手动登录一定打开可见浏览器窗口
- 日常读取可以在同一套独立 profile 上用 headless 模式执行

这意味着你可以让服务一直在线，但不需要长期挂着一个浏览器进程。

## 为什么做这个

对很多个人工作流来说，官方 X API 成本高，而且新鲜度也未必和真实浏览器里看到的一样。

这个项目的思路是：

- 用浏览器，不用官方 API
- 复用本地登录态，不用开发者凭证
- 直接做成 MCP，方便本地 agent 调用

## MCP 工具

当前暴露的工具：

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

## 登录模型

登录流程很直接：

1. 调用 `start_login`
2. 服务在独立 profile 上打开一个可见浏览器窗口
3. 你在窗口里完成 X 登录
4. 后续请求继续复用同一套 profile

如果会话还有效，服务之后就可以继续直接使用，不需要重复登录。

## 快速开始

### 1. 编译

```bash
go build -o x-browser-mcp .
```

### 2. 启动服务

```bash
./x-browser-mcp -port :18110
```

### 3. 需要时触发登录

```bash
curl -X POST http://127.0.0.1:18110/api/v1/login/start
```

### 4. 检查登录状态

```bash
curl http://127.0.0.1:18110/api/v1/login/status
```

### 5. 搜索 X

```bash
curl -X POST http://127.0.0.1:18110/api/v1/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"model context protocol","mode":"latest","limit":5}'
```

### 6. 读取首页时间线

```bash
curl 'http://127.0.0.1:18110/api/v1/home?limit=10'
```

## 客户端接入

`x-browser-mcp` 可以接到支持 streamable HTTP MCP 的客户端上。

包括：

- Claude Code
- Codex
- OpenClaw 的 ACP/acpx 会话

### Claude Code / Codex

把 MCP 地址指向：

```text
http://127.0.0.1:18110/mcp
```

### OpenClaw

OpenClaw 可以通过 `mcp-remote` 挂这条 MCP 服务：

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

## 运行说明

- 这个项目面向本地个人使用
- 它依赖真实浏览器登录态和本地浏览器状态
- 它使用独立 profile，不直接碰你日常浏览器 profile
- 它不依赖官方 X API

## 仓库范围

这个仓库只包含 MCP 服务本身。

不包含：

- 浏览器 profile 数据
- 本地 cookies
- launchd user agent 配置
- 个人 Claude / Codex / OpenClaw 配置文件
