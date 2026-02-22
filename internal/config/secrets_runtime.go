package config

import (
	"fmt"
	"strings"

	"github.com/grixate/squidbot/internal/secrets"
)

func resolveSecretRefs(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	resolver := secrets.NewResolver()
	resolveString := func(path, ref string, target *string) error {
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		value, err := resolver.Resolve(ref)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		*target = value
		return nil
	}

	providers := []*ProviderConfig{
		&cfg.Providers.OpenRouter,
		&cfg.Providers.Anthropic,
		&cfg.Providers.OpenAI,
		&cfg.Providers.Gemini,
		&cfg.Providers.Ollama,
		&cfg.Providers.LMStudio,
	}
	providerNames := []string{"openrouter", "anthropic", "openai", "gemini", "ollama", "lmstudio"}
	for i, providerCfg := range providers {
		if err := resolveString("providers."+providerNames[i]+".apiKeyRef", providerCfg.APIKeyRef, &providerCfg.APIKey); err != nil {
			return err
		}
	}
	for providerID, providerCfg := range cfg.Providers.Registry {
		next := providerCfg
		if err := resolveString("providers.registry."+providerID+".apiKeyRef", next.APIKeyRef, &next.APIKey); err != nil {
			return err
		}
		cfg.Providers.Registry[providerID] = next
	}

	if err := resolveString("channels.telegram.tokenRef", cfg.Channels.Telegram.TokenRef, &cfg.Channels.Telegram.Token); err != nil {
		return err
	}
	for channelID, channelCfg := range cfg.Channels.Registry {
		next := channelCfg
		if err := resolveString("channels.registry."+channelID+".tokenRef", next.TokenRef, &next.Token); err != nil {
			return err
		}
		if err := resolveString("channels.registry."+channelID+".authTokenRef", next.AuthTokenRef, &next.AuthToken); err != nil {
			return err
		}
		resolvedHeaders, err := resolver.ResolveMap(next.Headers)
		if err != nil {
			return fmt.Errorf("channels.registry.%s.headers: %w", channelID, err)
		}
		next.Headers = resolvedHeaders
		resolvedMetadata, err := resolver.ResolveMap(next.Metadata)
		if err != nil {
			return fmt.Errorf("channels.registry.%s.metadata: %w", channelID, err)
		}
		next.Metadata = resolvedMetadata
		cfg.Channels.Registry[channelID] = next
	}
	for channelID, channelCfg := range cfg.Channels.Plugins {
		next := channelCfg
		if err := resolveString("channels.plugins."+channelID+".authTokenRef", next.AuthTokenRef, &next.AuthToken); err != nil {
			return err
		}
		resolvedHeaders, err := resolver.ResolveMap(next.Headers)
		if err != nil {
			return fmt.Errorf("channels.plugins.%s.headers: %w", channelID, err)
		}
		next.Headers = resolvedHeaders
		resolvedMetadata, err := resolver.ResolveMap(next.Metadata)
		if err != nil {
			return fmt.Errorf("channels.plugins.%s.metadata: %w", channelID, err)
		}
		next.Metadata = resolvedMetadata
		cfg.Channels.Plugins[channelID] = next
	}

	if err := resolveString("tools.web.search.apiKeyRef", cfg.Tools.Web.Search.APIKeyRef, &cfg.Tools.Web.Search.APIKey); err != nil {
		return err
	}
	if err := resolveString("runtime.metricsHttp.authTokenRef", cfg.Runtime.MetricsHTTP.AuthTokenRef, &cfg.Runtime.MetricsHTTP.AuthToken); err != nil {
		return err
	}
	for idx, peer := range cfg.Runtime.Federation.Peers {
		if err := resolveString(fmt.Sprintf("runtime.federation.peers[%d].authTokenRef", idx), peer.AuthTokenRef, &peer.AuthToken); err != nil {
			return err
		}
		cfg.Runtime.Federation.Peers[idx] = peer
	}
	for serverName, serverCfg := range cfg.Tools.MCP.Servers {
		next := serverCfg
		resolvedEnv, err := resolver.ResolveMap(next.Env)
		if err != nil {
			return fmt.Errorf("tools.mcp.servers.%s.env: %w", serverName, err)
		}
		next.Env = resolvedEnv
		resolvedHeaders, err := resolver.ResolveMap(next.Headers)
		if err != nil {
			return fmt.Errorf("tools.mcp.servers.%s.headers: %w", serverName, err)
		}
		next.Headers = resolvedHeaders
		cfg.Tools.MCP.Servers[serverName] = next
	}
	migrateLegacyChannels(cfg)
	return nil
}
