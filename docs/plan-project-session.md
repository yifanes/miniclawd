# 支持项目级别的 Session

## Context

当前 miniclawd 的 session 是被动创建的——第一条消息到来时才会建立 chat_id 和 session。
工具的工作目录（working_dir）虽然配置了 `IsolationChat` 模式，但实际上所有 chat 共享同一个全局 `working_dir`，隔离并未落地。

**目标**：
1. Discord 创建文字频道 → 立即建立隔离的 session + 专属 workspace 目录
2. Discord 删除频道 → 自动清理全部数据（DB + 文件系统）
3. 真正实现 `WorkingDirIsolation = IsolationChat`：每个 chat 有独立的工作目录

---

## 架构理解与设计延伸

### 现有基础（可直接复用）

- `config.WorkingDirIsolation` 已定义 `IsolationChat / IsolationShared`，默认值已是 `IsolationChat`，但**从未实际生效**
- `config.GroupDir(chatID)` → `{DataDir}/runtime/groups/{chatID}/` 已存在，用于存档和 AGENTS.md
- `db.ResolveOrCreateChatID()` 是幂等的，重复调用安全
- `db.DeleteChatData(chatID)` 级联删除所有 DB 数据
- `tools.InjectAuthContext` / `ExtractAuthContext` 已有完整的 JSON 注入模式，可照抄给 working dir

### 核心设计决策

**Working dir 如何传递给工具？**

- ❌ 克隆 ToolRegistry（每次请求重建 25+ 工具，有内存和并发问题）
- ❌ context.Context 传值（隐式，难追踪，需改所有工具）
- ✅ **照搬 `__miniclawd_auth` 模式**：在 JSON input 注入 `__miniclawd_workdir` 字段，工具在 Execute 开头读取覆盖值，fallback 到 `t.workingDir`

这样只需改 6 个文件工具，不需要改工具接口或 registry 架构。

---

## 实现步骤

### Step 1 — config: 添加 WorkspaceDir 和 ResolveWorkingDir 辅助方法

**文件**: `internal/config/config.go`

```go
// WorkspaceDir 返回 chat 的专属工具工作目录
func (c *Config) WorkspaceDir(chatID int64) string {
    return filepath.Join(c.GroupDir(chatID), "workspace")
}

// ResolveWorkingDir 根据隔离模式返回有效工作目录
func (c *Config) ResolveWorkingDir(chatID int64) string {
    if c.WorkingDirIsolation == IsolationChat {
        return c.WorkspaceDir(chatID)
    }
    return c.WorkingDir
}
```

---

### Step 2 — storage: 添加 GetChatIDByExternal 只读查询

**文件**: `internal/storage/chats.go`

ChannelDelete 需要反向查找 chat_id，不能用 ResolveOrCreateChatID（会误建行）。
`idx_chats_channel_external` 索引（V2 migration）已完美覆盖此查询，无需 schema 变更。

```go
func (d *Database) GetChatIDByExternal(channel, externalID string) (chatID int64, found bool, err error) {
    err = d.db.QueryRowContext(context.Background(),
        `SELECT chat_id FROM chats WHERE channel = ? AND external_chat_id = ?`,
        channel, externalID,
    ).Scan(&chatID)
    if errors.Is(err, sql.ErrNoRows) {
        return 0, false, nil
    }
    return chatID, err == nil, err
}
```

---

### Step 3 — tools/tool.go: 添加 working dir 注入/提取辅助函数

**文件**: `internal/tools/tool.go`（照搬 auth 模式）

```go
const workingDirOverrideKey = "__miniclawd_workdir"

func InjectWorkingDir(input json.RawMessage, dir string) json.RawMessage {
    if dir == "" { return input }
    var m map[string]json.RawMessage
    json.Unmarshal(input, &m)
    if m == nil { m = make(map[string]json.RawMessage) }
    m[workingDirOverrideKey], _ = json.Marshal(dir)
    out, _ := json.Marshal(m)
    return out
}

func ExtractWorkingDir(input json.RawMessage) string {
    var wrapper struct {
        Dir string `json:"__miniclawd_workdir"`
    }
    json.Unmarshal(input, &wrapper)
    return wrapper.Dir
}
```

---

### Step 4 — tools/registry.go: 扩展 ExecuteWithAuth 签名

**文件**: `internal/tools/registry.go`

在现有 `ExecuteWithAuth` 基础上**增加** working dir 注入：

