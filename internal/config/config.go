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
	Features  FeaturesConfig  `json:"features"`
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
	Web        WebToolsConfig        `json:"web"`
	Exec       ExecToolsConfig       `json:"exec"`
	Filesystem FilesystemToolsConfig `json:"fs"`
}

type ExecToolsConfig struct {
	Enabled         bool     `json:"enabled"`
	AllowedCommands []string `json:"allowedCommands,omitempty"`
	BlockedCommands []string `json:"blockedCommands,omitempty"`
}

type FilesystemToolsConfig struct {
	ParentWriteEnabled   bool `json:"parentWriteEnabled"`
	SubagentWriteEnabled bool `json:"subagentWriteEnabled"`
}

type FeaturesConfig struct {
	Streaming      bool `json:"streaming"`
	ChannelsWave1  bool `json:"channelsWave1"`
	SemanticMemory bool `json:"semanticMemory"`
	Plugins        bool `json:"plugins"`
	MetricsHTTP    bool `json:"metricsHttp"`
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
	MailboxSize          int                      `json:"mailboxSize"`
	ActorIdleTTL         DurationValue            `json:"actorIdleTtl"`
	HeartbeatIntervalSec int                      `json:"heartbeatIntervalSec"`
	Subagents            SubagentRuntimeConfig    `json:"subagents"`
	Federation           FederationRuntimeConfig  `json:"federation"`
	Plugins              PluginsRuntimeConfig     `json:"plugins"`
	MetricsHTTP          MetricsHTTPRuntimeConfig `json:"metricsHttp"`
	TokenSafety          TokenSafetyRuntimeConfig `json:"tokenSafety"`
}

type PluginsRuntimeConfig struct {
	Enabled           bool     `json:"enabled"`
	Paths             []string `json:"paths"`
	DefaultTimeoutSec int      `json:"defaultTimeoutSec"`
	MaxConcurrent     int      `json:"maxConcurrent"`
	MaxProcesses      int      `json:"maxProcesses"`
}

type MetricsHTTPRuntimeConfig struct {
	Enabled       bool   `json:"enabled"`
	ListenAddr    string `json:"listenAddr"`
	AuthToken     string `json:"authToken,omitempty"`
	LocalhostOnly bool   `json:"localhostOnly"`
}

type SubagentRuntimeConfig struct {
	Enabled            bool `json:"enabled"`
	MaxConcurrent      int  `json:"maxConcurrent"`
	MaxQueue           int  `json:"maxQueue"`
	DefaultTimeoutSec  int  `json:"defaultTimeoutSec"`
	MaxAttempts        int  `json:"maxAttempts"`
	RetryBackoffSec    int  `json:"retryBackoffSec"`
	MaxDepth           int  `json:"maxDepth"`
	AllowWrites        bool `json:"allowWrites"`
	NotifyOnComplete   bool `json:"notifyOnComplete"`
	ReinjectCompletion bool `json:"reinjectCompletion"`
}

type TokenSafetyRuntimeConfig struct {
	Enabled                     bool     `json:"enabled"`
	Mode                        string   `json:"mode"`
	GlobalHardLimitTokens       uint64   `json:"globalHardLimitTokens"`
	GlobalSoftThresholdPct      int      `json:"globalSoftThresholdPct"`
	SessionHardLimitTokens      uint64   `json:"sessionHardLimitTokens"`
	SessionSoftThresholdPct     int      `json:"sessionSoftThresholdPct"`
	SubagentRunHardLimitTokens  uint64   `json:"subagentRunHardLimitTokens"`
	SubagentRunSoftThresholdPct int      `json:"subagentRunSoftThresholdPct"`
	EstimateOnMissingUsage      bool     `json:"estimateOnMissingUsage"`
	EstimateCharsPerToken       int      `json:"estimateCharsPerToken"`
	TrustedWriters              []string `json:"trustedWriters"`
}

