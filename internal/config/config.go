package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/catalog"
)

type Config struct {
	Agents    AgentsConfig    `json:"agents"`
	Providers ProvidersConfig `json:"providers"`
	Channels  ChannelsConfig  `json:"channels"`
	Tools     ToolsConfig     `json:"tools"`
	Gateway   GatewayConfig   `json:"gateway"`
	Auth      AuthConfig      `json:"auth"`
	Storage   StorageConfig   `json:"storage"`
	Runtime   RuntimeConfig   `json:"runtime"`
	Memory    MemoryConfig    `json:"memory"`
	Skills    SkillsConfig    `json:"skills"`
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
	Active     string                    `json:"active"`
	Registry   map[string]ProviderConfig `json:"registry,omitempty"`
	OpenRouter ProviderConfig            `json:"openrouter"`
	Anthropic  ProviderConfig            `json:"anthropic"`
	OpenAI     ProviderConfig            `json:"openai"`
	Gemini     ProviderConfig            `json:"gemini"`
	Ollama     ProviderConfig            `json:"ollama"`
	LMStudio   ProviderConfig            `json:"lmstudio"`
}

type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	APIBase string `json:"apiBase,omitempty"`
	Model   string `json:"model,omitempty"`
}

type ChannelsConfig struct {
	Telegram  TelegramConfig                  `json:"telegram"`
	Registry  map[string]GenericChannelConfig `json:"registry,omitempty"`
	Plugins   map[string]PluginChannelConfig  `json:"plugins,omitempty"`
	Scaffolds map[string]GenericChannelConfig `json:"scaffolds,omitempty"`
}

type TelegramConfig struct {
	Enabled   bool     `json:"enabled"`
	Token     string   `json:"token"`
	AllowFrom []string `json:"allowFrom"`
}

