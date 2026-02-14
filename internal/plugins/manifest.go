package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var allowedCapabilities = map[string]struct{}{
	"filesystem": {},
	"network":    {},
	"exec":       {},
	"channels":   {},
}

type discoveredManifest struct {
	Manifest
	Path string
}

func discoverManifests(paths []string) ([]discoveredManifest, error) {
	seen := map[string]struct{}{}
	out := make([]discoveredManifest, 0)
	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		entries, err := findManifestFiles(root)
		if err != nil {
			return nil, err
		}
		for _, manifestPath := range entries {
			if _, ok := seen[manifestPath]; ok {
				continue
			}
			seen[manifestPath] = struct{}{}
			manifest, err := readManifest(manifestPath)
			if err != nil {
				return nil, err
			}
			out = append(out, discoveredManifest{Manifest: manifest, Path: manifestPath})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func findManifestFiles(root string) ([]string, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		if strings.EqualFold(filepath.Base(root), "plugin.json") {
			return []string{root}, nil
		}
		return nil, nil
	}
	matches := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "plugin.json") {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid plugin manifest %s: %w", path, err)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid plugin manifest %s: %w", path, err)
	}
	return manifest, nil
}

func validateManifest(manifest Manifest) error {
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(manifest.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if len(manifest.Tools) == 0 {
		return fmt.Errorf("at least one tool is required")
	}
	for _, capability := range manifest.Capabilities {
		capability = strings.TrimSpace(strings.ToLower(capability))
		if capability == "" {
			continue
		}
		if _, ok := allowedCapabilities[capability]; !ok {
			return fmt.Errorf("unsupported capability %q", capability)
		}
	}
	seenTools := map[string]struct{}{}
	pluginCaps := map[string]struct{}{}
	for _, capability := range manifest.Capabilities {
		pluginCaps[strings.TrimSpace(strings.ToLower(capability))] = struct{}{}
	}
	for _, tool := range manifest.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return fmt.Errorf("tool name is required")
		}
		if _, ok := seenTools[name]; ok {
			return fmt.Errorf("duplicate tool %q", name)
		}
		seenTools[name] = struct{}{}
		if len(tool.Schema) == 0 {
			return fmt.Errorf("tool %q schema is required", name)
		}
		for _, capability := range tool.Capabilities {
			capability = strings.TrimSpace(strings.ToLower(capability))
			if capability == "" {
				continue
			}
			if _, ok := allowedCapabilities[capability]; !ok {
				return fmt.Errorf("tool %q has unsupported capability %q", name, capability)
			}
			if _, ok := pluginCaps[capability]; !ok {
				return fmt.Errorf("tool %q requires capability %q not granted by plugin", name, capability)
			}
		}
	}
	return nil
}
