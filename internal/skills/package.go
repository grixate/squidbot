package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/grixate/squidbot/internal/config"
)

type packageRecord struct {
	Descriptor SkillDescriptor
	Err        error
}

func loadDirPackage(path string) packageRecord {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return packageRecord{Err: err}
	}
	root := filepath.Dir(path)
	desc := parseSkillDocument(path, root, "dir", bytes)
	return packageRecord{Descriptor: desc}
}

func materializeSkill(ctx context.Context, descriptor SkillDescriptor, cache *ZipCache, workspace string, cfg config.Config) (SkillMaterialized, error) {
	_ = ctx
	_ = workspace
	if !descriptor.Valid {
		return SkillMaterialized{}, fmt.Errorf("skill %q is invalid", descriptor.Name)
	}
	body := ""
	resolved := make([]string, 0, len(descriptor.References))
	extractedPath := ""
	switch descriptor.SourceKind {
	case "dir":
		content, err := os.ReadFile(descriptor.Path)
		if err != nil {
			return SkillMaterialized{}, err
		}
		_, _, parsedBody, _, _ := parseFrontMatter(string(content))
		if strings.TrimSpace(parsedBody) == "" {
			parsedBody = string(content)
		}
		body = strings.TrimSpace(parsedBody)
		for _, ref := range descriptor.References {
			clean := strings.TrimSpace(ref)
			if clean == "" {
				continue
			}
			if filepath.IsAbs(clean) {
				resolved = append(resolved, filepath.Clean(clean))
				continue
			}
			resolved = append(resolved, filepath.Clean(filepath.Join(descriptor.RootPath, clean)))
		}
	case "zip":
		if !cfg.Skills.AllowZip {
			return SkillMaterialized{}, fmt.Errorf("zip skills disabled")
		}
		extracted, err := cache.Extract(descriptor.Path)
		if err != nil {
			return SkillMaterialized{}, err
		}
		extractedPath = extracted
		bytes, err := readSkillFromZip(descriptor.Path)
		if err != nil {
			return SkillMaterialized{}, err
		}
		_, _, parsedBody, _, _ := parseFrontMatter(string(bytes))
		if strings.TrimSpace(parsedBody) == "" {
			parsedBody = string(bytes)
		}
		body = strings.TrimSpace(parsedBody)
		for _, ref := range descriptor.References {
			clean := strings.TrimSpace(ref)
			if clean == "" {
				continue
			}
			resolved = append(resolved, filepath.Clean(filepath.Join(extracted, clean)))
		}
	default:
		return SkillMaterialized{}, fmt.Errorf("unsupported source kind %q", descriptor.SourceKind)
	}
	if len(body) > maxInt(cfg.Skills.SkillMaxChars, 1) {
		body = body[:maxInt(cfg.Skills.SkillMaxChars, 1)]
	}
	return SkillMaterialized{
		Descriptor:         descriptor,
		Body:               body,
		ResolvedReferences: resolved,
		ExtractedPath:      extractedPath,
	}, nil
}