```go
// ExecuteWithAuth 同时注入 auth 和 working dir override
func (r *ToolRegistry) ExecuteWithAuth(ctx context.Context, name string,
    input json.RawMessage, auth *ToolAuthContext, workingDir ...string,
) ToolResult {
    if auth != nil {
        input = InjectAuthContext(input, auth)
    }
    if len(workingDir) > 0 && workingDir[0] != "" {
        input = InjectWorkingDir(input, workingDir[0])
    }
    return r.Execute(ctx, name, input)
}
```

用可变参数保持向后兼容，所有现有调用不需要修改。

---

### Step 5 — 6个文件工具: 读取 working dir override

涉及文件（机械修改，每个只改开头几行）：
- `internal/tools/bash.go`
- `internal/tools/readfile.go`
- `internal/tools/writefile.go`
- `internal/tools/editfile.go`
- `internal/tools/glob.go`
- `internal/tools/grep.go`

修改模式（以 bash.go 为例）：

```go
func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) ToolResult {
    workingDir := t.workingDir
    if override := ExtractWorkingDir(input); override != "" {
        workingDir = override
    }
    // ...后续 t.workingDir 全部替换为 workingDir 局部变量
    cmd.Dir = workingDir
```

---

### Step 6 — agent/engine.go: AgentRequestContext 添加 WorkingDir

**文件**: `internal/agent/engine.go`

```go
type AgentRequestContext struct {
    CallerChannel string
    ChatID        int64
    ChatType      string
    WorkingDir    string // 空字符串 = 使用工具默认值
}
```

在工具执行处传入：

```go
// 原来（engine.go 约 274 行）:
result := deps.Tools.ExecuteWithAuth(ctx, tu.Name, tu.Input, auth)

// 改为:
result := deps.Tools.ExecuteWithAuth(ctx, tu.Name, tu.Input, auth, reqCtx.WorkingDir)
```

---

### Step 7 — Discord 适配器: ChannelCreate / ChannelDelete 事件

**文件**: `internal/channels/discord.go`

**7a. 添加 IntentsGuilds intent**（CHANNEL_CREATE/DELETE 事件所需）

```go
// NewDiscordAdapter 中:
s.Identify.Intents = discordgo.IntentsGuilds |  // 新增：频道生命周期事件
    discordgo.IntentsGuildMessages |
    discordgo.IntentsDirectMessages |
    discordgo.IntentsMessageContent
```

注：`IntentsGuilds` 不是特权 intent，无需 Discord Developer Portal 额外配置。

**7b. 在 StartDiscordBot 注册两个新 handler**

```go
adapter.session.AddHandler(func(s *discordgo.Session, e *discordgo.ChannelCreate) {
    go handleDiscordChannelCreate(ctx, adapter, db, deps.Config, e.Channel)
})
adapter.session.AddHandler(func(s *discordgo.Session, e *discordgo.ChannelDelete) {
    go handleDiscordChannelDelete(ctx, db, deps.Config, e.Channel)
})
```

**7c. handleDiscordChannelCreate 实现**

```go
func handleDiscordChannelCreate(ctx context.Context, adapter *DiscordAdapter,
    db *storage.Database, cfg *config.Config, ch *discordgo.Channel) {

    // 只处理服务器文字频道（Type 0）和公告频道（Type 5）
    if ch.GuildID == "" { return }
    if ch.Type != discordgo.ChannelTypeGuildText &&
        ch.Type != discordgo.ChannelTypeGuildNews { return }

    // 检查 allowed_channels 白名单
    if !isChannelAllowed(adapter.allowedChannels, ch.ID) { return }

    title := ch.Name
    chatID, err := db.ResolveOrCreateChatID("discord", ch.ID, &title, "discord_guild")
    if err != nil {
        log.Printf("[discord] channel_create: %v", err); return
    }

    // 预创建 workspace 目录
    wsDir := cfg.WorkspaceDir(chatID)
    if err := os.MkdirAll(wsDir, 0o755); err != nil {
        log.Printf("[discord] channel_create: workspace mkdir: %v", err)
    }
    log.Printf("[discord] channel_create: #%s → chat_id=%d workspace=%s", ch.Name, chatID, wsDir)
}
```

**7d. handleDiscordChannelDelete 实现**