type GenericChannelConfig struct {
	Label     string            `json:"label,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Enabled   bool              `json:"enabled"`
	Token     string            `json:"token,omitempty"`
	AllowFrom []string          `json:"allowFrom,omitempty"`
	Endpoint  string            `json:"endpoint,omitempty"`
	AuthToken string            `json:"authToken,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type PluginChannelConfig struct {
	Enabled   bool              `json:"enabled"`
	Endpoint  string            `json:"endpoint,omitempty"`
	AuthToken string            `json:"authToken,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
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

func init() {
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(supportedProviders)+len(catalog.OpenClawProviders))
	for _, id := range supportedProviders {
		id = strings.TrimSpace(strings.ToLower(id))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	for _, profile := range catalog.OpenClawProviders {
		id := strings.TrimSpace(strings.ToLower(profile.ID))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	sort.Strings(merged)
	supportedProviders = merged
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
			Registry:  map[string]GenericChannelConfig{},
			Plugins:   map[string]PluginChannelConfig{},
			Scaffolds: map[string]GenericChannelConfig{},
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
	migrateLegacyProviders(&cfg)
	migrateLegacyChannels(&cfg)
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
	if cfg.Providers.Registry == nil {
		cfg.Providers.Registry = map[string]ProviderConfig{}
	}
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]GenericChannelConfig{}
	}
	if cfg.Channels.Plugins == nil {
		cfg.Channels.Plugins = map[string]PluginChannelConfig{}
	}
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
	applyDynamicProviderEnvOverrides(cfg)
	applyDynamicChannelEnvOverrides(cfg)
	migrateLegacyProviders(cfg)
	migrateLegacyChannels(cfg)
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
	for _, providerID := range SupportedProviders() {
		if p, ok := c.Providers.Registry[providerID]; ok {
			if hasProviderCredentials(providerID, p) {
				return providerID, p
			}
		}
	}
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

func SupportedChannels() []string {
	out := make([]string, 0, len(catalog.OpenClawChannels))
	for _, item := range catalog.OpenClawChannels {
		out = append(out, item.ID)
	}
	sort.Strings(out)
	return out
}

func ChannelProfile(name string) (catalog.ChannelProfile, bool) {
	return catalog.ChannelByID(strings.ToLower(strings.TrimSpace(name)))
}

func NormalizeProviderName(name string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		return "", false
	}
	for _, item := range supportedProviders {
		if trimmed == item {
			return trimmed, true
		}
	}
	if strings.HasPrefix(trimmed, "custom-") || strings.HasPrefix(trimmed, "custom:") {
		return trimmed, true
	}
	return "", false
}

func (c Config) ProviderByName(name string) (ProviderConfig, bool) {
	normalized, ok := NormalizeProviderName(name)
	if !ok {
		return ProviderConfig{}, false
	}
	if p, exists := c.Providers.Registry[normalized]; exists {
		return p, true
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
	if c.Providers.Registry == nil {
		c.Providers.Registry = map[string]ProviderConfig{}
	}
	c.Providers.Registry[normalized] = provider
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
		return true
	}
	return true
}

func ProviderDefaultAPIBase(name string) string {
	normalized, ok := NormalizeProviderName(name)
	if !ok {
		return ""
	}
	if profile, exists := catalog.ProviderByID(normalized); exists {
		return strings.TrimSpace(profile.DefaultAPIBase)
	}
	return ""
}

func ProviderDefaultModel(name string) string {
	normalized, ok := NormalizeProviderName(name)
	if !ok {
		return ""
	}
	if profile, exists := catalog.ProviderByID(normalized); exists {
		return strings.TrimSpace(profile.DefaultModel)
	}
	return ""
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
	if profile, exists := catalog.ProviderByID(normalized); exists {
		return profile.RequiresAPIKey, profile.RequiresModel, true
	}
	if strings.HasPrefix(normalized, "custom-") || strings.HasPrefix(normalized, "custom:") {
		return false, false, true
	}
	return false, false, false
}

func validateProviderConfig(name string, provider ProviderConfig) error {
	requiresAPIKey, requiresModel, ok := ProviderRequirements(name)
	if !ok {
		return fmt.Errorf("unsupported provider %q", name)
	}
	if requiresAPIKey && strings.TrimSpace(provider.APIKey) == "" {
		return fmt.Errorf("provider %q requires apiKey", name)
	}
	if requiresModel && strings.TrimSpace(provider.Model) == "" {
		return fmt.Errorf("provider %q requires model", name)
	}
	return nil
}

func hasProviderCredentials(providerID string, provider ProviderConfig) bool {
	requiresAPIKey, requiresModel, ok := ProviderRequirements(providerID)
	if !ok {
		return false
	}
	if requiresAPIKey {
		return strings.TrimSpace(provider.APIKey) != ""
	}
	if requiresModel {
		return strings.TrimSpace(provider.Model) != ""
	}
	return strings.TrimSpace(provider.APIKey) != "" || strings.TrimSpace(provider.Model) != "" || strings.TrimSpace(provider.APIBase) != ""
}

func migrateLegacyProviders(cfg *Config) {
	if cfg.Providers.Registry == nil {
		cfg.Providers.Registry = map[string]ProviderConfig{}
	}
	if _, ok := cfg.Providers.Registry[ProviderOpenRouter]; !ok && !isEmptyProvider(cfg.Providers.OpenRouter) {
		cfg.Providers.Registry[ProviderOpenRouter] = cfg.Providers.OpenRouter
	}
	if _, ok := cfg.Providers.Registry[ProviderAnthropic]; !ok && !isEmptyProvider(cfg.Providers.Anthropic) {
		cfg.Providers.Registry[ProviderAnthropic] = cfg.Providers.Anthropic
	}
	if _, ok := cfg.Providers.Registry[ProviderOpenAI]; !ok && !isEmptyProvider(cfg.Providers.OpenAI) {
		cfg.Providers.Registry[ProviderOpenAI] = cfg.Providers.OpenAI
	}
	if _, ok := cfg.Providers.Registry[ProviderGemini]; !ok && !isEmptyProvider(cfg.Providers.Gemini) {
		cfg.Providers.Registry[ProviderGemini] = cfg.Providers.Gemini
	}
	if _, ok := cfg.Providers.Registry[ProviderOllama]; !ok && !isEmptyProvider(cfg.Providers.Ollama) {
		cfg.Providers.Registry[ProviderOllama] = cfg.Providers.Ollama
	}
	if _, ok := cfg.Providers.Registry[ProviderLMStudio]; !ok && !isEmptyProvider(cfg.Providers.LMStudio) {
		cfg.Providers.Registry[ProviderLMStudio] = cfg.Providers.LMStudio
	}
}

func migrateLegacyChannels(cfg *Config) {
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]GenericChannelConfig{}
	}
	if cfg.Channels.Plugins == nil {
		cfg.Channels.Plugins = map[string]PluginChannelConfig{}
	}
	legacy := GenericChannelConfig{
		Label:     "Telegram",
		Kind:      "core",
		Enabled:   cfg.Channels.Telegram.Enabled,
		Token:     strings.TrimSpace(cfg.Channels.Telegram.Token),
		AllowFrom: normalizeAllowFrom(cfg.Channels.Telegram.AllowFrom),
	}
	current := cfg.Channels.Registry["telegram"]
	if strings.TrimSpace(current.Token) == "" && strings.TrimSpace(legacy.Token) != "" {
		current.Token = legacy.Token
	}
	if len(current.AllowFrom) == 0 && len(legacy.AllowFrom) > 0 {
		current.AllowFrom = legacy.AllowFrom
	}
	current.Label = defaultString(current.Label, legacy.Label)
	current.Kind = defaultString(current.Kind, legacy.Kind)
	current.Enabled = current.Enabled || legacy.Enabled
	cfg.Channels.Registry["telegram"] = current

	telegram := cfg.Channels.Registry["telegram"]
	cfg.Channels.Telegram = TelegramConfig{
		Enabled:   telegram.Enabled,
		Token:     strings.TrimSpace(telegram.Token),
		AllowFrom: normalizeAllowFrom(telegram.AllowFrom),
	}
}

func applyDynamicProviderEnvOverrides(cfg *Config) {
	const prefix = "SQUIDBOT_PROVIDER_"
	for _, envEntry := range os.Environ() {
		parts := strings.SplitN(envEntry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := strings.TrimSpace(parts[1])
		if value == "" || !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		field := ""
		switch {
		case strings.HasSuffix(rest, "_API_KEY"):
			field = "api_key"
			rest = strings.TrimSuffix(rest, "_API_KEY")
		case strings.HasSuffix(rest, "_API_BASE"):
			field = "api_base"
			rest = strings.TrimSuffix(rest, "_API_BASE")
		case strings.HasSuffix(rest, "_MODEL"):
			field = "model"
			rest = strings.TrimSuffix(rest, "_MODEL")
		default:
			continue
		}
		providerID := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rest), "_", "-"))
		if providerID == "" {
			continue
		}
		if cfg.Providers.Registry == nil {
			cfg.Providers.Registry = map[string]ProviderConfig{}
		}
		current := cfg.Providers.Registry[providerID]
		switch field {
		case "api_key":
			current.APIKey = value
		case "api_base":
			current.APIBase = value
		case "model":
			current.Model = value
		}
		cfg.Providers.Registry[providerID] = current
	}
}

func applyDynamicChannelEnvOverrides(cfg *Config) {
	const prefix = "SQUIDBOT_CHANNEL_"
	for _, envEntry := range os.Environ() {
		parts := strings.SplitN(envEntry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := strings.TrimSpace(parts[1])
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		field := ""
		switch {
		case strings.HasSuffix(rest, "_ENABLED"):
			field = "enabled"
			rest = strings.TrimSuffix(rest, "_ENABLED")
		case strings.HasSuffix(rest, "_TOKEN"):
			field = "token"
			rest = strings.TrimSuffix(rest, "_TOKEN")
		case strings.HasSuffix(rest, "_ENDPOINT"):
			field = "endpoint"
			rest = strings.TrimSuffix(rest, "_ENDPOINT")
		case strings.HasSuffix(rest, "_AUTH_TOKEN"):
			field = "auth_token"
			rest = strings.TrimSuffix(rest, "_AUTH_TOKEN")
		default:
			continue
		}
		channelID := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rest), "_", "-"))
		if channelID == "" {
			continue
		}
		if cfg.Channels.Registry == nil {
			cfg.Channels.Registry = map[string]GenericChannelConfig{}
		}
		current := cfg.Channels.Registry[channelID]
		switch field {
		case "enabled":
			enabled, err := strconv.ParseBool(value)
			if err == nil {
				current.Enabled = enabled
			}
		case "token":
			current.Token = value
		case "endpoint":
			current.Endpoint = value
		case "auth_token":
			current.AuthToken = value
		}
		cfg.Channels.Registry[channelID] = current
	}
}

func isEmptyProvider(provider ProviderConfig) bool {
	return strings.TrimSpace(provider.APIKey) == "" && strings.TrimSpace(provider.APIBase) == "" && strings.TrimSpace(provider.Model) == ""
}
