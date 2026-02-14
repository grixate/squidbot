package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Provider() string
	Model() string
}

type noopEmbedder struct{}

func (noopEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

func (noopEmbedder) Provider() string { return "none" }
func (noopEmbedder) Model() string    { return "" }

type openAIEmbedder struct {
	provider string
	apiKey   string
	apiBase  string
	model    string
	client   *http.Client
}

func NewEmbedder(cfg config.Config) Embedder {
	provider := strings.ToLower(strings.TrimSpace(cfg.Memory.EmbeddingsProvider))
	switch provider {
	case "openai", "gemini", "ollama":
		providerCfg, _ := cfg.ProviderByName(provider)
		apiBase := strings.TrimSpace(providerCfg.APIBase)
		if apiBase == "" {
			apiBase = config.ProviderDefaultAPIBase(provider)
		}
		if apiBase == "" {
			return noopEmbedder{}
		}
		model := strings.TrimSpace(cfg.Memory.EmbeddingsModel)
		if model == "" {
			model = strings.TrimSpace(providerCfg.Model)
		}
		if model == "" {
			model = "text-embedding-3-small"
		}
		return &openAIEmbedder{
			provider: provider,
			apiKey:   strings.TrimSpace(providerCfg.APIKey),
			apiBase:  strings.TrimRight(apiBase, "/"),
			model:    model,
			client:   &http.Client{Timeout: 30 * time.Second},
		}
	default:
		return noopEmbedder{}
	}
}

func (e *openAIEmbedder) Provider() string { return e.provider }
func (e *openAIEmbedder) Model() string    { return e.model }

func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(e.apiBase) == "" {
		return make([][]float32, len(texts)), nil
	}
	payload := map[string]any{
		"model": e.model,
		"input": texts,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.apiBase+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(e.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var raw map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&raw)
		return nil, fmt.Errorf("embedding provider http %d: %v", resp.StatusCode, raw)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(out) {
			continue
		}
		out[item.Index] = item.Embedding
	}
	return out, nil
}
