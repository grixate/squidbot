package contextctrl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	defaultContextWindowTokens = 8192
	defaultOutputReserveTokens = 1024
	defaultCharsPerToken       = 4.0
)

type ModelWindow struct {
	ContextWindowTokens int
	OutputReserveTokens int
	CharsPerToken       float64
	Source              string
}

type windowRegistry struct {
	Version  int                 `json:"version"`
	Defaults windowRegistryEntry `json:"defaults"`
	Models   []windowRegistryEntry
}

type windowRegistryEntry struct {
	Name                string   `json:"name"`
	Aliases             []string `json:"aliases"`
	ContextWindowTokens int      `json:"contextWindowTokens"`
	OutputReserveTokens int      `json:"outputReserveTokens"`
	CharsPerToken       float64  `json:"charsPerToken"`
}

type Registry struct {
	path     string
	fallback ModelWindow
	logf     func(format string, args ...any)

	mu       sync.Mutex
	lastMod  int64
	lastGood windowRegistry
	hasLast  bool
}

func NewRegistry(path string, defaultWindow, outputReserve int, charsPerToken float64, logf func(format string, args ...any)) *Registry {
	fallback := ModelWindow{
		ContextWindowTokens: maxInt(defaultWindow, defaultContextWindowTokens),
		OutputReserveTokens: maxInt(outputReserve, defaultOutputReserveTokens),
		CharsPerToken:       normalizeCharsPerToken(charsPerToken),
		Source:              "config_defaults",
	}
	if fallback.OutputReserveTokens >= fallback.ContextWindowTokens {
		fallback.OutputReserveTokens = maxInt(fallback.ContextWindowTokens/4, 1)
	}
	return &Registry{
		path:     strings.TrimSpace(path),
		fallback: fallback,
		logf:     logf,
	}
}

func (r *Registry) Resolve(model string) (ModelWindow, error) {
	if r == nil {
		return ModelWindow{
			ContextWindowTokens: defaultContextWindowTokens,
			OutputReserveTokens: defaultOutputReserveTokens,
			CharsPerToken:       defaultCharsPerToken,
			Source:              "default",
		}, nil
	}
	if strings.TrimSpace(r.path) == "" {
		return r.fallback, nil
	}

	cleanPath := r.path
	if !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Clean(cleanPath)
	}

	stat, statErr := os.Stat(cleanPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return r.fallback, nil
		}
		r.logf("contextctrl registry stat failed path=%s err=%v", cleanPath, statErr)
		return r.fallback, statErr
	}

	mod := stat.ModTime().UTC().UnixNano()
	r.mu.Lock()
	if r.hasLast && r.lastMod == mod {
		loaded := r.lastGood
		r.mu.Unlock()
		return r.resolveFromRegistry(model, loaded), nil
	}
	r.mu.Unlock()

	bytes, readErr := os.ReadFile(cleanPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return r.fallback, nil
		}
		r.logf("contextctrl registry read failed path=%s err=%v", cleanPath, readErr)
		r.mu.Lock()
		if r.hasLast {
			loaded := r.lastGood
			r.mu.Unlock()
			return r.resolveFromRegistry(model, loaded), readErr
		}
		r.mu.Unlock()
		return r.fallback, readErr
	}

	var parsed windowRegistry
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		parseErr := fmt.Errorf("parse model windows registry: %w", err)
		r.logf("contextctrl registry parse failed path=%s err=%v", cleanPath, parseErr)
		r.mu.Lock()
		if r.hasLast {
			loaded := r.lastGood
			r.mu.Unlock()
			return r.resolveFromRegistry(model, loaded), parseErr
		}
		r.mu.Unlock()
		return r.fallback, parseErr
	}
	parsed = normalizeRegistry(parsed)
	r.mu.Lock()
	r.lastGood = parsed
	r.lastMod = mod
	r.hasLast = true
	r.mu.Unlock()
	return r.resolveFromRegistry(model, parsed), nil
}

func normalizeRegistry(reg windowRegistry) windowRegistry {
	reg.Models = append([]windowRegistryEntry(nil), reg.Models...)
	reg.Defaults = normalizeRegistryEntry(reg.Defaults)
	for i := range reg.Models {
		reg.Models[i] = normalizeRegistryEntry(reg.Models[i])
	}
	return reg
}

func normalizeRegistryEntry(entry windowRegistryEntry) windowRegistryEntry {
	entry.Name = strings.TrimSpace(entry.Name)
	if entry.ContextWindowTokens < 0 {
		entry.ContextWindowTokens = 0
	}
	if entry.OutputReserveTokens < 0 {
		entry.OutputReserveTokens = 0
	}
	if entry.CharsPerToken < 0 {
		entry.CharsPerToken = 0
	}
	outAliases := make([]string, 0, len(entry.Aliases))
	for _, alias := range entry.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		outAliases = append(outAliases, alias)
	}
	entry.Aliases = outAliases
	return entry
}

func (r *Registry) resolveFromRegistry(model string, reg windowRegistry) ModelWindow {
	needle := strings.ToLower(strings.TrimSpace(model))
	entry, source := findRegistryEntry(reg, needle)
	if source == "" {
		return r.fallback
	}

	out := ModelWindow{
		ContextWindowTokens: firstPositiveInt(entry.ContextWindowTokens, reg.Defaults.ContextWindowTokens, r.fallback.ContextWindowTokens),
		OutputReserveTokens: firstPositiveInt(entry.OutputReserveTokens, reg.Defaults.OutputReserveTokens, r.fallback.OutputReserveTokens),
		CharsPerToken:       firstPositiveFloat(entry.CharsPerToken, reg.Defaults.CharsPerToken, r.fallback.CharsPerToken),
		Source:              source,
	}
	if out.OutputReserveTokens >= out.ContextWindowTokens {
		out.OutputReserveTokens = maxInt(out.ContextWindowTokens/4, 1)
	}
	return out
}

func findRegistryEntry(reg windowRegistry, model string) (windowRegistryEntry, string) {
	if model == "" {
		if reg.Defaults.ContextWindowTokens > 0 || reg.Defaults.OutputReserveTokens > 0 || reg.Defaults.CharsPerToken > 0 {
			return reg.Defaults, "registry:defaults"
		}
		return windowRegistryEntry{}, ""
	}
	for _, entry := range reg.Models {
		if strings.EqualFold(strings.TrimSpace(entry.Name), model) {
			return entry, "registry:model"
		}
	}
	for _, entry := range reg.Models {
		for _, alias := range entry.Aliases {
			if strings.EqualFold(strings.TrimSpace(alias), model) {
				return entry, "registry:model_alias"
			}
		}
	}
	if reg.Defaults.ContextWindowTokens > 0 || reg.Defaults.OutputReserveTokens > 0 || reg.Defaults.CharsPerToken > 0 {
		return reg.Defaults, "registry:defaults"
	}
	return windowRegistryEntry{}, ""
}

func firstPositiveInt(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstPositiveFloat(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return normalizeCharsPerToken(v)
		}
	}
	return defaultCharsPerToken
}

func maxInt(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}
