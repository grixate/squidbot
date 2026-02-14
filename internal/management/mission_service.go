package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/memory"
	"github.com/grixate/squidbot/internal/mission"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/telemetry"
)

var (
	errNotFound = errors.New("not found")
	errConflict = errors.New("conflict")
)

type MissionControlService struct {
	store        *storepkg.Store
	configPath   string
	metrics      *telemetry.Metrics
	heartbeat    HeartbeatRuntime
	logger       *log.Logger
	getConfig    func() config.Config
	saveConfig   func(next config.Config) error
	providerTest func(ctx context.Context, providerName string, providerCfg config.ProviderConfig) error
}

type Overview struct {
	RuntimeOnline bool              `json:"runtimeOnline"`
	AgentState    string            `json:"agentState"`
	ActiveActors  uint64            `json:"activeActors"`
	ActiveTurns   uint64            `json:"activeTurns"`
	OpenTasks     int               `json:"openTasks"`
	DueSoon       int               `json:"dueSoon"`
	Overdue       int               `json:"overdue"`
	TokenToday    mission.UsageDay  `json:"tokenToday"`
	TokenWeek     mission.UsageDay  `json:"tokenWeek"`
	Metrics       map[string]uint64 `json:"metrics"`
}

type KanbanBoard struct {
	Columns []mission.Column `json:"columns"`
	Tasks   []mission.Task   `json:"tasks"`
}

type CreateTaskInput struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	ColumnID    string             `json:"columnId"`
	Priority    string             `json:"priority"`
	Assignee    string             `json:"assignee"`
	Notes       string             `json:"notes"`
	DueAt       *time.Time         `json:"dueAt,omitempty"`
	Source      mission.TaskSource `json:"source"`
	Dedupe      bool               `json:"dedupe"`
	Metadata    map[string]any     `json:"metadata,omitempty"`
}

