package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all MiniClawd configuration.
type Config struct {
	// LLM / API
	LLMProvider        string  `yaml:"llm_provider"`
	APIKey             string  `yaml:"api_key"`
	Model              string  `yaml:"model"`
	LLMBaseURL         *string `yaml:"llm_base_url"`
	MaxTokens          uint32  `yaml:"max_tokens"`
	MaxToolIterations  int     `yaml:"max_tool_iterations"`
	CompactionTimeout  uint64  `yaml:"compaction_timeout_secs"`
	MaxHistoryMessages int     `yaml:"max_history_messages"`
	MaxDocumentSizeMB  uint64  `yaml:"max_document_size_mb"`
	MemoryTokenBudget  int     `yaml:"memory_token_budget"`
	MaxSessionMessages int     `yaml:"max_session_messages"`
	CompactKeepRecent  int     `yaml:"compact_keep_recent"`
	ShowThinking       bool    `yaml:"show_thinking"`

	// Paths & environment
	DataDir              string              `yaml:"data_dir"`
	WorkingDir           string              `yaml:"working_dir"`
	WorkingDirIsolation  WorkingDirIsolation `yaml:"working_dir_isolation"`
	Sandbox              SandboxConfig       `yaml:"sandbox"`
	Timezone             string              `yaml:"timezone"`
	ControlChatIDs       []int64             `yaml:"control_chat_ids"`

	// Discord
	DiscordBotToken        *string  `yaml:"discord_bot_token"`
	DiscordAllowedChannels []uint64 `yaml:"discord_allowed_channels"`
	DiscordNoMention       bool     `yaml:"discord_no_mention"`

	// Web UI
	WebEnabled                bool    `yaml:"web_enabled"`
	WebHost                   string  `yaml:"web_host"`
	WebPort                   uint16  `yaml:"web_port"`
	WebAuthToken              *string `yaml:"web_auth_token"`
	WebMaxInflightPerSession  int     `yaml:"web_max_inflight_per_session"`
	WebMaxRequestsPerWindow   int     `yaml:"web_max_requests_per_window"`
	WebRateWindowSeconds      uint64  `yaml:"web_rate_window_seconds"`
	WebRunHistoryLimit        int     `yaml:"web_run_history_limit"`
	WebSessionIdleTTLSeconds  uint64  `yaml:"web_session_idle_ttl_seconds"`

	// Embedding
	EmbeddingProvider *string `yaml:"embedding_provider"`
	EmbeddingAPIKey   *string `yaml:"embedding_api_key"`
	EmbeddingBaseURL  *string `yaml:"embedding_base_url"`
	EmbeddingModel    *string `yaml:"embedding_model"`
	EmbeddingDim      *int    `yaml:"embedding_dim"`
	OpenAIAPIKey      *string `yaml:"openai_api_key"`

	// Pricing
	ModelPrices []ModelPrice `yaml:"model_prices"`

	// Reflector
	ReflectorEnabled      bool   `yaml:"reflector_enabled"`
	ReflectorIntervalMins uint64 `yaml:"reflector_interval_mins"`

	// Soul
	SoulPath *string `yaml:"soul_path"`

	// ClawHub
	ClawHubRegistry             string  `yaml:"clawhub_registry"`
	ClawHubToken                *string `yaml:"clawhub_token"`
	ClawHubAgentToolsEnabled    bool    `yaml:"clawhub_agent_tools_enabled"`
	ClawHubSkipSecurityWarnings bool    `yaml:"clawhub_skip_security_warnings"`

	// Channel registry (dynamic config)
	Channels map[string]yaml.Node `yaml:"channels"`

	// Legacy channel fields
	TelegramBotToken string  `yaml:"telegram_bot_token"`
	BotUsername       string  `yaml:"bot_username"`
	AllowedGroups    []int64 `yaml:"allowed_groups"`
}

// WorkingDirIsolation determines how tool working directories are isolated.
type WorkingDirIsolation string

const (
	IsolationShared WorkingDirIsolation = "shared"
	IsolationChat   WorkingDirIsolation = "chat"
)

// SandboxConfig controls sandboxed command execution.
type SandboxConfig struct {
	Mode            string  `yaml:"mode"`             // "off" or "all"
	Backend         string  `yaml:"backend"`          // "auto" or "docker"
	Image           string  `yaml:"image"`            // default "ubuntu:25.10"
	ContainerPrefix string  `yaml:"container_prefix"` // default "miniclawd-sandbox"
	NoNetwork       bool    `yaml:"no_network"`       // default true
	RequireRuntime  bool    `yaml:"require_runtime"`
	MemoryLimit     *string `yaml:"memory_limit"`
	CPUQuota        *float64 `yaml:"cpu_quota"`
	PidsLimit       *uint32 `yaml:"pids_limit"`
}

// ModelPrice defines per-model token pricing.
type ModelPrice struct {
	Model              string  `yaml:"model"`
	InputPerMillionUSD float64 `yaml:"input_per_million_usd"`
	OutputPerMillionUSD float64 `yaml:"output_per_million_usd"`
}