```go
func handleDiscordChannelDelete(ctx context.Context,
    db *storage.Database, cfg *config.Config, ch *discordgo.Channel) {

    if ch.GuildID == "" { return }
    if ch.Type != discordgo.ChannelTypeGuildText &&
        ch.Type != discordgo.ChannelTypeGuildNews { return }

    chatID, found, err := db.GetChatIDByExternal("discord", ch.ID)
    if err != nil { log.Printf("[discord] channel_delete lookup: %v", err); return }
    if !found { return } // 从未注册过（如不在白名单），跳过

    channelName := ch.Name
    groupDir := cfg.GroupDir(chatID)

    // 清理 DB（messages, session, memories, tasks 全部级联删除）
    if err := db.DeleteChatData(chatID); err != nil {
        log.Printf("[discord] channel_delete db: %v", err)
    }
    // 清理文件系统（workspace/ + archives/ + AGENTS.md）
    if err := os.RemoveAll(groupDir); err != nil && !os.IsNotExist(err) {
        log.Printf("[discord] channel_delete fs: %v", err)
    }
    log.Printf("[discord] channel_delete: cleaned up #%s (chat_id=%d)", channelName, chatID)
}
```

**7e. handleDiscordMessage 中填充 WorkingDir 并确保目录存在**

```go
// 在 ResolveOrCreateChatID 成功之后，ProcessWithAgent 之前：
reqCtx := agent.AgentRequestContext{
    CallerChannel: "discord",
    ChatID:        chatID,
    ChatType:      chatType,
    WorkingDir:    deps.Config.ResolveWorkingDir(chatID),  // 新增
}

// 确保目录存在（兼容 bot 启动前就存在的频道）
if deps.Config.WorkingDirIsolation == config.IsolationChat {
    os.MkdirAll(deps.Config.WorkspaceDir(chatID), 0o755)
}
```

---

### Step 8 — 其他调用方补全 WorkingDir

搜索所有构建 `AgentRequestContext` 的地方，统一补上 `WorkingDir`：

- `internal/channels/telegram.go` — Telegram 消息处理
- `internal/web/` — Web SSE 处理
- `internal/scheduler/scheduler.go` — 定时任务

```go
WorkingDir: deps.Config.ResolveWorkingDir(chatID),
```

---

## 受影响文件汇总

| 文件 | 变更类型 |
|------|----------|
| `internal/config/config.go` | 添加 `WorkspaceDir()`、`ResolveWorkingDir()` |
| `internal/storage/chats.go` | 添加 `GetChatIDByExternal()` |
| `internal/tools/tool.go` | 添加 `InjectWorkingDir()`、`ExtractWorkingDir()` |
| `internal/tools/registry.go` | 扩展 `ExecuteWithAuth()` variadic 参数 |
| `internal/tools/bash.go` | 读取 working dir override |
| `internal/tools/readfile.go` | 读取 working dir override |
| `internal/tools/writefile.go` | 读取 working dir override |
| `internal/tools/editfile.go` | 读取 working dir override |
| `internal/tools/glob.go` | 读取 working dir override |
| `internal/tools/grep.go` | 读取 working dir override |
| `internal/agent/engine.go` | `AgentRequestContext` 添加 `WorkingDir`；传递给 `ExecuteWithAuth` |
| `internal/channels/discord.go` | 添加 `IntentsGuilds`；`ChannelCreate`/`ChannelDelete` handlers；填充 `WorkingDir` |
| `internal/channels/telegram.go` | 填充 `reqCtx.WorkingDir` |
| `internal/web/` (handler) | 填充 `reqCtx.WorkingDir` |
| `internal/scheduler/scheduler.go` | 填充 `reqCtx.WorkingDir` |

---

## 验证方式

### 单元测试
```bash
go test ./internal/storage/...   # GetChatIDByExternal
go test ./internal/tools/...     # working dir override
go test ./internal/config/...    # WorkspaceDir/ResolveWorkingDir
go test ./...                    # 全量回归
```

### 端到端验证（Discord）
1. **ChannelCreate**: 在 Discord 服务器创建新文字频道 → 查看日志 `channel_create: #xxx → chat_id=N`，确认 `{DataDir}/runtime/groups/N/workspace/` 目录已创建
2. **工作目录隔离**: 在两个不同频道分别执行 `bash: echo hello > test.txt`，确认文件分别写到各自 workspace 而非共享 tmp
3. **ChannelDelete**: 删除 Discord 频道 → 日志 `channel_delete: cleaned up #xxx`，`{DataDir}/runtime/groups/N/` 目录已删除，DB 无残留记录
4. **兼容性**: 已有频道（bot 启动前创建）发消息时 workspace 目录自动创建，session 正常
