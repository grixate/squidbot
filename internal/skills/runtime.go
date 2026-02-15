package skills

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

type Runtime interface {
	Discover(ctx context.Context) error
	Activate(ctx context.Context, req ActivationRequest) (ActivationResult, error)
	Reload(ctx context.Context) (IndexSnapshot, error)
	Snapshot() IndexSnapshot
}

type ActivationRequest struct {
	Query            string
	Channel          string
	SessionID        string
	ExplicitMentions []string
	IsSubagent       bool
}

type ActivationDiagnostics struct {
	QueryHash   string
	Matched     int
	Activated   int
	Skipped     int
	Explicit    []string
	InvalidSeen int
	Ranked      []SkillRanked
}

type ActivationResult struct {
	Activated   []SkillActivation
	Skipped     []SkillSkip
	Warnings    []string
	Errors      []string
	Diagnostics ActivationDiagnostics
}

type SkillActivation struct {
	Skill       SkillMaterialized
	Score       int
	Reason      string
	Explicit    bool
	MatchedBy   []string
	PolicyScope string
	Breakdown   ScoreBreakdown
}

type SkillSkip struct {
	ID     string
	Name   string
	Reason string
	Score  int
}

type ScoreBreakdown struct {
	ExplicitBonus    int
	IDBonus          int
	NameBonus        int
	AliasBonus       int
	TagHits          int
	TagBonus         int
	DescriptionHits  int
	DescriptionBonus int
	ExampleHits      int
	ExampleBonus     int
	Total            int
}

type SkillRanked struct {
	ID        string
	Name      string
	Score     int
	Explicit  bool
	MatchedBy []string
	Status    string
	Reason    string
	Breakdown ScoreBreakdown
}

type SkillDescriptor struct {
	ID          string
	Name        string
	Description string
	Tags        []string
	Aliases     []string
	Examples    []string
	References  []string
	Tools       []string
	Version     string
	Path        string
	RootPath    string
	SourceKind  string
	Valid       bool
	Errors      []string
	Summary     string
	Extra       map[string]any
}

type SkillMaterialized struct {
	Descriptor         SkillDescriptor
	Body               string
	ResolvedReferences []string
	ExtractedPath      string
}

type IndexSnapshot struct {
	GeneratedAt time.Time
	Skills      []SkillDescriptor
	Warnings    []string
}

type Manager struct {
	cfg       config.Config
	workspace string
	logger    *log.Logger
	cache     *ZipCache

	mu            sync.RWMutex
	snapshot      IndexSnapshot
	lastRefresh   time.Time
	refreshPeriod time.Duration
}

func NewManager(cfg config.Config, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.Default()
	}
	workspace := config.WorkspacePath(cfg)
	refresh := time.Duration(maxInt(cfg.Skills.RefreshIntervalSec, 30)) * time.Second
	cacheDir := strings.TrimSpace(cfg.Skills.CacheDir)
	if cacheDir == "" {
		cacheDir = filepath.Join(workspace, ".squidbot", "skills-cache")
	}
	if !filepath.IsAbs(cacheDir) {
		cacheDir = filepath.Join(workspace, cacheDir)
	}
	return &Manager{
		cfg:           cfg,
		workspace:     workspace,
		logger:        logger,
		cache:         NewZipCache(filepath.Clean(cacheDir)),
		refreshPeriod: refresh,
		snapshot:      IndexSnapshot{GeneratedAt: time.Time{}, Skills: []SkillDescriptor{}, Warnings: []string{}},
	}
}

func (m *Manager) Discover(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if !m.cfg.Skills.Enabled {
		m.mu.Lock()
		m.snapshot = IndexSnapshot{GeneratedAt: time.Now().UTC(), Skills: []SkillDescriptor{}, Warnings: []string{"skills disabled"}}
		m.lastRefresh = time.Now().UTC()
		m.mu.Unlock()
		return nil
	}
	result := discoverIndex(ctx, m.workspace, m.cfg, m.cache)
	m.mu.Lock()
	m.snapshot = result
	m.lastRefresh = time.Now().UTC()
	m.mu.Unlock()
	return nil
}