// DefaultConfig returns a Config with all defaults applied.
func DefaultConfig() Config {
	return Config{
		LLMProvider:              "anthropic",
		MaxTokens:               8192,
		MaxToolIterations:       100,
		CompactionTimeout:       180,
		MaxHistoryMessages:      50,
		MaxDocumentSizeMB:       100,
		MemoryTokenBudget:       1500,
		MaxSessionMessages:      40,
		CompactKeepRecent:       20,
		DataDir:                 "./miniclawd.data",
		WorkingDir:              "./tmp",
		WorkingDirIsolation:     IsolationChat,
		Timezone:                "UTC",
		WebEnabled:              false,
		WebHost:                 "127.0.0.1",
		WebPort:                 10961,
		WebMaxInflightPerSession: 2,
		WebMaxRequestsPerWindow:  8,
		WebRateWindowSeconds:    10,
		WebRunHistoryLimit:      512,
		WebSessionIdleTTLSeconds: 300,
		ReflectorEnabled:        true,
		ReflectorIntervalMins:   15,
		ClawHubRegistry:         "https://clawhub.ai",
		ClawHubAgentToolsEnabled: true,
		Sandbox: SandboxConfig{
			Mode:            "off",
			Backend:         "auto",
			Image:           "ubuntu:25.10",
			ContainerPrefix: "miniclawd-sandbox",
			NoNetwork:       true,
		},
	}
}

// LoadConfig reads and parses the configuration file.
// Resolution order: MINICLAWD_CONFIG env → ./miniclawd.config.yaml → ./miniclawd.config.yml
func LoadConfig() (*Config, error) {
	path := os.Getenv("MINICLAWD_CONFIG")
	if path == "" {
		candidates := []string{"miniclawd.config.yaml", "miniclawd.config.yml"}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				path = c
				break
			}
		}
	}
	if path == "" {
		return nil, fmt.Errorf("no config file found (set MINICLAWD_CONFIG or create miniclawd.config.yaml)")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg.postDeserialize()
	return &cfg, nil
}

// postDeserialize normalizes and validates config after YAML parsing.
func (c *Config) postDeserialize() {
	c.LLMProvider = strings.ToLower(strings.TrimSpace(c.LLMProvider))

	// Auto-select model by provider if empty.
	if c.Model == "" {
		switch c.LLMProvider {
		case "anthropic":
			c.Model = "claude-sonnet-4-5-20250929"
		case "ollama":
			c.Model = "llama3.2"
		default:
			c.Model = "gpt-4o"
		}
	}

	// Ensure critical limits have sane minimums.
	if c.MemoryTokenBudget <= 0 {
		c.MemoryTokenBudget = 1500
	}
	if c.MaxToolIterations <= 0 {
		c.MaxToolIterations = 100
	}
	if c.MaxSessionMessages <= 0 {
		c.MaxSessionMessages = 40
	}
	if c.CompactKeepRecent <= 0 {
		c.CompactKeepRecent = 20
	}
	if c.WebMaxInflightPerSession <= 0 {
		c.WebMaxInflightPerSession = 2
	}
	if c.WebMaxRequestsPerWindow <= 0 {
		c.WebMaxRequestsPerWindow = 8
	}
	if c.WebRateWindowSeconds <= 0 {
		c.WebRateWindowSeconds = 10
	}

	// Synthesize legacy channel fields into Channels map if Channels is empty.
	if len(c.Channels) == 0 && c.TelegramBotToken != "" {
		// Legacy Telegram config present; leave it for the adapter to read directly.
	}
}

// Validate checks for configuration errors.
func (c *Config) Validate() error {
	// Check that at least one channel is enabled.
	hasTelegram := c.TelegramBotToken != ""
	hasDiscord := c.DiscordBotToken != nil && *c.DiscordBotToken != ""
	hasWeb := c.WebEnabled
	hasChannels := len(c.Channels) > 0

	if !hasTelegram && !hasDiscord && !hasWeb && !hasChannels {
		return fmt.Errorf("at least one channel must be enabled (telegram, discord, web, or channels config)")
	}

	// Require auth token for non-local web hosts.
	if c.WebEnabled && !isLocalHost(c.WebHost) {
		if c.WebAuthToken == nil || *c.WebAuthToken == "" {
			return fmt.Errorf("web_auth_token is required when web_host is not localhost")
		}
	}

	return nil
}

func isLocalHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1" || host == ""
}

// RuntimeDir returns the runtime directory path.
func (c *Config) RuntimeDir() string {
	return filepath.Join(c.DataDir, "runtime")
}

// SkillsDir returns the skills directory path.
func (c *Config) SkillsDir() string {
	return filepath.Join(c.DataDir, "skills")
}

// RuntimeSkillsDir returns the compiled/runtime skills directory.
func (c *Config) RuntimeSkillsDir() string {
	return filepath.Join(c.RuntimeDir(), "skills")
}

// DBPath returns the SQLite database file path.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "miniclawd.db")
}

// GroupDir returns the per-chat data directory.
func (c *Config) GroupDir(chatID int64) string {
	return filepath.Join(c.RuntimeDir(), "groups", fmt.Sprintf("%d", chatID))
}
