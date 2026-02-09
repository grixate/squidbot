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
	Agents     AgentsConfig     `json:"agents"`
	Providers  ProvidersConfig  `json:"providers"`
	Channels   ChannelsConfig   `json:"channels"`
	Tools      ToolsConfig      `json:"tools"`
	Gateway    GatewayConfig    `json:"gateway"`
	Management ManagementConfig `json:"management"`
	Auth       AuthConfig       `json:"auth"`
	Storage    StorageConfig    `json:"storage"`
	Runtime    RuntimeConfig    `json:"runtime"`
	Memory     MemoryConfig     `json:"memory"`
	Skills     SkillsConfig     `json:"skills"`
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
	Active     string         `json:"active"`
	OpenRouter ProviderConfig `json:"openrouter"`
	Anthropic  ProviderConfig `json:"anthropic"`
	OpenAI     ProviderConfig `json:"openai"`
	Gemini     ProviderConfig `json:"gemini"`
	Ollama     ProviderConfig `json:"ollama"`
	LMStudio   ProviderConfig `json:"lmstudio"`
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

type ManagementConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	PublicBaseURL  string `json:"publicBaseUrl"`
	ServeInGateway bool   `json:"serveInGateway"`
}

type AuthConfig struct {
	PasswordHash      string `json:"passwordHash"`
	PasswordUpdatedAt string `json:"passwordUpdatedAt,omitempty"`
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

type MemoryConfig struct {
	Enabled            bool   `json:"enabled"`
	IndexPath          string `json:"indexPath"`
	TopK               int    `json:"topK"`
	RecencyDays        int    `json:"recencyDays"`
	EmbeddingsProvider string `json:"embeddingsProvider"`
	EmbeddingsModel    string `json:"embeddingsModel"`
}

type SkillsConfig struct {
	Paths []string `json:"paths"`
}

type DurationValue struct {
	time.Duration
}

const (
	ProviderOpenRouter = "openrouter"
	ProviderAnthropic  = "anthropic"
	ProviderOpenAI     = "openai"
	ProviderGemini     = "gemini"
	ProviderOllama     = "ollama"
	ProviderLMStudio   = "lmstudio"
)

var supportedProviders = []string{
	ProviderOpenRouter,
	ProviderAnthropic,
	ProviderOpenAI,
	ProviderGemini,
	ProviderOllama,
	ProviderLMStudio,
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
		Management: ManagementConfig{
			Host:           "127.0.0.1",
			Port:           18790,
			PublicBaseURL:  "",
			ServeInGateway: false,
		},
		Auth: AuthConfig{},
		Storage: StorageConfig{
			Backend: "bbolt",
			DBPath:  filepath.Join(home, "data", "squidbot.db"),
		},
		Runtime: RuntimeConfig{
			MailboxSize:          64,
			ActorIdleTTL:         DurationValue{Duration: 15 * time.Minute},
			HeartbeatIntervalSec: 1800,
		},
		Memory: MemoryConfig{
			Enabled:            true,
			IndexPath:          filepath.Join(home, "data", "memory_index.db"),
			TopK:               8,
			RecencyDays:        30,
			EmbeddingsProvider: "none",
			EmbeddingsModel:    "",
		},
		Skills: SkillsConfig{
			Paths: []string{filepath.Join(workspace, "skills")},
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
		"SQUIDBOT_PROVIDER_ACTIVE":            &cfg.Providers.Active,
		"SQUIDBOT_OPENROUTER_API_KEY":         &cfg.Providers.OpenRouter.APIKey,
		"SQUIDBOT_OPENROUTER_API_BASE":        &cfg.Providers.OpenRouter.APIBase,
		"SQUIDBOT_OPENROUTER_MODEL":           &cfg.Providers.OpenRouter.Model,
		"SQUIDBOT_ANTHROPIC_API_KEY":          &cfg.Providers.Anthropic.APIKey,
		"SQUIDBOT_ANTHROPIC_MODEL":            &cfg.Providers.Anthropic.Model,
		"SQUIDBOT_OPENAI_API_KEY":             &cfg.Providers.OpenAI.APIKey,
		"SQUIDBOT_OPENAI_API_BASE":            &cfg.Providers.OpenAI.APIBase,
		"SQUIDBOT_OPENAI_MODEL":               &cfg.Providers.OpenAI.Model,
		"SQUIDBOT_GEMINI_API_KEY":             &cfg.Providers.Gemini.APIKey,
		"SQUIDBOT_GEMINI_API_BASE":            &cfg.Providers.Gemini.APIBase,
		"SQUIDBOT_GEMINI_MODEL":               &cfg.Providers.Gemini.Model,
		"SQUIDBOT_OLLAMA_API_KEY":             &cfg.Providers.Ollama.APIKey,
		"SQUIDBOT_OLLAMA_API_BASE":            &cfg.Providers.Ollama.APIBase,
		"SQUIDBOT_OLLAMA_MODEL":               &cfg.Providers.Ollama.Model,
		"SQUIDBOT_LMSTUDIO_API_KEY":           &cfg.Providers.LMStudio.APIKey,
		"SQUIDBOT_LMSTUDIO_API_BASE":          &cfg.Providers.LMStudio.APIBase,
		"SQUIDBOT_LMSTUDIO_MODEL":             &cfg.Providers.LMStudio.Model,
		"SQUIDBOT_TELEGRAM_TOKEN":             &cfg.Channels.Telegram.Token,
		"SQUIDBOT_BRAVE_API_KEY":              &cfg.Tools.Web.Search.APIKey,
		"SQUIDBOT_MEMORY_INDEX_PATH":          &cfg.Memory.IndexPath,
		"SQUIDBOT_MEMORY_EMBEDDINGS_PROVIDER": &cfg.Memory.EmbeddingsProvider,
		"SQUIDBOT_MEMORY_EMBEDDINGS_MODEL":    &cfg.Memory.EmbeddingsModel,
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
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_MEMORY_ENABLED")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Memory.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_MEMORY_TOPK")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			cfg.Memory.TopK = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_MEMORY_RECENCY_DAYS")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			cfg.Memory.RecencyDays = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_PATHS")); value != "" {
		paths := strings.Split(value, ",")
		out := make([]string, 0, len(paths))
		for _, path := range paths {
			path = strings.TrimSpace(path)
			if path != "" {
				out = append(out, path)
			}
		}
		if len(out) > 0 {
			cfg.Skills.Paths = out
		}
	}
}

