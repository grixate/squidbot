package plugins

import (
	"context"
	"encoding/json"
)

type Manifest struct {
	Name          string              `json:"name"`
	Version       string              `json:"version"`
	Command       string              `json:"command"`
	Args          []string            `json:"args,omitempty"`
	Capabilities  []string            `json:"capabilities,omitempty"`
	Tools         []ToolManifest      `json:"tools,omitempty"`
	Timeouts      TimeoutConfig       `json:"timeouts,omitempty"`
	ResourceLimit ResourceLimitConfig `json:"resourceLimits,omitempty"`
}

type TimeoutConfig struct {
	CallTimeoutSec int `json:"callTimeoutSec,omitempty"`
}

type ResourceLimitConfig struct {
	MaxRestarts int `json:"maxRestarts,omitempty"`
}

type ToolManifest struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Schema       map[string]any `json:"schema"`
}

type RegisteredTool struct {
	NamespacedName string
	PluginName     string
	Name           string
	Description    string
	Schema         map[string]any
}

type CallResult struct {
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Runtime interface {
	Discover(ctx context.Context) error
	Tools() []RegisteredTool
	Call(ctx context.Context, namespacedName string, args json.RawMessage) (CallResult, error)
	Close() error
}