func (m *Manager) Reload(ctx context.Context) (IndexSnapshot, error) {
	if err := m.Discover(ctx); err != nil {
		return IndexSnapshot{}, err
	}
	return m.Snapshot(), nil
}

func (m *Manager) Snapshot() IndexSnapshot {
	if m == nil {
		return IndexSnapshot{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := IndexSnapshot{
		GeneratedAt: m.snapshot.GeneratedAt,
		Warnings:    append([]string(nil), m.snapshot.Warnings...),
		Skills:      make([]SkillDescriptor, 0, len(m.snapshot.Skills)),
	}
	for _, skill := range m.snapshot.Skills {
		out.Skills = append(out.Skills, cloneDescriptor(skill))
	}
	return out
}

func (m *Manager) ensureFresh(ctx context.Context) error {
	if m == nil || !m.cfg.Skills.Enabled {
		return nil
	}
	m.mu.RLock()
	stale := m.lastRefresh.IsZero() || time.Since(m.lastRefresh) >= m.refreshPeriod
	m.mu.RUnlock()
	if stale {
		return m.Discover(ctx)
	}
	return nil
}

func (m *Manager) Activate(ctx context.Context, req ActivationRequest) (ActivationResult, error) {
	if m == nil || !m.cfg.Skills.Enabled {
		return ActivationResult{}, nil
	}
	if err := m.ensureFresh(ctx); err != nil {
		return ActivationResult{}, err
	}
	snap := m.Snapshot()
	result := routeSkills(req, snap, m.cfg)
	if len(result.Errors) == 0 {
		materialized := make([]SkillActivation, 0, len(result.Activated))
		for _, activation := range result.Activated {
			full, err := materializeSkill(ctx, activation.Skill.Descriptor, m.cache, m.workspace, m.cfg)
			if err != nil {
				if activation.Explicit {
					result.Errors = append(result.Errors, fmt.Sprintf("skill %q failed to load: %v", activation.Skill.Descriptor.Name, err))
					continue
				}
				result.Skipped = append(result.Skipped, SkillSkip{ID: activation.Skill.Descriptor.ID, Name: activation.Skill.Descriptor.Name, Reason: "materialize_failed"})
				continue
			}
			activation.Skill = full
			materialized = append(materialized, activation)
		}
		result.Activated = materialized
	}
	result.Diagnostics.QueryHash = hashQuery(req.Query)
	result.Diagnostics.Activated = len(result.Activated)
	result.Diagnostics.Skipped = len(result.Skipped)
	return result, nil
}

func hashQuery(query string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(strings.ToLower(query))))
	return hex.EncodeToString(sum[:])
}

func maxInt(v, floor int) int {
	if v < floor {
		return floor
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func cloneDescriptor(in SkillDescriptor) SkillDescriptor {
	out := in
	out.Tags = append([]string(nil), in.Tags...)
	out.Aliases = append([]string(nil), in.Aliases...)
	out.Examples = append([]string(nil), in.Examples...)
	out.References = append([]string(nil), in.References...)
	out.Tools = append([]string(nil), in.Tools...)
	if len(in.Errors) > 0 {
		out.Errors = append([]string(nil), in.Errors...)
	}
	if len(in.Extra) > 0 {
		extra := make(map[string]any, len(in.Extra))
		for k, v := range in.Extra {
			extra[k] = v
		}
		out.Extra = extra
	}
	return out
}

func normalizeID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "skill"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		id = "skill"
	}
	return id
}

func sortDescriptors(skills []SkillDescriptor) {
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].ID == skills[j].ID {
			return skills[i].Path < skills[j].Path
		}
		return skills[i].ID < skills[j].ID
	})
}
