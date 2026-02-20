package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yifanes/miniclawd/internal/agent"
	"github.com/yifanes/miniclawd/internal/channels"
	"github.com/yifanes/miniclawd/internal/config"
	"github.com/yifanes/miniclawd/internal/embedding"
	"github.com/yifanes/miniclawd/internal/hooks"
	"github.com/yifanes/miniclawd/internal/llm"
	"github.com/yifanes/miniclawd/internal/scheduler"
	"github.com/yifanes/miniclawd/internal/skills"
	"github.com/yifanes/miniclawd/internal/storage"
	"github.com/yifanes/miniclawd/internal/tools"
	"github.com/yifanes/miniclawd/internal/web"
)

// AppState holds all shared application state.
type AppState struct {
	Config    *config.Config
	Registry  *channels.ChannelRegistry
	DB        *storage.Database
	Memory    *MemoryManager
	Skills    *skills.SkillManager
	Hooks     *hooks.HookManager
	LLM       llm.LLMProvider
	Embedding embedding.EmbeddingProvider // may be nil
	Tools     *tools.ToolRegistry
}

// Run is the main entry point that wires everything together and starts the bot.
func Run(cfg *config.Config, db *storage.Database) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[app] shutting down...")
		cancel()
	}()

	// Create MemoryManager and ensure directories.
	memory := NewMemoryManager(cfg.DataDir)
	if err := memory.EnsureDirectories(); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	// Create LLM provider.
	provider := llm.CreateProvider(cfg)
	log.Printf("[app] LLM provider: %s (model: %s)", provider.ProviderName(), provider.ModelName())

	// Create embedding provider (optional).
	var embeddingProvider embedding.EmbeddingProvider
	if cfg.EmbeddingProvider != nil && *cfg.EmbeddingProvider != "" {
		apiKey := ""
		if cfg.EmbeddingAPIKey != nil {
			apiKey = *cfg.EmbeddingAPIKey
		} else if cfg.OpenAIAPIKey != nil {
			apiKey = *cfg.OpenAIAPIKey
		}
		baseURL := ""
		if cfg.EmbeddingBaseURL != nil {
			baseURL = *cfg.EmbeddingBaseURL
		}
		model := ""
		if cfg.EmbeddingModel != nil {
			model = *cfg.EmbeddingModel
		}
		dim := 0
		if cfg.EmbeddingDim != nil {
			dim = *cfg.EmbeddingDim
		}
		embeddingProvider = embedding.NewOpenAIEmbeddingProvider(apiKey, baseURL, model, dim)
		log.Printf("[app] embedding provider: %s (model: %s)", *cfg.EmbeddingProvider, embeddingProvider.ModelName())
	}

	// Create SkillManager.
	skillsMgr := skills.NewSkillManager(cfg.SkillsDir())
	log.Printf("[app] skills: %d discovered", len(skillsMgr.List()))

	// Create HookManager.
	hooksMgr := hooks.NewHookManager(cfg.DataDir)
	log.Printf("[app] hooks: %d discovered", len(hooksMgr.ListHooks()))

	// Build ChannelRegistry.
	registry := channels.NewChannelRegistry()

	// Register Web adapter.
	if cfg.WebEnabled {
		registry.Register(channels.NewWebAdapter())
	}

	// Register Telegram adapter.
	var telegramAdapter *channels.TelegramAdapter
	if cfg.TelegramBotToken != "" {
		var err error
		telegramAdapter, err = channels.NewTelegramAdapter(cfg.TelegramBotToken, cfg.BotUsername, cfg.AllowedGroups)
		if err != nil {
			log.Printf("[app] telegram adapter error: %v", err)
		} else {
			registry.Register(telegramAdapter)
			log.Printf("[app] telegram: @%s", cfg.BotUsername)
		}
	}

	// Build working directory.
	workingDir := cfg.WorkingDir
	os.MkdirAll(workingDir, 0o755)

	// Build ToolRegistry.
	sender := &registrySender{registry: registry, db: db}
	toolRegistry := tools.BuildStandardRegistry(tools.RegistryConfig{
		WorkingDir:      workingDir,
		DataDir:         cfg.DataDir,
		SkillsDir:       cfg.SkillsDir(),
		Timezone:        cfg.Timezone,
		DB:              db,
		Sender:          sender,
		ClawHubEnabled:  cfg.ClawHubAgentToolsEnabled,
		ClawHubRegistry: cfg.ClawHubRegistry,
		ClawHubToken:    cfg.ClawHubToken,
	})
	log.Printf("[app] tools: %d registered", len(toolRegistry.ToolNames()))

	// Build AgentDeps.
	deps := &agent.AgentDeps{
		Config: cfg,
		DB:     db,
		LLM:    provider,
		Tools:  toolRegistry,
		Skills: skillsMgr.BuildCatalog(),
	}

	// Build AppState.
	state := &AppState{
		Config:    cfg,
		Registry:  registry,
		DB:        db,
		Memory:    memory,
		Skills:    skillsMgr,
		Hooks:     hooksMgr,
		LLM:       provider,
		Embedding: embeddingProvider,
		Tools:     toolRegistry,
	}
	_ = state

	// Spawn scheduler.
	scheduler.SpawnScheduler(ctx, db, deps, registry)
	log.Println("[app] scheduler started")

	// Spawn reflector.
	if cfg.ReflectorEnabled {
		scheduler.SpawnReflector(ctx, db, provider, int(cfg.ReflectorIntervalMins))
		log.Printf("[app] reflector started (interval: %dm)", cfg.ReflectorIntervalMins)
	}

	// Start Web server.
	if cfg.WebEnabled {
		go func() {
			if err := web.StartWebServer(ctx, cfg, db, deps); err != nil {
				log.Printf("[app] web server error: %v", err)
			}
		}()
	}

	// Start Telegram bot (blocking).
	if telegramAdapter != nil {
		log.Println("[app] starting telegram long-poll (blocking)...")
		channels.StartTelegramBot(ctx, telegramAdapter, db, deps)
	} else {
		// No telegram: wait for signal.
		log.Println("[app] running (no telegram). Press Ctrl+C to stop.")
		<-ctx.Done()
	}

	return nil
}