type FederationPeerConfig struct {
	ID             string   `json:"id"`
	BaseURL        string   `json:"baseUrl"`
	AuthToken      string   `json:"authToken,omitempty"`
	Enabled        bool     `json:"enabled"`
	Capabilities   []string `json:"capabilities,omitempty"`
	Roles          []string `json:"roles,omitempty"`
	Priority       int      `json:"priority"`
	MaxConcurrent  int      `json:"maxConcurrent"`
	MaxQueue       int      `json:"maxQueue"`
	HealthEndpoint string   `json:"healthEndpoint,omitempty"`
}

type FederationRuntimeConfig struct {
	Enabled           bool                   `json:"enabled"`
	NodeID            string                 `json:"nodeId"`
	ListenAddr        string                 `json:"listenAddr"`
	RequestTimeoutSec int                    `json:"requestTimeoutSec"`
	MaxRetries        int                    `json:"maxRetries"`
	RetryBackoffMs    int                    `json:"retryBackoffMs"`
	AllowFromNodeIDs  []string               `json:"allowFromNodeIDs,omitempty"`
	AutoFallback      bool                   `json:"autoFallback"`
	Peers             []FederationPeerConfig `json:"peers,omitempty"`
}

type MemoryConfig struct {
	Enabled            bool                 `json:"enabled"`
	IndexPath          string               `json:"indexPath"`
	TopK               int                  `json:"topK"`
	RecencyDays        int                  `json:"recencyDays"`
	EmbeddingsProvider string               `json:"embeddingsProvider"`
	EmbeddingsModel    string               `json:"embeddingsModel"`
	Semantic           MemorySemanticConfig `json:"semantic"`
}

type MemorySemanticConfig struct {
	Enabled        bool `json:"enabled"`
	TopKCandidates int  `json:"topKCandidates"`
	RerankTopK     int  `json:"rerankTopK"`
}

type SkillsConfig struct {
	Enabled            bool               `json:"enabled"`
	Paths              []string           `json:"paths"`
	MaxActive          int                `json:"maxActive"`
	MatchThreshold     int                `json:"matchThreshold"`
	RefreshIntervalSec int                `json:"refreshIntervalSec"`
	PromptMaxChars     int                `json:"promptMaxChars"`
	SkillMaxChars      int                `json:"skillMaxChars"`
	AllowZip           bool               `json:"allowZip"`
	CacheDir           string             `json:"cacheDir"`
	Policy             SkillsPolicyConfig `json:"policy"`
}

type SkillsPolicyConfig struct {
	Allow    []string                             `json:"allow"`
	Deny     []string                             `json:"deny"`
	Channels map[string]SkillsChannelPolicyConfig `json:"channels"`
}

