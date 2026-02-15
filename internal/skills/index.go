package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

func discoverIndex(ctx context.Context, workspace string, cfg config.Config, cache *ZipCache) IndexSnapshot {
	_ = ctx
	roots := resolveRoots(workspace, cfg.Skills.Paths)
	snapshot := IndexSnapshot{
		GeneratedAt: time.Now().UTC(),
		Skills:      []SkillDescriptor{},
		Warnings:    []string{},
	}
	seenIDPath := map[string]string{}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			snapshot.Warnings = append(snapshot.Warnings, "skills: failed to stat "+root+": "+err.Error())
			continue
		}
		if !info.IsDir() {
			if strings.EqualFold(filepath.Base(root), "SKILL.md") {
				rec := loadDirPackage(root)
				if rec.Err != nil {
					snapshot.Warnings = append(snapshot.Warnings, "skills: failed to read "+root+": "+rec.Err.Error())
					continue
				}
				snapshot.Skills = append(snapshot.Skills, rec.Descriptor)
			}
			if cfg.Skills.AllowZip && strings.HasSuffix(strings.ToLower(root), ".zip") {
				desc := parseZipSkill(root)
				snapshot.Skills = append(snapshot.Skills, desc)
			}
			continue
		}

		walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				snapshot.Warnings = append(snapshot.Warnings, "skills: failed to read "+path+": "+walkErr.Error())
				return nil
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			switch {
			case strings.EqualFold(name, "SKILL.md"):
				rec := loadDirPackage(path)
				if rec.Err != nil {
					snapshot.Warnings = append(snapshot.Warnings, "skills: failed to read "+path+": "+rec.Err.Error())
					return nil
				}
				snapshot.Skills = append(snapshot.Skills, rec.Descriptor)
			case cfg.Skills.AllowZip && strings.HasSuffix(strings.ToLower(name), ".zip"):
				desc := parseZipSkill(path)
				snapshot.Skills = append(snapshot.Skills, desc)
			}
			return nil
		})
		if walkErr != nil {
			snapshot.Warnings = append(snapshot.Warnings, "skills: failed to walk "+root+": "+walkErr.Error())
		}
	}

	sortDescriptors(snapshot.Skills)
	for i := range snapshot.Skills {
		desc := &snapshot.Skills[i]
		if !desc.Valid {
			continue
		}
		if firstPath, ok := seenIDPath[desc.ID]; ok {
			desc.Valid = false
			desc.Errors = append(desc.Errors, fmt.Sprintf("duplicate skill id %q (first: %s)", desc.ID, firstPath))
			continue
		}
		seenIDPath[desc.ID] = desc.Path
	}
	return snapshot
}

func parseZipSkill(path string) SkillDescriptor {
	bytes, err := readSkillFromZip(path)
	if err != nil {
		return SkillDescriptor{
			ID:          normalizeID(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
			Name:        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			Description: "Invalid zip skill package",
			Path:        path,
			RootPath:    path,
			SourceKind:  "zip",
			Valid:       false,
			Errors:      []string{err.Error()},
			Summary:     "Invalid zip skill package",
			Tags:        []string{},
			Aliases:     []string{},
			Examples:    []string{},
			References:  []string{},
			Tools:       []string{},
			Extra:       map[string]any{},
		}
	}
	desc := parseSkillDocument(path, path, "zip", bytes)
	if desc.Name == "" {
		desc.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		desc.ID = normalizeID(desc.Name)
	}
	if desc.Description == "" {
		desc.Description = desc.Summary
	}
	return desc
}

func resolveRoots(workspace string, configured []string) []string {
	paths := configured
	if len(paths) == 0 {
		paths = []string{filepath.Join(workspace, "skills")}
	}

	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = expandPath(path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
