package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MigrationResult struct {
	UpdatedSecrets int
	ConfigPath     string
	OutputDir      string
}

func MigratePlaintextSecrets(path, outputDir string) (MigrationResult, error) {
	if strings.TrimSpace(path) == "" {
		path = ConfigPath()
	}
	path = expandPath(path)
	if strings.TrimSpace(outputDir) == "" {
		outputDir = "/etc/squidbot/credentials"
	}
	outputDir = expandPath(outputDir)

	raw, err := os.ReadFile(path)
	if err != nil {
		return MigrationResult{}, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return MigrationResult{}, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return MigrationResult{}, err
	}
	if err := os.Chmod(outputDir, 0o700); err != nil {
		return MigrationResult{}, err
	}

	updated := 0
	writeSecret := func(name, value string) (string, error) {
		clean := strings.TrimSpace(value)
		if clean == "" {
			return "", nil
		}
		secretPath := filepath.Join(outputDir, name)
		if err := os.WriteFile(secretPath, []byte(clean+"\n"), 0o400); err != nil {
			return "", err
		}
		if err := os.Chmod(secretPath, 0o400); err != nil {
			return "", err
		}
		updated++
		return "file:" + secretPath, nil
	}

	migrateObjectSecret := func(parent map[string]any, valueKey, refKey, filename string) error {
		if parent == nil {
			return nil
		}
		value, ok := parent[valueKey].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil
		}
		ref, err := writeSecret(filename, value)
		if err != nil {
			return err
		}
		parent[refKey] = ref
		delete(parent, valueKey)
		return nil
	}

	providers := asMap(root["providers"])
	if providers != nil {
		for _, item := range []struct {
			ID   string
			File string
		}{
			{ID: "openrouter", File: "provider-openrouter-api-key"},
			{ID: "anthropic", File: "provider-anthropic-api-key"},
			{ID: "openai", File: "provider-openai-api-key"},
			{ID: "gemini", File: "provider-gemini-api-key"},
			{ID: "ollama", File: "provider-ollama-api-key"},
			{ID: "lmstudio", File: "provider-lmstudio-api-key"},
		} {
			if err := migrateObjectSecret(asMap(providers[item.ID]), "apiKey", "apiKeyRef", item.File); err != nil {
				return MigrationResult{}, err
			}
		}
		if registry := asMap(providers["registry"]); registry != nil {
			for providerID, rawProvider := range registry {
				if err := migrateObjectSecret(asMap(rawProvider), "apiKey", "apiKeyRef", "provider-"+safeID(providerID)+"-api-key"); err != nil {
					return MigrationResult{}, err
				}
			}
		}
	}

	channels := asMap(root["channels"])
	if channels != nil {
		if err := migrateObjectSecret(asMap(channels["telegram"]), "token", "tokenRef", "channel-telegram-token"); err != nil {
			return MigrationResult{}, err
		}
		if registry := asMap(channels["registry"]); registry != nil {
			for channelID, rawChannel := range registry {
				channel := asMap(rawChannel)
				if err := migrateObjectSecret(channel, "token", "tokenRef", "channel-"+safeID(channelID)+"-token"); err != nil {
					return MigrationResult{}, err
				}
				if err := migrateObjectSecret(channel, "authToken", "authTokenRef", "channel-"+safeID(channelID)+"-auth-token"); err != nil {
					return MigrationResult{}, err
				}
			}
		}
		if plugins := asMap(channels["plugins"]); plugins != nil {
			for channelID, rawChannel := range plugins {
				if err := migrateObjectSecret(asMap(rawChannel), "authToken", "authTokenRef", "channel-plugin-"+safeID(channelID)+"-auth-token"); err != nil {
					return MigrationResult{}, err
				}
			}
		}
	}

	tools := asMap(root["tools"])
	if tools != nil {
		if err := migrateObjectSecret(asMap(asMap(tools["web"])["search"]), "apiKey", "apiKeyRef", "tools-web-search-api-key"); err != nil {
			return MigrationResult{}, err
		}
	}
	runtimeObj := asMap(root["runtime"])
	if runtimeObj != nil {
		if err := migrateObjectSecret(asMap(runtimeObj["metricsHttp"]), "authToken", "authTokenRef", "runtime-metrics-auth-token"); err != nil {
			return MigrationResult{}, err
		}
		federation := asMap(runtimeObj["federation"])
		if federation != nil {
			if peers, ok := federation["peers"].([]any); ok {
				for idx, peer := range peers {
					peerMap := asMap(peer)
					if peerMap == nil {
						continue
					}
					peerID, _ := peerMap["id"].(string)
					name := "runtime-federation-peer-" + safeID(defaultString(peerID, fmt.Sprintf("%d", idx))) + "-auth-token"
					if err := migrateObjectSecret(peerMap, "authToken", "authTokenRef", name); err != nil {
						return MigrationResult{}, err
					}
					peers[idx] = peerMap
				}
				federation["peers"] = peers
			}
		}
	}

	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return MigrationResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return MigrationResult{}, err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return MigrationResult{}, err
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return MigrationResult{}, err
	}
	return MigrationResult{UpdatedSecrets: updated, ConfigPath: path, OutputDir: outputDir}, nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func safeID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "-", "_", "-", " ", "-", ".", "-", ":", "-", "\\", "-")
	return replacer.Replace(id)
}
