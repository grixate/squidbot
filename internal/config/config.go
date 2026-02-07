package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Agents    AgentsConfig    `json:"agents"`
	Providers ProvidersConfig `json:"providers"`
	Channels  ChannelsConfig  `json:"channels"`
	Tools     ToolsConfig     `json:"tools"`
	Gateway   GatewayConfig   `json:"gateway"`
	Storage   StorageConfig   `json:"storage"`
	Runtime   RuntimeConfig   `json:"runtime"`
}

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
}

type AgentDefaults struct {
	Workspace         string  `json:"workspace"`
	Model             string  `json:"model"`
	MaxTokens         int     `json:"maxTokens"`
	Temperature       float64 `json:"temperature"`
	MaxToolIterations int     `json:"maxToolIterations"`
	TurnTimeoutSec    int     `json:"turnTimeoutSec"`
	ToolTimeoutSec    int     `json:"toolTimeoutSec"`
}

type ProvidersConfig struct {
	OpenRouter ProviderConfig `json:"openrouter"`
	Anthropic  ProviderConfig `json:"anthropic"`
	OpenAI     ProviderConfig `json:"openai"`
}

type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	APIBase string `json:"apiBase,omitempty"`
	Model   string `json:"model,omitempty"`
}

type ChannelsConfig struct {
	Telegram TelegramConfig `json:"telegram"`
}

type TelegramConfig struct {
	Enabled   bool     `json:"enabled"`
	Token     string   `json:"token"`
	AllowFrom []string `json:"allowFrom"`
}

type ToolsConfig struct {
	Web WebToolsConfig `json:"web"`
}

type WebToolsConfig struct {
	Search WebSearchConfig `json:"search"`
}

type WebSearchConfig struct {
	APIKey     string `json:"apiKey"`
	MaxResults int    `json:"maxResults"`
}

type GatewayConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type StorageConfig struct {
	Backend string `json:"backend"`
	DBPath  string `json:"dbPath"`
}

type RuntimeConfig struct {
	MailboxSize          int           `json:"mailboxSize"`
	ActorIdleTTL         DurationValue `json:"actorIdleTtl"`
	HeartbeatIntervalSec int           `json:"heartbeatIntervalSec"`
}

type DurationValue struct {
	time.Duration
}

func (d DurationValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *DurationValue) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		parsed, parseErr := time.ParseDuration(asString)
		if parseErr != nil {
			return parseErr
		}
		d.Duration = parsed
		return nil
	}

	var asNumber int64
	if err := json.Unmarshal(data, &asNumber); err == nil {
		d.Duration = time.Duration(asNumber)
		return nil
	}

	return fmt.Errorf("invalid duration value: %s", string(data))
}

func Default() Config {
	home := HomeDir()
	workspace := filepath.Join(home, "workspace")
	return Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         workspace,
				Model:             "anthropic/claude-opus-4-1",
				MaxTokens:         8192,
				Temperature:       0.7,
				MaxToolIterations: 20,
				TurnTimeoutSec:    120,
				ToolTimeoutSec:    60,
			},
		},
		Providers: ProvidersConfig{},
		Channels: ChannelsConfig{
			Telegram: TelegramConfig{
				Enabled:   false,
				AllowFrom: []string{},
			},
		},
		Tools: ToolsConfig{
			Web: WebToolsConfig{
				Search: WebSearchConfig{
					MaxResults: 5,
				},
			},
		},
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
			Port: 18789,
		},
		Storage: StorageConfig{
			Backend: "bbolt",
			DBPath:  filepath.Join(home, "data", "squidbot.db"),
		},
		Runtime: RuntimeConfig{
			MailboxSize:          64,
			ActorIdleTTL:         DurationValue{Duration: 15 * time.Minute},
			HeartbeatIntervalSec: 1800,
		},
	}
}

func HomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ".squidbot"
	}
	return filepath.Join(h, ".squidbot")
}

func ConfigPath() string {
	return filepath.Join(HomeDir(), "config.json")
}

func DataRoot() string {
	return filepath.Join(HomeDir(), "data")
}

func WorkspacePath(cfg Config) string {
	if cfg.Agents.Defaults.Workspace == "" {
		return filepath.Join(HomeDir(), "workspace")
	}
	return expandPath(cfg.Agents.Defaults.Workspace)
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		h, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(h, path[2:])
		}
	}
	return path
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = ConfigPath()
	}
	path = expandPath(path)
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(&cfg)
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return cfg, err
	}
	applyEnvOverrides(&cfg)
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = ConfigPath()
	}
	path = expandPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func applyEnvOverrides(cfg *Config) {
	env := map[string]*string{
		"SQUIDBOT_OPENROUTER_API_KEY":  &cfg.Providers.OpenRouter.APIKey,
		"SQUIDBOT_OPENROUTER_API_BASE": &cfg.Providers.OpenRouter.APIBase,
		"SQUIDBOT_ANTHROPIC_API_KEY":   &cfg.Providers.Anthropic.APIKey,
		"SQUIDBOT_OPENAI_API_KEY":      &cfg.Providers.OpenAI.APIKey,
		"SQUIDBOT_TELEGRAM_TOKEN":      &cfg.Channels.Telegram.Token,
		"SQUIDBOT_BRAVE_API_KEY":       &cfg.Tools.Web.Search.APIKey,
	}
	for key, target := range env {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			*target = value
		}
	}

	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TELEGRAM_ENABLED")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Channels.Telegram.Enabled = parsed
		}
	}
}

func (c Config) PrimaryProvider() (name string, provider ProviderConfig) {
	if strings.TrimSpace(c.Providers.OpenRouter.APIKey) != "" {
		return "openrouter", c.Providers.OpenRouter
	}
	if strings.TrimSpace(c.Providers.Anthropic.APIKey) != "" {
		return "anthropic", c.Providers.Anthropic
	}
	if strings.TrimSpace(c.Providers.OpenAI.APIKey) != "" {
		return "openai", c.Providers.OpenAI
	}
	return "", ProviderConfig{}
}