func (c Config) PrimaryProvider() (name string, provider ProviderConfig) {
	if normalized, ok := NormalizeProviderName(c.Providers.Active); ok {
		if selected, exists := c.ProviderByName(normalized); exists {
			return normalized, selected
		}
	}
	return c.legacyPrimaryProvider()
}

func (c Config) legacyPrimaryProvider() (name string, provider ProviderConfig) {
	if strings.TrimSpace(c.Providers.OpenRouter.APIKey) != "" {
		return ProviderOpenRouter, c.Providers.OpenRouter
	}
	if strings.TrimSpace(c.Providers.Anthropic.APIKey) != "" {
		return ProviderAnthropic, c.Providers.Anthropic
	}
	if strings.TrimSpace(c.Providers.OpenAI.APIKey) != "" {
		return ProviderOpenAI, c.Providers.OpenAI
	}
	if strings.TrimSpace(c.Providers.Gemini.APIKey) != "" {
		return ProviderGemini, c.Providers.Gemini
	}
	return "", ProviderConfig{}
}

func SupportedProviders() []string {
	out := make([]string, len(supportedProviders))
	copy(out, supportedProviders)
	return out
}

func NormalizeProviderName(name string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	switch trimmed {
	case ProviderOpenRouter, ProviderAnthropic, ProviderOpenAI, ProviderGemini, ProviderOllama, ProviderLMStudio:
		return trimmed, true
	default:
		return "", false
	}
}

