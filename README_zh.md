# MiniClawd

[English](README.md) | [中文](README_zh.md)

[![License MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

多平台 AI 聊天机器人，支持可扩展的 Agent 系统、工具、技能和定时任务。

## 目录

- [功能特性](#功能特性)
- [安装](#安装)
- [快速开始](#快速开始)
- [命令行](#命令行)
- [开源协议](#开源协议)

## 功能特性

- **多渠道** — Telegram、Web UI（SSE 流式输出）
- **多模型** — Anthropic、OpenAI 及 OpenAI 兼容服务（Ollama 等）
- **25+ 内置工具** — 文件操作、网页抓取、搜索、记忆、定时任务、子代理
- **技能系统** — 基于 SKILL.md 的插件机制，支持 ClawHub 技能市场
- **Hooks** — 事件驱动扩展（`before_llm`、`after_tool`）
- **定时任务** — 支持 cron 表达式和一次性任务，支持时区
- **记忆系统** — 结构化记忆，支持搜索、全局/会话级作用域、自动归档
- **MCP** — Model Context Protocol 服务器集成（stdio 和 HTTP）
- **Embedding** — OpenAI 兼容的向量接口，用于语义搜索

## 安装

### Homebrew（macOS / Linux）

```bash
brew tap yifanes/tap
brew install miniclawd
```

### 一键脚本

```bash
curl -fsSL https://raw.githubusercontent.com/yifanes/miniclawd/main/install.sh | bash
```

环境变量：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `MINICLAWD_INSTALL_DIR` | `/usr/local/bin` 或 `~/.local/bin` | 安装目录 |
| `MINICLAWD_INSTALL_METHOD` | `auto` | `auto`、`release`、`homebrew` 或 `go` |

### Go Install

```bash
go install github.com/yifanes/miniclawd@latest
```

### 从源码构建

```bash
git clone https://github.com/yifanes/miniclawd.git
cd miniclawd
go build -o miniclawd .
```

## 快速开始

```bash
# 交互式配置 — 生成配置文件
miniclawd setup

# 检查配置
miniclawd doctor

# 启动机器人
miniclawd start
```

## 命令行

| 命令 | 说明 |
|---|---|
| `start` | 启动机器人 |
| `setup` | 交互式配置向导 |
| `doctor` | 运行预检诊断 |
| `hooks` | 管理 hooks（列出、启用、禁用） |
| `version` | 打印版本号 |
| `help` | 显示帮助 |

## 开源协议

[MIT](LICENSE)
