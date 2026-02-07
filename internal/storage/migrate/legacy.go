package migrate

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/cron"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
)

type Report struct {
	SessionsImported int
	TurnsImported    int
	JobsImported     int
	FilesCopied      int
	ConfigMerged     bool
}

func ImportLegacy(ctx context.Context, legacyHome, configPath string, cfg config.Config, store *storepkg.Store, mergeConfig bool) (Report, error) {
	report := Report{}
	legacyHome = expandPath(legacyHome)

	if mergeConfig {
		merged, changed, err := mergeLegacyConfig(legacyHome, cfg)
		if err != nil {
			return report, err
		}
		if changed {
			if err := config.Save(configPath, merged); err != nil {
				return report, err
			}
			cfg = merged
			report.ConfigMerged = true
		}
	}

	copied, err := copyLegacyWorkspace(legacyHome, config.WorkspacePath(cfg))
	if err != nil {
		return report, err
	}
	report.FilesCopied += copied

	sessionsImported, turnsImported, err := importLegacySessions(ctx, legacyHome, store)
	if err != nil {
		return report, err
	}
	report.SessionsImported = sessionsImported
	report.TurnsImported = turnsImported

	jobsImported, err := importLegacyCronJobs(ctx, legacyHome, store)
	if err != nil {
		return report, err
	}
	report.JobsImported = jobsImported

	return report, nil
}

func importLegacySessions(ctx context.Context, legacyHome string, store *storepkg.Store) (int, int, error) {
	dir := filepath.Join(legacyHome, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	sessions, turns := 0, 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		sessionID := decodeLegacySessionID(strings.TrimSuffix(entry.Name(), ".jsonl"))
		path := filepath.Join(dir, entry.Name())

		file, openErr := os.Open(path)
		if openErr != nil {
			return sessions, turns, openErr
		}

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		meta := map[string]any{}
		touched := false

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var raw map[string]any
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				continue
			}
			if typeValue, _ := raw["_type"].(string); typeValue == "metadata" {
				if m, ok := raw["metadata"].(map[string]any); ok {
					meta = m
				}
				continue
			}

			role, _ := raw["role"].(string)
			content, _ := raw["content"].(string)
			if strings.TrimSpace(role) == "" || content == "" {
				continue
			}
			turn := agent.Turn{
				SessionID: sessionID,
				Role:      role,
				Content:   content,
				CreatedAt: parseLegacyTimestamp(raw["timestamp"]),
			}
			if err := store.AppendTurn(ctx, turn); err != nil {
				_ = file.Close()
				return sessions, turns, err
			}
			turns++
			touched = true
		}
		if scanErr := scanner.Err(); scanErr != nil {
			_ = file.Close()
			return sessions, turns, scanErr
		}
		_ = file.Close()

		if touched {
			sessions++
			if err := store.SaveSessionMeta(ctx, sessionID, meta); err != nil {
				return sessions, turns, err
			}
		}
	}

	return sessions, turns, nil
}

