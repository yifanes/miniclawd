# MiniClawd

[English](README.md) | [中文](README_zh.md)

[![License MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Multi-platform AI chat bot with extensible agent system, tools, skills, and scheduling.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [CLI Commands](#cli-commands)
- [License](#license)

## Features

- **Multi-channel** — Telegram, Web UI (SSE streaming)
- **Multi-LLM** — Anthropic, OpenAI, and OpenAI-compatible providers (Ollama, etc.)
- **25+ built-in tools** — file ops, web fetch, search, memory, scheduling, sub-agents
- **Skills** — plugin system with SKILL.md, supports ClawHub marketplace
- **Hooks** — event-driven extensibility (`before_llm`, `after_tool`)
- **Scheduler** — cron and one-shot task scheduling with timezone support
- **Memory** — structured memory with search, global/per-chat scoping, auto-archival
- **MCP** — Model Context Protocol server integration (stdio & HTTP)
- **Embedding** — OpenAI-compatible embedding API for semantic search

## Installation

### Homebrew (macOS / Linux)

```bash
brew tap yifanes/tap
brew install miniclawd
```

### Shell Script

```bash
curl -fsSL https://raw.githubusercontent.com/yifanes/miniclawd/main/install.sh | bash
```

Environment variables:

| Variable | Default | Description |
|---|---|---|
| `MINICLAWD_INSTALL_DIR` | `/usr/local/bin` or `~/.local/bin` | Installation directory |
| `MINICLAWD_INSTALL_METHOD` | `auto` | `auto`, `release`, `homebrew`, or `go` |

### Go Install

```bash
go install github.com/yifanes/miniclawd@latest
```

### Build from Source

```bash
git clone https://github.com/yifanes/miniclawd.git
cd miniclawd
go build -o miniclawd .
```

## Quick Start

```bash
# Interactive setup — generates config file
miniclawd setup

# Check configuration
miniclawd doctor

# Start the bot
miniclawd start
```

## CLI Commands

| Command | Description |
|---|---|
| `start` | Start the bot |
| `setup` | Interactive setup wizard |
| `doctor` | Run preflight diagnostics |
| `hooks` | Manage hooks (list, enable, disable) |
| `version` | Print version |
| `help` | Show help |

## License

[MIT](LICENSE)