type SkillsChannelPolicyConfig struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
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
			Exec: ExecToolsConfig{
				Enabled:         false,
				AllowedCommands: []string{},
				BlockedCommands: []string{"rm", "shutdown", "reboot", "mkfs", "dd"},
			},
			Filesystem: FilesystemToolsConfig{
				ParentWriteEnabled:   false,
				SubagentWriteEnabled: false,
			},
		},
		Features: FeaturesConfig{
			Streaming:      false,
			ChannelsWave1:  false,
			SemanticMemory: false,
			Plugins:        false,
			MetricsHTTP:    false,
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
			Subagents: SubagentRuntimeConfig{
				Enabled:            true,
				MaxConcurrent:      4,
				MaxQueue:           64,
				DefaultTimeoutSec:  300,
				MaxAttempts:        2,
				RetryBackoffSec:    8,
				MaxDepth:           1,
				AllowWrites:        false,
				NotifyOnComplete:   true,
				ReinjectCompletion: false,
			},
			Federation: FederationRuntimeConfig{
				Enabled:           false,
				NodeID:            "",
				ListenAddr:        "127.0.0.1:18900",
				RequestTimeoutSec: 30,
				MaxRetries:        2,
				RetryBackoffMs:    500,
				AllowFromNodeIDs:  []string{},
				AutoFallback:      true,
				Peers:             []FederationPeerConfig{},
			},
			Plugins: PluginsRuntimeConfig{
				Enabled:           false,
				Paths:             []string{filepath.Join(workspace, "plugins")},
				DefaultTimeoutSec: 60,
				MaxConcurrent:     4,
				MaxProcesses:      8,
			},
			MetricsHTTP: MetricsHTTPRuntimeConfig{
				Enabled:       false,
				ListenAddr:    "127.0.0.1:19090",
				AuthToken:     "",
				LocalhostOnly: true,
			},
			TokenSafety: TokenSafetyRuntimeConfig{
				Enabled:                     true,
				Mode:                        "hybrid",
				GlobalHardLimitTokens:       1200000,
				GlobalSoftThresholdPct:      85,
				SessionHardLimitTokens:      180000,
				SessionSoftThresholdPct:     85,
				SubagentRunHardLimitTokens:  60000,
				SubagentRunSoftThresholdPct: 85,
				EstimateOnMissingUsage:      true,
				EstimateCharsPerToken:       4,
				TrustedWriters:              []string{"cli:user"},
			},
		},
		Memory: MemoryConfig{
			Enabled:            true,
			IndexPath:          filepath.Join(home, "data", "memory_index.db"),
			TopK:               8,
			RecencyDays:        30,
			EmbeddingsProvider: "none",
			EmbeddingsModel:    "",
			Semantic: MemorySemanticConfig{
				Enabled:        false,
				TopKCandidates: 24,
				RerankTopK:     8,
			},
		},
		Skills: SkillsConfig{
			Enabled:            true,
			Paths:              []string{filepath.Join(workspace, "skills")},
			MaxActive:          3,
			MatchThreshold:     35,
			RefreshIntervalSec: 30,
			PromptMaxChars:     12000,
			SkillMaxChars:      4000,
			AllowZip:           true,
			CacheDir:           filepath.Join(workspace, ".squidbot", "skills-cache"),
			Policy: SkillsPolicyConfig{
				Allow:    []string{},
				Deny:     []string{},
				Channels: map[string]SkillsChannelPolicyConfig{},
			},
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
			normalizeDefaultChannels(&cfg)
			applyEnvOverrides(&cfg)
			normalizeSkillsConfig(&cfg)
			return cfg, nil
		}
		return cfg, err
	}
	var raw map[string]any
	_ = json.Unmarshal(bytes, &raw)
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return cfg, err
	}
	applyLegacyCompatibilityDefaults(&cfg, raw)
	migrateLegacyProviders(&cfg)
	migrateLegacyChannels(&cfg)
	normalizeDefaultChannels(&cfg)
	applyEnvOverrides(&cfg)
	normalizeSkillsConfig(&cfg)
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
	if cfg.Skills.Policy.Channels == nil {
		cfg.Skills.Policy.Channels = map[string]SkillsChannelPolicyConfig{}
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
		"SQUIDBOT_METRICS_HTTP_LISTEN_ADDR":   &cfg.Runtime.MetricsHTTP.ListenAddr,
		"SQUIDBOT_METRICS_HTTP_AUTH_TOKEN":    &cfg.Runtime.MetricsHTTP.AuthToken,
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
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEATURE_STREAMING")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Features.Streaming = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEATURE_CHANNELS_WAVE1")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Features.ChannelsWave1 = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEATURE_SEMANTIC_MEMORY")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Features.SemanticMemory = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEATURE_PLUGINS")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Features.Plugins = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEATURE_METRICS_HTTP")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Features.MetricsHTTP = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOOLS_EXEC_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Tools.Exec.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOOLS_EXEC_ALLOWED_COMMANDS")); value != "" {
		cfg.Tools.Exec.AllowedCommands = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOOLS_EXEC_BLOCKED_COMMANDS")); value != "" {
		cfg.Tools.Exec.BlockedCommands = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOOLS_FS_PARENT_WRITE_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Tools.Filesystem.ParentWriteEnabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOOLS_FS_SUBAGENT_WRITE_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Tools.Filesystem.SubagentWriteEnabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_RUNTIME_PLUGINS_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Runtime.Plugins.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_RUNTIME_PLUGINS_PATHS")); value != "" {
		cfg.Runtime.Plugins.Paths = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_RUNTIME_PLUGINS_DEFAULT_TIMEOUT_SEC")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Runtime.Plugins.DefaultTimeoutSec = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_RUNTIME_PLUGINS_MAX_CONCURRENT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Runtime.Plugins.MaxConcurrent = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_RUNTIME_PLUGINS_MAX_PROCESSES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Runtime.Plugins.MaxProcesses = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_METRICS_HTTP_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Runtime.MetricsHTTP.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_METRICS_HTTP_LOCALHOST_ONLY")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Runtime.MetricsHTTP.LocalhostOnly = parsed
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
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_MEMORY_SEMANTIC_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Memory.Semantic.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_MEMORY_SEMANTIC_TOPK_CANDIDATES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Memory.Semantic.TopKCandidates = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_MEMORY_SEMANTIC_RERANK_TOPK")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Memory.Semantic.RerankTopK = parsed
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
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Skills.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_MAX_ACTIVE")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Skills.MaxActive = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_MATCH_THRESHOLD")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Skills.MatchThreshold = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_REFRESH_INTERVAL_SEC")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Skills.RefreshIntervalSec = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_PROMPT_MAX_CHARS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Skills.PromptMaxChars = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_SKILL_MAX_CHARS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Skills.SkillMaxChars = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_ALLOW_ZIP")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Skills.AllowZip = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SKILLS_CACHE_DIR")); value != "" {
		cfg.Skills.CacheDir = value
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_ENABLED")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Runtime.Subagents.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_MAX_CONCURRENT")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			cfg.Runtime.Subagents.MaxConcurrent = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_MAX_QUEUE")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			cfg.Runtime.Subagents.MaxQueue = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_DEFAULT_TIMEOUT_SEC")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			cfg.Runtime.Subagents.DefaultTimeoutSec = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_MAX_ATTEMPTS")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			cfg.Runtime.Subagents.MaxAttempts = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_RETRY_BACKOFF_SEC")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 0 {
			cfg.Runtime.Subagents.RetryBackoffSec = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_MAX_DEPTH")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed >= 0 {
			cfg.Runtime.Subagents.MaxDepth = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_ALLOW_WRITES")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Runtime.Subagents.AllowWrites = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_NOTIFY_ON_COMPLETE")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Runtime.Subagents.NotifyOnComplete = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_SUBAGENTS_REINJECT_COMPLETION")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Runtime.Subagents.ReinjectCompletion = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_ENABLED")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Runtime.Federation.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_NODE_ID")); value != "" {
		cfg.Runtime.Federation.NodeID = value
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_LISTEN_ADDR")); value != "" {
		cfg.Runtime.Federation.ListenAddr = value
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_REQUEST_TIMEOUT_SEC")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.Runtime.Federation.RequestTimeoutSec = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_MAX_RETRIES")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			cfg.Runtime.Federation.MaxRetries = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_RETRY_BACKOFF_MS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			cfg.Runtime.Federation.RetryBackoffMs = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_ALLOW_FROM_NODE_IDS")); value != "" {
		cfg.Runtime.Federation.AllowFromNodeIDs = splitCSV(value)
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_FEDERATION_AUTO_FALLBACK")); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			cfg.Runtime.Federation.AutoFallback = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_ENABLED")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Runtime.TokenSafety.Enabled = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_MODE")); value != "" {
		cfg.Runtime.TokenSafety.Mode = strings.ToLower(value)
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_GLOBAL_HARD_LIMIT_TOKENS")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err == nil {
			cfg.Runtime.TokenSafety.GlobalHardLimitTokens = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_GLOBAL_SOFT_THRESHOLD_PCT")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			cfg.Runtime.TokenSafety.GlobalSoftThresholdPct = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_SESSION_HARD_LIMIT_TOKENS")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err == nil {
			cfg.Runtime.TokenSafety.SessionHardLimitTokens = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_SESSION_SOFT_THRESHOLD_PCT")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			cfg.Runtime.TokenSafety.SessionSoftThresholdPct = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_SUBAGENT_RUN_HARD_LIMIT_TOKENS")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err == nil {
			cfg.Runtime.TokenSafety.SubagentRunHardLimitTokens = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_SUBAGENT_RUN_SOFT_THRESHOLD_PCT")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			cfg.Runtime.TokenSafety.SubagentRunSoftThresholdPct = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_ESTIMATE_ON_MISSING_USAGE")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			cfg.Runtime.TokenSafety.EstimateOnMissingUsage = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_ESTIMATE_CHARS_PER_TOKEN")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			cfg.Runtime.TokenSafety.EstimateCharsPerToken = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("SQUIDBOT_TOKEN_SAFETY_TRUSTED_WRITERS")); value != "" {
		cfg.Runtime.TokenSafety.TrustedWriters = splitCSV(value)
	}
	applyDynamicProviderEnvOverrides(cfg)
	applyDynamicChannelEnvOverrides(cfg)
	migrateLegacyProviders(cfg)
	migrateLegacyChannels(cfg)
	normalizeDefaultChannels(cfg)
	normalizeSkillsConfig(cfg)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func normalizeSkillsConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	workspace := WorkspacePath(*cfg)
	if len(cfg.Skills.Paths) == 0 {
		cfg.Skills.Paths = []string{filepath.Join(workspace, "skills")}
	}
	if cfg.Skills.MaxActive <= 0 {
		cfg.Skills.MaxActive = 3
	}
	if cfg.Skills.MatchThreshold <= 0 {
		cfg.Skills.MatchThreshold = 35
	}
	if cfg.Skills.RefreshIntervalSec <= 0 {
		cfg.Skills.RefreshIntervalSec = 30
	}
	if cfg.Skills.PromptMaxChars <= 0 {
		cfg.Skills.PromptMaxChars = 12000
	}
	if cfg.Skills.SkillMaxChars <= 0 {
		cfg.Skills.SkillMaxChars = 4000
	}
	if strings.TrimSpace(cfg.Skills.CacheDir) == "" {
		cfg.Skills.CacheDir = filepath.Join(workspace, ".squidbot", "skills-cache")
	}
	if cfg.Skills.Policy.Channels == nil {
		cfg.Skills.Policy.Channels = map[string]SkillsChannelPolicyConfig{}
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

func normalizeDefaultChannels(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]GenericChannelConfig{}
	}
	defaults := map[string]GenericChannelConfig{
		"slack":    {Label: "Slack", Kind: "core"},
		"discord":  {Label: "Discord", Kind: "core"},
		"webchat":  {Label: "Web Chat", Kind: "core"},
		"whatsapp": {Label: "WhatsApp", Kind: "core"},
	}
	for channelID, defaultsCfg := range defaults {
		current := cfg.Channels.Registry[channelID]
		current.Label = defaultString(current.Label, defaultsCfg.Label)
		current.Kind = defaultString(current.Kind, defaultsCfg.Kind)
		cfg.Channels.Registry[channelID] = current
	}
}

func applyLegacyCompatibilityDefaults(cfg *Config, raw map[string]any) {
	if cfg == nil {
		return
	}
	// Existing configs that predate hardening retain old behavior by default.
	if !nestedPathExists(raw, "tools", "exec", "enabled") {
		cfg.Tools.Exec.Enabled = true
	}
	if !nestedPathExists(raw, "tools", "fs", "parentWriteEnabled") {
		cfg.Tools.Filesystem.ParentWriteEnabled = true
	}
	if !nestedPathExists(raw, "tools", "fs", "subagentWriteEnabled") {
		cfg.Tools.Filesystem.SubagentWriteEnabled = cfg.Runtime.Subagents.AllowWrites
	}
}

func nestedPathExists(root map[string]any, path ...string) bool {
	current := any(root)
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return false
		}
		next, exists := obj[key]
		if !exists {
			return false
		}
		current = next
	}
	return true
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