func (c Config) ProviderByName(name string) (ProviderConfig, bool) {
	normalized, ok := NormalizeProviderName(name)
	if !ok {
		return ProviderConfig{}, false
	}
	switch normalized {
	case ProviderOpenRouter:
		return c.Providers.OpenRouter, true
	case ProviderAnthropic:
		return c.Providers.Anthropic, true
	case ProviderOpenAI:
		return c.Providers.OpenAI, true
	case ProviderGemini:
		return c.Providers.Gemini, true
	case ProviderOllama:
		return c.Providers.Ollama, true
	case ProviderLMStudio:
		return c.Providers.LMStudio, true
	default:
		return ProviderConfig{}, false
	}
}

func (c *Config) SetProviderByName(name string, provider ProviderConfig) bool {
	normalized, ok := NormalizeProviderName(name)
	if !ok {
		return false
	}
	switch normalized {
	case ProviderOpenRouter:
		c.Providers.OpenRouter = provider
	case ProviderAnthropic:
		c.Providers.Anthropic = provider
	case ProviderOpenAI:
		c.Providers.OpenAI = provider
	case ProviderGemini:
		c.Providers.Gemini = provider
	case ProviderOllama:
		c.Providers.Ollama = provider
	case ProviderLMStudio:
		c.Providers.LMStudio = provider
	default:
		return false
	}
	return true
}

func ProviderDefaultAPIBase(name string) string {
	switch name {
	case ProviderOpenRouter:
		return "https://openrouter.ai/api/v1"
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case ProviderOllama:
		return "http://localhost:11434/v1"
	case ProviderLMStudio:
		return "http://localhost:1234/v1"
	default:
		return ""
	}
}

func ProviderDefaultModel(name string) string {
	switch name {
	case ProviderGemini:
		return "gemini-3.0-pro"
	case ProviderOllama:
		return "llama3.1:8b"
	case ProviderLMStudio:
		return "local-model"
	default:
		return ""
	}
}

func ValidateActiveProvider(cfg Config) error {
	active := strings.TrimSpace(cfg.Providers.Active)
	if active == "" {
		name, provider := cfg.legacyPrimaryProvider()
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("providers.active must be one of: %s", strings.Join(SupportedProviders(), ", "))
		}
		return validateProviderConfig(name, provider)
	}
	normalized, ok := NormalizeProviderName(active)
	if !ok {
		return fmt.Errorf("providers.active must be one of: %s", strings.Join(SupportedProviders(), ", "))
	}
	provider, exists := cfg.ProviderByName(normalized)
	if !exists {
		return fmt.Errorf("unsupported provider %q", normalized)
	}
	return validateProviderConfig(normalized, provider)
}

func IsSetupComplete(cfg Config) bool {
	if strings.TrimSpace(cfg.Auth.PasswordHash) == "" {
		return false
	}
	if err := ValidateActiveProvider(cfg); err != nil {
		return false
	}
	return validateTelegramOnboarding(cfg) == nil
}

func ProviderRequirements(name string) (requiresAPIKey, requiresModel bool, ok bool) {
	normalized, ok := NormalizeProviderName(name)
	if !ok {
		return false, false, false
	}
	switch normalized {
	case ProviderOpenRouter, ProviderAnthropic, ProviderOpenAI, ProviderGemini:
		return true, false, true
	case ProviderOllama, ProviderLMStudio:
		return false, true, true
	default:
		return false, false, false
	}
}

func validateProviderConfig(name string, provider ProviderConfig) error {
	switch name {
	case ProviderOpenRouter, ProviderAnthropic, ProviderOpenAI, ProviderGemini:
		if strings.TrimSpace(provider.APIKey) == "" {
			return fmt.Errorf("provider %q requires apiKey", name)
		}
	case ProviderOllama, ProviderLMStudio:
		if strings.TrimSpace(provider.Model) == "" {
			return fmt.Errorf("provider %q requires model", name)
		}
	}
	return nil
}