type UpdateTaskInput struct {
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	ColumnID    *string    `json:"columnId,omitempty"`
	Priority    *string    `json:"priority,omitempty"`
	Assignee    *string    `json:"assignee,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
	DueAt       *time.Time `json:"dueAt,omitempty"`
	ClearDueAt  bool       `json:"clearDueAt,omitempty"`
}

type ConfigSnapshot struct {
	Providers struct {
		Active string `json:"active"`
		Items  []struct {
			ID        string `json:"id"`
			Label     string `json:"label"`
			APIBase   string `json:"apiBase,omitempty"`
			Model     string `json:"model,omitempty"`
			HasAPIKey bool   `json:"hasApiKey"`
		} `json:"items"`
	} `json:"providers"`
	Channels struct {
		Items map[string]struct {
			ID        string   `json:"id"`
			Label     string   `json:"label"`
			Kind      string   `json:"kind"`
			Enabled   bool     `json:"enabled"`
			TokenSet  bool     `json:"tokenSet"`
			AllowFrom []string `json:"allowFrom,omitempty"`
			Endpoint  string   `json:"endpoint,omitempty"`
		} `json:"items"`
		Telegram struct {
			Enabled   bool     `json:"enabled"`
			TokenSet  bool     `json:"tokenSet"`
			AllowFrom []string `json:"allowFrom"`
		} `json:"telegram"`
	} `json:"channels"`
	Runtime struct {
		HeartbeatIntervalSec int `json:"heartbeatIntervalSec"`
		MailboxSize          int `json:"mailboxSize"`
	} `json:"runtime"`
	Management struct {
		Host           string `json:"host"`
		Port           int    `json:"port"`
		PublicBaseURL  string `json:"publicBaseUrl"`
		ServeInGateway bool   `json:"serveInGateway"`
	} `json:"management"`
}

type FileDescriptor struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Path     string `json:"path"`
	Editable bool   `json:"editable"`
}

var editableFiles = map[string]FileDescriptor{
	"AGENTS":    {ID: "AGENTS", Label: "AGENTS.md", Path: "AGENTS.md", Editable: true},
	"SOUL":      {ID: "SOUL", Label: "SOUL.md", Path: "SOUL.md", Editable: true},
	"USER":      {ID: "USER", Label: "USER.md", Path: "USER.md", Editable: true},
	"TOOLS":     {ID: "TOOLS", Label: "TOOLS.md", Path: "TOOLS.md", Editable: true},
	"HEARTBEAT": {ID: "HEARTBEAT", Label: "HEARTBEAT.md", Path: "HEARTBEAT.md", Editable: true},
	"MEMORY":    {ID: "MEMORY", Label: "memory/MEMORY.md", Path: "memory/MEMORY.md", Editable: true},
}

func NewMissionControlService(
	initialCfg config.Config,
	configPath string,
	metrics *telemetry.Metrics,
	heartbeat HeartbeatRuntime,
	logger *log.Logger,
	getCfg func() config.Config,
	saveCfg func(next config.Config) error,
	providerTest func(ctx context.Context, providerName string, providerCfg config.ProviderConfig) error,
) (*MissionControlService, error) {
	if logger == nil {
		logger = log.Default()
	}
	if metrics == nil {
		metrics = &telemetry.Metrics{}
	}
	if getCfg == nil {
		cfgCopy := initialCfg
		getCfg = func() config.Config { return cfgCopy }
	}
	if saveCfg == nil {
		resolvedPath := strings.TrimSpace(configPath)
		if resolvedPath == "" {
			resolvedPath = config.ConfigPath()
		}
		saveCfg = func(next config.Config) error {
			return config.Save(resolvedPath, next)
		}
	}

	store, err := storepkg.Open(initialCfg.Storage.DBPath)
	if err != nil {
		return nil, err
	}

	svc := &MissionControlService{
		store:        store,
		configPath:   configPath,
		metrics:      metrics,
		heartbeat:    heartbeat,
		logger:       logger,
		getConfig:    getCfg,
		saveConfig:   saveCfg,
		providerTest: providerTest,
	}
	if _, err := svc.ensureColumns(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	return svc, nil
}

func (s *MissionControlService) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *MissionControlService) Overview(ctx context.Context) (Overview, error) {
	_ = ctx
	board, err := s.Kanban(context.Background())
	if err != nil {
		return Overview{}, err
	}
	now := time.Now().UTC()
	openTasks := 0
	dueSoon := 0
	overdue := 0
	for _, task := range board.Tasks {
		if task.ColumnID != mission.ColumnDone {
			openTasks++
		}
		if task.DueAt == nil {
			continue
		}
		if task.DueAt.Before(now) && task.ColumnID != mission.ColumnDone {
			overdue++
			continue
		}
		if task.DueAt.After(now) && task.DueAt.Before(now.Add(24*time.Hour)) {
			dueSoon++
		}
	}

	usageDays, err := s.store.ListUsageDays(context.Background())
	if err != nil {
		return Overview{}, err
	}
	var today mission.UsageDay
	week := mission.UsageDay{Day: now.Format("2006-01-02")}
	weekStart := now.AddDate(0, 0, -6).Format("2006-01-02")
	todayDay := now.Format("2006-01-02")
	for _, day := range usageDays {
		if day.Day == todayDay {
			today = day
		}
		if day.Day >= weekStart {
			week.PromptTokens += day.PromptTokens
			week.CompletionTokens += day.CompletionTokens
			week.TotalTokens += day.TotalTokens
			week.UpdatedAt = day.UpdatedAt
		}
	}

	metrics := s.metrics.Snapshot()
	agentState := "offline"
	runtimeOnline := s.heartbeat != nil
	if runtimeOnline {
		agentState = "idle"
		if metrics["active_turns"] > 0 {
			agentState = "working"
		}
	}
	return Overview{
		RuntimeOnline: runtimeOnline,
		AgentState:    agentState,
		ActiveActors:  metrics["active_actors"],
		ActiveTurns:   metrics["active_turns"],
		OpenTasks:     openTasks,
		DueSoon:       dueSoon,
		Overdue:       overdue,
		TokenToday:    today,
		TokenWeek:     week,
		Metrics:       metrics,
	}, nil
}

func (s *MissionControlService) Kanban(ctx context.Context) (KanbanBoard, error) {
	columns, err := s.ensureColumns(ctx)
	if err != nil {
		return KanbanBoard{}, err
	}
	tasks, err := s.store.ListMissionTasks(ctx)
	if err != nil {
		return KanbanBoard{}, err
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].ColumnID == tasks[j].ColumnID {
			if tasks[i].Position == tasks[j].Position {
				return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
			}
			return tasks[i].Position < tasks[j].Position
		}
		return tasks[i].ColumnID < tasks[j].ColumnID
	})
	return KanbanBoard{Columns: columns, Tasks: tasks}, nil
}

func (s *MissionControlService) CreateTask(ctx context.Context, in CreateTaskInput) (mission.Task, bool, error) {
	if strings.TrimSpace(in.Title) == "" {
		return mission.Task{}, false, fmt.Errorf("title is required")
	}
	columns, err := s.ensureColumns(ctx)
	if err != nil {
		return mission.Task{}, false, err
	}
	columnSet := map[string]struct{}{}
	for _, c := range columns {
		columnSet[c.ID] = struct{}{}
	}
	columnID := strings.TrimSpace(in.ColumnID)
	if columnID == "" {
		columnID = mission.ColumnBacklog
	}
	if _, ok := columnSet[columnID]; !ok {
		columnID = mission.ColumnBacklog
	}
	now := time.Now().UTC()
	tasks, err := s.store.ListMissionTasks(ctx)
	if err != nil {
		return mission.Task{}, false, err
	}

	if in.Dedupe {
		normalizedTitle := mission.NormalizeTaskTitle(in.Title)
		for _, task := range tasks {
			if mission.NormalizeTaskTitle(task.Title) != normalizedTitle {
				continue
			}
			if task.Source.Type != in.Source.Type {
				continue
			}
			if strings.TrimSpace(in.Source.SessionID) != "" && task.Source.SessionID != in.Source.SessionID {
				continue
			}
			if now.Sub(task.CreatedAt) > 6*time.Hour {
				continue
			}
			task.UpdatedAt = now
			task.Version++
			if strings.TrimSpace(in.Description) != "" {
				task.Description = strings.TrimSpace(in.Description)
			}
			if priority := mission.NormalizePriority(in.Priority); priority != "" {
				task.Priority = priority
			}
			if strings.TrimSpace(in.Assignee) != "" {
				task.Assignee = strings.TrimSpace(in.Assignee)
			}
			if strings.TrimSpace(in.Notes) != "" {
				if strings.TrimSpace(task.Notes) == "" {
					task.Notes = strings.TrimSpace(in.Notes)
				} else {
					task.Notes = strings.TrimSpace(task.Notes) + "\n" + strings.TrimSpace(in.Notes)
				}
			}
			if in.DueAt != nil {
				due := in.DueAt.UTC()
				task.DueAt = &due
			}
			task.Events = append(task.Events, mission.TaskEvent{
				ID:        "evt-" + now.Format("20060102150405.000000000"),
				Type:      mission.TaskEventUpdated,
				Actor:     "system",
				Summary:   "task deduplicated",
				CreatedAt: now,
			})
			if err := s.store.PutMissionTask(ctx, task); err != nil {
				return mission.Task{}, false, err
			}
			return task, true, nil
		}
	}

	task := mission.Task{
		ID:          fmt.Sprintf("task-%d", now.UnixNano()),
		Title:       strings.TrimSpace(in.Title),
		Description: strings.TrimSpace(in.Description),
		ColumnID:    columnID,
		Priority:    mission.NormalizePriority(in.Priority),
		Assignee:    strings.TrimSpace(in.Assignee),
		Notes:       strings.TrimSpace(in.Notes),
		Source:      in.Source,
		Position:    nextPosition(tasks, columnID),
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
		Events: []mission.TaskEvent{{
			ID:        "evt-" + now.Format("20060102150405.000000000"),
			Type:      mission.TaskEventCreated,
			Actor:     "system",
			Summary:   "task created",
			CreatedAt: now,
		}},
	}
	if in.DueAt != nil {
		due := in.DueAt.UTC()
		task.DueAt = &due
	}
	if err := s.store.PutMissionTask(ctx, task); err != nil {
		return mission.Task{}, false, err
	}
	return task, false, nil
}

func (s *MissionControlService) UpdateTask(ctx context.Context, id string, in UpdateTaskInput) (mission.Task, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return mission.Task{}, fmt.Errorf("task id is required")
	}
	tasks, err := s.store.ListMissionTasks(ctx)
	if err != nil {
		return mission.Task{}, err
	}
	found := -1
	for i := range tasks {
		if tasks[i].ID == id {
			found = i
			break
		}
	}
	if found < 0 {
		return mission.Task{}, errNotFound
	}

	task := tasks[found]
	if in.Title != nil {
		value := strings.TrimSpace(*in.Title)
		if value == "" {
			return mission.Task{}, fmt.Errorf("title cannot be empty")
		}
		task.Title = value
	}
	if in.Description != nil {
		task.Description = strings.TrimSpace(*in.Description)
	}
	if in.ColumnID != nil {
		columns, colErr := s.ensureColumns(ctx)
		if colErr != nil {
			return mission.Task{}, colErr
		}
		columnID := strings.TrimSpace(*in.ColumnID)
		valid := false
		for _, col := range columns {
			if col.ID == columnID {
				valid = true
				break
			}
		}
		if !valid {
			return mission.Task{}, fmt.Errorf("unknown column %q", columnID)
		}
		if task.ColumnID != columnID {
			task.ColumnID = columnID
			task.Position = nextPosition(tasks, columnID)
			task.Events = append(task.Events, mission.TaskEvent{
				ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
				Type:      mission.TaskEventMoved,
				Actor:     "user",
				Summary:   "task moved",
				CreatedAt: time.Now().UTC(),
			})
		}
	}
	if in.Priority != nil {
		task.Priority = mission.NormalizePriority(*in.Priority)
	}
	if in.Assignee != nil {
		task.Assignee = strings.TrimSpace(*in.Assignee)
	}
	if in.Notes != nil {
		task.Notes = strings.TrimSpace(*in.Notes)
	}
	if in.ClearDueAt {
		task.DueAt = nil
	} else if in.DueAt != nil {
		due := in.DueAt.UTC()
		task.DueAt = &due
	}

	task.UpdatedAt = time.Now().UTC()
	task.Version++
	task.Events = append(task.Events, mission.TaskEvent{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      mission.TaskEventUpdated,
		Actor:     "user",
		Summary:   "task updated",
		CreatedAt: time.Now().UTC(),
	})
	if err := s.store.PutMissionTask(ctx, task); err != nil {
		return mission.Task{}, err
	}
	return task, nil
}

func (s *MissionControlService) MoveTask(ctx context.Context, id, targetColumn string, targetPosition int) (mission.Task, error) {
	task, err := s.UpdateTask(ctx, id, UpdateTaskInput{ColumnID: &targetColumn})
	if err != nil {
		return mission.Task{}, err
	}
	all, err := s.store.ListMissionTasks(ctx)
	if err != nil {
		return mission.Task{}, err
	}
	columnTasks := make([]mission.Task, 0, len(all))
	for _, t := range all {
		if t.ColumnID == targetColumn && t.ID != task.ID {
			columnTasks = append(columnTasks, t)
		}
	}
	sort.Slice(columnTasks, func(i, j int) bool { return columnTasks[i].Position < columnTasks[j].Position })
	if targetPosition < 0 {
		targetPosition = 0
	}
	if targetPosition > len(columnTasks) {
		targetPosition = len(columnTasks)
	}
	rebuilt := make([]mission.Task, 0, len(columnTasks)+1)
	for i, t := range columnTasks {
		if i == targetPosition {
			rebuilt = append(rebuilt, task)
		}
		rebuilt = append(rebuilt, t)
	}
	if targetPosition >= len(columnTasks) {
		rebuilt = append(rebuilt, task)
	}
	for i := range rebuilt {
		rebuilt[i].Position = i
		rebuilt[i].UpdatedAt = time.Now().UTC()
		rebuilt[i].Version++
		if err := s.store.PutMissionTask(ctx, rebuilt[i]); err != nil {
			return mission.Task{}, err
		}
		if rebuilt[i].ID == task.ID {
			task = rebuilt[i]
		}
	}
	return task, nil
}

func (s *MissionControlService) DeleteTask(ctx context.Context, id string) error {
	tasks, err := s.store.ListMissionTasks(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, task := range tasks {
		if task.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errNotFound
	}
	return s.store.DeleteMissionTask(ctx, id)
}

func (s *MissionControlService) SetColumns(ctx context.Context, columns []mission.Column) ([]mission.Column, error) {
	if len(columns) == 0 {
		return nil, fmt.Errorf("at least one column is required")
	}
	seen := map[string]struct{}{}
	now := time.Now().UTC()
	for i := range columns {
		columns[i].ID = strings.TrimSpace(columns[i].ID)
		columns[i].Label = strings.TrimSpace(columns[i].Label)
		if columns[i].ID == "" || columns[i].Label == "" {
			return nil, fmt.Errorf("column id and label are required")
		}
		if _, ok := seen[columns[i].ID]; ok {
			return nil, fmt.Errorf("duplicate column %q", columns[i].ID)
		}
		seen[columns[i].ID] = struct{}{}
		columns[i].Position = i
		if columns[i].CreatedAt.IsZero() {
			columns[i].CreatedAt = now
		}
		columns[i].UpdatedAt = now
		if columns[i].Version <= 0 {
			columns[i].Version = 1
		}
	}
	if err := s.store.ReplaceMissionColumns(ctx, columns); err != nil {
		return nil, err
	}

	// Move tasks from removed columns to backlog.
	tasks, err := s.store.ListMissionTasks(ctx)
	if err != nil {
		return nil, err
	}
	nextBacklog := nextPosition(tasks, mission.ColumnBacklog)
	for _, task := range tasks {
		if _, ok := seen[task.ColumnID]; ok {
			continue
		}
		task.ColumnID = mission.ColumnBacklog
		task.Position = nextBacklog
		nextBacklog++
		task.UpdatedAt = now
		task.Version++
		task.Events = append(task.Events, mission.TaskEvent{
			ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
			Type:      mission.TaskEventMoved,
			Actor:     "system",
			Summary:   "column removed, task moved to backlog",
			CreatedAt: now,
		})
		if err := s.store.PutMissionTask(ctx, task); err != nil {
			return nil, err
		}
	}
	return columns, nil
}

func (s *MissionControlService) HeartbeatStatus(ctx context.Context) (map[string]any, error) {
	_ = ctx
	out := map[string]any{
		"runtimeOnline": s.heartbeat != nil,
		"intervalSec":   0,
		"running":       false,
		"nextRunAt":     "",
		"lastRun":       nil,
		"recentRuns":    []mission.HeartbeatRun{},
	}
	cfg := s.getConfig()
	out["intervalSec"] = cfg.Runtime.HeartbeatIntervalSec
	if s.heartbeat != nil {
		out["running"] = s.heartbeat.Running()
		out["intervalSec"] = int(s.heartbeat.Interval().Seconds())
		if next, ok := s.heartbeat.NextRunAt(); ok {
			out["nextRunAt"] = next.UTC().Format(time.RFC3339)
		}
		if last, ok := s.heartbeat.LastRun(); ok {
			out["lastRun"] = map[string]any{
				"triggeredBy": last.TriggeredBy,
				"status":      last.Status,
				"error":       last.Error,
				"response":    last.Response,
				"startedAt":   last.StartedAt.UTC().Format(time.RFC3339),
				"finishedAt":  last.FinishedAt.UTC().Format(time.RFC3339),
			}
		}
	}
	runs, err := s.store.ListHeartbeatRuns(context.Background(), 30)
	if err != nil {
		return nil, err
	}
	out["recentRuns"] = runs
	return out, nil
}

func (s *MissionControlService) TriggerHeartbeat(ctx context.Context) (string, error) {
	if s.heartbeat == nil {
		return "", fmt.Errorf("runtime is offline")
	}
	return s.heartbeat.TriggerNow(ctx)
}

func (s *MissionControlService) SetHeartbeatInterval(ctx context.Context, interval time.Duration) (map[string]any, error) {
	_ = ctx
	if interval <= 0 {
		return nil, fmt.Errorf("interval must be > 0")
	}
	cfg := s.getConfig()
	cfg.Runtime.HeartbeatIntervalSec = int(interval.Seconds())
	if cfg.Runtime.HeartbeatIntervalSec <= 0 {
		cfg.Runtime.HeartbeatIntervalSec = 1
	}
	if err := s.saveConfig(cfg); err != nil {
		return nil, err
	}
	applied := false
	if s.heartbeat != nil {
		s.heartbeat.SetInterval(interval)
		applied = true
	}
	return map[string]any{
		"intervalSec":     cfg.Runtime.HeartbeatIntervalSec,
		"runtimeApplied":  applied,
		"restartRequired": !applied,
	}, nil
}

func (s *MissionControlService) MemorySearch(ctx context.Context, query string, limit int) ([]memory.Chunk, error) {
	cfg := s.getConfig()
	manager := memory.NewManager(cfg)
	if err := manager.EnsureIndex(ctx); err != nil {
		return nil, err
	}
	return manager.Search(ctx, query, limit)
}

func (s *MissionControlService) ListFiles(_ context.Context) []FileDescriptor {
	out := make([]FileDescriptor, 0, len(editableFiles))
	for _, file := range editableFiles {
		out = append(out, file)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func (s *MissionControlService) ReadFile(ctx context.Context, fileID string) (map[string]any, error) {
	_ = ctx
	file, path, err := s.resolveEditableFile(fileID)
	if err != nil {
		return nil, err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":      file.ID,
		"path":    file.Path,
		"content": string(bytes),
		"etag":    etagForBytes(bytes),
	}, nil
}

func (s *MissionControlService) WriteFile(ctx context.Context, fileID, content, expectedETag string) (map[string]any, error) {
	_ = ctx
	file, path, err := s.resolveEditableFile(fileID)
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if strings.TrimSpace(expectedETag) != "" {
		if etagForBytes(current) != strings.TrimSpace(expectedETag) {
			return nil, errConflict
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}

	cfg := s.getConfig()
	manager := memory.NewManager(cfg)
	if syncErr := manager.Sync(ctx); syncErr != nil {
		s.logger.Printf("memory sync after file write failed: %v", syncErr)
	}

	bytes := []byte(content)
	return map[string]any{
		"id":      file.ID,
		"path":    file.Path,
		"content": content,
		"etag":    etagForBytes(bytes),
	}, nil
}

func (s *MissionControlService) AnalyticsHealth(ctx context.Context) (map[string]any, error) {
	_ = ctx
	cfg := s.getConfig()
	status := config.BuildStatus(cfg)
	metrics := s.metrics.Snapshot()
	return map[string]any{
		"status": map[string]any{
			"configPath":  status.ConfigPath,
			"configOK":    status.ConfigOK,
			"workspace":   status.Workspace,
			"workspaceOK": status.WorkspaceOK,
			"dataRoot":    status.DataRoot,
			"dataRootOK":  status.DataRootOK,
		},
		"metrics": metrics,
	}, nil
}

func (s *MissionControlService) AnalyticsLogs(ctx context.Context, limit int) ([]map[string]any, error) {
	toolEvents, err := s.store.ListToolEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	jobRuns, err := s.store.ListJobRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	heartbeatRuns, err := s.store.ListHeartbeatRuns(ctx, limit)
	if err != nil {
		return nil, err
	}
	records := make([]map[string]any, 0, len(toolEvents)+len(jobRuns)+len(heartbeatRuns))
	for _, event := range toolEvents {
		records = append(records, map[string]any{
			"type":      "tool",
			"name":      event.ToolName,
			"sessionId": event.SessionID,
			"summary":   truncateLine(event.Output, 180),
			"createdAt": event.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	for _, run := range jobRuns {
		records = append(records, map[string]any{
			"type":      "cron",
			"jobId":     run["job_id"],
			"createdAt": toRFC3339(run["run_at"]),
			"summary":   truncateLine(fmt.Sprint(run["result"]), 180),
			"error":     fmt.Sprint(run["error"]),
		})
	}
	for _, run := range heartbeatRuns {
		records = append(records, map[string]any{
			"type":      "heartbeat",
			"status":    run.Status,
			"createdAt": run.StartedAt.UTC().Format(time.RFC3339),
			"summary":   truncateLine(run.Preview, 180),
			"error":     run.Error,
		})
	}
	sort.Slice(records, func(i, j int) bool {
		return fmt.Sprint(records[i]["createdAt"]) > fmt.Sprint(records[j]["createdAt"])
	})
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *MissionControlService) Settings() ConfigSnapshot {
	cfg := s.getConfig()
	var out ConfigSnapshot
	out.Providers.Active = cfg.Providers.Active
	for _, providerName := range config.SupportedProviders() {
		providerCfg, _ := cfg.ProviderByName(providerName)
		item := struct {
			ID        string `json:"id"`
			Label     string `json:"label"`
			APIBase   string `json:"apiBase,omitempty"`
			Model     string `json:"model,omitempty"`
			HasAPIKey bool   `json:"hasApiKey"`
		}{
			ID:        providerName,
			Label:     providerLabel(providerName),
			APIBase:   providerCfg.APIBase,
			Model:     providerCfg.Model,
			HasAPIKey: strings.TrimSpace(providerCfg.APIKey) != "",
		}
		out.Providers.Items = append(out.Providers.Items, item)
	}
	out.Channels.Items = map[string]struct {
		ID        string   `json:"id"`
		Label     string   `json:"label"`
		Kind      string   `json:"kind"`
		Enabled   bool     `json:"enabled"`
		TokenSet  bool     `json:"tokenSet"`
		AllowFrom []string `json:"allowFrom,omitempty"`
		Endpoint  string   `json:"endpoint,omitempty"`
	}{}
	for _, channelID := range config.SupportedChannels() {
		profile, _ := config.ChannelProfile(channelID)
		current := cfg.Channels.Registry[channelID]
		out.Channels.Items[channelID] = struct {
			ID        string   `json:"id"`
			Label     string   `json:"label"`
			Kind      string   `json:"kind"`
			Enabled   bool     `json:"enabled"`
			TokenSet  bool     `json:"tokenSet"`
			AllowFrom []string `json:"allowFrom,omitempty"`
			Endpoint  string   `json:"endpoint,omitempty"`
		}{
			ID:        channelID,
			Label:     profile.Label,
			Kind:      profile.Kind,
			Enabled:   current.Enabled,
			TokenSet:  strings.TrimSpace(current.Token) != "",
			AllowFrom: current.AllowFrom,
			Endpoint:  current.Endpoint,
		}
	}
	out.Channels.Telegram.Enabled = cfg.Channels.Telegram.Enabled
	out.Channels.Telegram.TokenSet = strings.TrimSpace(cfg.Channels.Telegram.Token) != ""
	out.Channels.Telegram.AllowFrom = cfg.Channels.Telegram.AllowFrom
	out.Runtime.HeartbeatIntervalSec = cfg.Runtime.HeartbeatIntervalSec
	out.Runtime.MailboxSize = cfg.Runtime.MailboxSize
	out.Management.Host = cfg.Management.Host
	out.Management.Port = cfg.Management.Port
	out.Management.PublicBaseURL = cfg.Management.PublicBaseURL
	out.Management.ServeInGateway = cfg.Management.ServeInGateway
	return out
}

func (s *MissionControlService) TestProvider(ctx context.Context, providerName string, draft config.ProviderConfig) error {
	if s.providerTest == nil {
		return fmt.Errorf("provider test is unavailable")
	}
	return s.providerTest(ctx, providerName, draft)
}

func (s *MissionControlService) UpdateProvider(ctx context.Context, providerName string, draft config.ProviderConfig, activate bool, remove bool) (map[string]any, error) {
	_ = ctx
	cfg := s.getConfig()
	normalized, ok := config.NormalizeProviderName(providerName)
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", providerName)
	}
	if remove {
		_ = cfg.SetProviderByName(normalized, config.ProviderConfig{})
		if cfg.Providers.Active == normalized {
			return nil, fmt.Errorf("cannot remove active provider")
		}
		if err := s.saveConfig(cfg); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "restartRequired": true}, nil
	}
	if err := config.ValidateProviderDraft(normalized, draft); err != nil {
		return nil, err
	}
	_ = cfg.SetProviderByName(normalized, draft)
	if activate {
		cfg.Providers.Active = normalized
		if err := config.ValidateActiveProvider(cfg); err != nil {
			return nil, err
		}
	}
	if err := s.saveConfig(cfg); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "restartRequired": true}, nil
}

func (s *MissionControlService) UpdateTelegram(ctx context.Context, telegram config.TelegramConfig) (map[string]any, error) {
	_ = ctx
	cfg := s.getConfig()
	activeProvider := cfg.Providers.Active
	activeCfg, _ := cfg.ProviderByName(activeProvider)
	next, err := config.ApplyOnboardingInput(cfg, config.OnboardingInput{
		Provider:       activeProvider,
		ProviderConfig: activeCfg,
		Telegram:       telegram,
	})
	if err != nil {
		return nil, err
	}
	if err := s.saveConfig(next); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "restartRequired": true}, nil
}

func (s *MissionControlService) UpdateChannel(ctx context.Context, channelID string, channel config.GenericChannelConfig) (map[string]any, error) {
	_ = ctx
	channelID = strings.TrimSpace(strings.ToLower(channelID))
	if channelID == "" {
		return nil, fmt.Errorf("channel id is required")
	}
	cfg := s.getConfig()
	activeProvider := cfg.Providers.Active
	activeCfg, _ := cfg.ProviderByName(activeProvider)
	channels := map[string]config.GenericChannelConfig{}
	for id, current := range cfg.Channels.Registry {
		channels[id] = current
	}
	channels[channelID] = channel
	next, err := config.ApplyOnboardingInput(cfg, config.OnboardingInput{
		Provider:       activeProvider,
		ProviderConfig: activeCfg,
		Telegram:       cfg.Channels.Telegram,
		Channels:       channels,
	})
	if err != nil {
		return nil, err
	}
	if err := s.saveConfig(next); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "restartRequired": true}, nil
}

func (s *MissionControlService) UpdateRuntime(ctx context.Context, heartbeatIntervalSec *int, mailboxSize *int) (map[string]any, error) {
	_ = ctx
	cfg := s.getConfig()
	if heartbeatIntervalSec != nil {
		if *heartbeatIntervalSec <= 0 {
			return nil, fmt.Errorf("heartbeatIntervalSec must be > 0")
		}
		cfg.Runtime.HeartbeatIntervalSec = *heartbeatIntervalSec
	}
	if mailboxSize != nil {
		if *mailboxSize <= 0 {
			return nil, fmt.Errorf("mailboxSize must be > 0")
		}
		cfg.Runtime.MailboxSize = *mailboxSize
	}
	if err := s.saveConfig(cfg); err != nil {
		return nil, err
	}
	applied := false
	if heartbeatIntervalSec != nil && s.heartbeat != nil {
		s.heartbeat.SetInterval(time.Duration(*heartbeatIntervalSec) * time.Second)
		applied = true
	}
	return map[string]any{
		"ok":              true,
		"runtimeApplied":  applied,
		"restartRequired": heartbeatIntervalSec != nil && !applied,
	}, nil
}

func (s *MissionControlService) UpdatePassword(ctx context.Context, currentPassword, nextPassword string, minLength int) (map[string]any, error) {
	_ = ctx
	currentPassword = strings.TrimSpace(currentPassword)
	nextPassword = strings.TrimSpace(nextPassword)
	if len(nextPassword) < minLength {
		return nil, fmt.Errorf("password must be at least %d characters", minLength)
	}
	cfg := s.getConfig()
	if !VerifyPassword(currentPassword, cfg.Auth.PasswordHash) {
		return nil, fmt.Errorf("invalid current password")
	}
	hash, err := HashPassword(nextPassword)
	if err != nil {
		return nil, err
	}
	cfg.Auth.PasswordHash = hash
	cfg.Auth.PasswordUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.saveConfig(cfg); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (s *MissionControlService) ensureColumns(ctx context.Context) ([]mission.Column, error) {
	columns, err := s.store.ListMissionColumns(ctx)
	if err != nil {
		return nil, err
	}
	if len(columns) > 0 {
		return columns, nil
	}
	defaults := mission.DefaultColumns(time.Now().UTC())
	if err := s.store.ReplaceMissionColumns(ctx, defaults); err != nil {
		return nil, err
	}
	return defaults, nil
}

func (s *MissionControlService) resolveEditableFile(fileID string) (FileDescriptor, string, error) {
	key := strings.TrimSpace(strings.ToUpper(fileID))
	file, ok := editableFiles[key]
	if !ok {
		return FileDescriptor{}, "", errNotFound
	}
	cfg := s.getConfig()
	workspace := config.WorkspacePath(cfg)
	path := filepath.Join(workspace, filepath.FromSlash(file.Path))
	cleanWorkspace := filepath.Clean(workspace) + string(os.PathSeparator)
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath, cleanWorkspace) && cleanPath != filepath.Clean(workspace) {
		return FileDescriptor{}, "", fmt.Errorf("path outside workspace")
	}
	return file, cleanPath, nil
}

func nextPosition(tasks []mission.Task, columnID string) int {
	maxPos := -1
	for _, task := range tasks {
		if task.ColumnID != columnID {
			continue
		}
		if task.Position > maxPos {
			maxPos = task.Position
		}
	}
	return maxPos + 1
}

func etagForBytes(bytes []byte) string {
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

func truncateLine(in string, max int) string {
	trimmed := strings.TrimSpace(in)
	if len(trimmed) <= max || max <= 0 {
		return trimmed
	}
	return trimmed[:max-3] + "..."
}

func toRFC3339(value any) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			return typed
		}
	case time.Time:
		if !typed.IsZero() {
			return typed.UTC().Format(time.RFC3339)
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}