func importLegacyCronJobs(ctx context.Context, legacyHome string, store *storepkg.Store) (int, error) {
	path := filepath.Join(legacyHome, "cron", "jobs.json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var data legacyCronStore
	if err := json.Unmarshal(bytes, &data); err != nil {
		return 0, err
	}

	service := cron.NewService(store, nil, nil)
	imported := 0
	for _, job := range data.Jobs {
		converted := convertLegacyJob(job)
		if err := service.Put(ctx, converted); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

func convertLegacyJob(job legacyCronJob) cron.Job {
	converted := cron.Job{
		ID:        job.ID,
		Name:      job.Name,
		Enabled:   job.Enabled,
		CreatedAt: msToTime(job.CreatedAtMS),
		UpdatedAt: msToTime(job.UpdatedAtMS),
		Version:   1,
		Payload: cron.JobPayload{
			Message: job.Payload.Message,
			Deliver: job.Payload.Deliver,
			Channel: job.Payload.Channel,
			To:      job.Payload.To,
		},
		State: cron.JobState{
			NextRunAt:  msPtrToTime(job.State.NextRunAtMS),
			LastRunAt:  msPtrToTime(job.State.LastRunAtMS),
			LastStatus: job.State.LastStatus,
			LastError:  job.State.LastError,
		},
	}

	switch strings.ToLower(job.Schedule.Kind) {
	case "at":
		converted.Schedule = cron.JobSchedule{Kind: cron.ScheduleAt, At: msPtrToTime(job.Schedule.AtMS)}
	case "cron":
		converted.Schedule = cron.JobSchedule{Kind: cron.ScheduleCron, Expr: job.Schedule.Expr, TZ: job.Schedule.TZ}
	default:
		every := int64(0)
		if job.Schedule.EveryMS != nil {
			every = *job.Schedule.EveryMS
		}
		converted.Schedule = cron.JobSchedule{Kind: cron.ScheduleEvery, Every: every}
	}

	return converted
}

func copyLegacyWorkspace(legacyHome, workspace string) (int, error) {
	legacyWorkspace := filepath.Join(legacyHome, "workspace")
	if _, err := os.Stat(legacyWorkspace); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0o755); err != nil {
		return 0, err
	}

	copied := 0
	rootFiles := []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md", "HEARTBEAT.md", "IDENTITY.md"}
	for _, name := range rootFiles {
		src := filepath.Join(legacyWorkspace, name)
		dst := filepath.Join(workspace, name)
		n, err := copyIfMissing(src, dst)
		if err != nil {
			return copied, err
		}
		copied += n
	}

	memoryFiles, _ := filepath.Glob(filepath.Join(legacyWorkspace, "memory", "*.md"))
	for _, src := range memoryFiles {
		dst := filepath.Join(workspace, "memory", filepath.Base(src))
		n, err := copyIfMissing(src, dst)
		if err != nil {
			return copied, err
		}
		copied += n
	}

	return copied, nil
}

func copyIfMissing(src, dst string) (int, error) {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if _, err := os.Stat(dst); err == nil {
		return 0, nil
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	bytes, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(dst, bytes, 0o644); err != nil {
		return 0, err
	}
	return 1, nil
}

func mergeLegacyConfig(legacyHome string, cfg config.Config) (config.Config, bool, error) {
	path := filepath.Join(legacyHome, "config.json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, err
	}

	legacy := config.Default()
	if err := json.Unmarshal(bytes, &legacy); err != nil {
		return cfg, false, err
	}

	changed := false
	if strings.TrimSpace(cfg.Providers.OpenRouter.APIKey) == "" && strings.TrimSpace(legacy.Providers.OpenRouter.APIKey) != "" {
		cfg.Providers.OpenRouter = legacy.Providers.OpenRouter
		changed = true
	}
	if strings.TrimSpace(cfg.Providers.Anthropic.APIKey) == "" && strings.TrimSpace(legacy.Providers.Anthropic.APIKey) != "" {
		cfg.Providers.Anthropic = legacy.Providers.Anthropic
		changed = true
	}
	if strings.TrimSpace(cfg.Providers.OpenAI.APIKey) == "" && strings.TrimSpace(legacy.Providers.OpenAI.APIKey) != "" {
		cfg.Providers.OpenAI = legacy.Providers.OpenAI
		changed = true
	}

	if strings.TrimSpace(cfg.Channels.Telegram.Token) == "" && strings.TrimSpace(legacy.Channels.Telegram.Token) != "" {
		cfg.Channels.Telegram.Token = legacy.Channels.Telegram.Token
		changed = true
	}
	if len(cfg.Channels.Telegram.AllowFrom) == 0 && len(legacy.Channels.Telegram.AllowFrom) > 0 {
		cfg.Channels.Telegram.AllowFrom = append([]string{}, legacy.Channels.Telegram.AllowFrom...)
		changed = true
	}
	if !cfg.Channels.Telegram.Enabled && legacy.Channels.Telegram.Enabled {
		cfg.Channels.Telegram.Enabled = true
		changed = true
	}

	if strings.TrimSpace(cfg.Tools.Web.Search.APIKey) == "" && strings.TrimSpace(legacy.Tools.Web.Search.APIKey) != "" {
		cfg.Tools.Web.Search.APIKey = legacy.Tools.Web.Search.APIKey
		changed = true
	}
	if cfg.Tools.Web.Search.MaxResults == 0 && legacy.Tools.Web.Search.MaxResults > 0 {
		cfg.Tools.Web.Search.MaxResults = legacy.Tools.Web.Search.MaxResults
		changed = true
	}

	if strings.TrimSpace(cfg.Agents.Defaults.Model) == "" && strings.TrimSpace(legacy.Agents.Defaults.Model) != "" {
		cfg.Agents.Defaults.Model = legacy.Agents.Defaults.Model
		changed = true
	}

	return cfg, changed, nil
}

func parseLegacyTimestamp(value any) time.Time {
	text, _ := value.(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Now().UTC()
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

func decodeLegacySessionID(stem string) string {
	if stem == "" {
		return "legacy:unknown"
	}
	return strings.ReplaceAll(stem, "_", ":")
}

func msPtrToTime(value *int64) *time.Time {
	if value == nil {
		return nil
	}
	t := msToTime(*value)
	return &t
}

func msToTime(value int64) time.Time {
	if value <= 0 {
		return time.Now().UTC()
	}
	return time.UnixMilli(value).UTC()
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

type legacyCronStore struct {
	Version int             `json:"version"`
	Jobs    []legacyCronJob `json:"jobs"`
}

type legacyCronJob struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Schedule struct {
		Kind    string `json:"kind"`
		AtMS    *int64 `json:"atMs"`
		EveryMS *int64 `json:"everyMs"`
		Expr    string `json:"expr"`
		TZ      string `json:"tz"`
	} `json:"schedule"`
	Payload struct {
		Message string `json:"message"`
		Deliver bool   `json:"deliver"`
		Channel string `json:"channel"`
		To      string `json:"to"`
	} `json:"payload"`
	State struct {
		NextRunAtMS *int64 `json:"nextRunAtMs"`
		LastRunAtMS *int64 `json:"lastRunAtMs"`
		LastStatus  string `json:"lastStatus"`
		LastError   string `json:"lastError"`
	} `json:"state"`
	CreatedAtMS int64 `json:"createdAtMs"`
	UpdatedAtMS int64 `json:"updatedAtMs"`
}
