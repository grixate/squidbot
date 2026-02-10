package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	runtime      RuntimeController
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
		Telegram struct {
			Enabled   bool     `json:"enabled"`
			TokenSet  bool     `json:"tokenSet"`
			AllowFrom []string `json:"allowFrom"`
		} `json:"telegram"`
		Scaffolds map[string]config.GenericChannelConfig `json:"scaffolds,omitempty"`
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

type ChannelDescriptor struct {
	ID              string                       `json:"id"`
	Label           string                       `json:"label"`
	Kind            string                       `json:"kind"`
	RuntimeHotApply bool                         `json:"runtimeHotApply"`
	Config          map[string]any               `json:"config,omitempty"`
	Scaffold        *config.GenericChannelConfig `json:"scaffold,omitempty"`
}

type AnalyticsLogFilter struct {
	Type  string
	From  *time.Time
	To    *time.Time
	Limit int
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
	runtime RuntimeController,
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
		runtime:      runtime,
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
	policy := mission.DefaultTaskAutomationPolicy(now)
	if in.Dedupe {
		if stored, policyErr := s.store.GetTaskAutomationPolicy(ctx); policyErr == nil {
			policy = stored
		}
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
			if now.Sub(task.CreatedAt) > policy.DedupeWindow() {
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

func (s *MissionControlService) TaskPolicy(ctx context.Context) (mission.TaskAutomationPolicy, error) {
	policy, err := s.store.GetTaskAutomationPolicy(ctx)
	if err != nil {
		return mission.TaskAutomationPolicy{}, err
	}
	if strings.TrimSpace(policy.DefaultColumnID) == "" {
		policy.DefaultColumnID = mission.ColumnBacklog
	}
	if policy.DedupeWindowSec <= 0 {
		policy.DedupeWindowSec = int((6 * time.Hour).Seconds())
	}
	return policy, nil
}

func (s *MissionControlService) SetTaskPolicy(ctx context.Context, in mission.TaskAutomationPolicy) (mission.TaskAutomationPolicy, error) {
	policy := in
	if strings.TrimSpace(policy.DefaultColumnID) == "" {
		policy.DefaultColumnID = mission.ColumnBacklog
	}
	if policy.DedupeWindowSec <= 0 {
		policy.DedupeWindowSec = int((6 * time.Hour).Seconds())
	}
	columns, err := s.ensureColumns(ctx)
	if err != nil {
		return mission.TaskAutomationPolicy{}, err
	}
	validColumn := false
	for _, column := range columns {
		if column.ID == policy.DefaultColumnID {
			validColumn = true
			break
		}
	}
	if !validColumn {
		return mission.TaskAutomationPolicy{}, fmt.Errorf("defaultColumnId must be an existing column")
	}
	policy.UpdatedAt = time.Now().UTC()
	if err := s.store.PutTaskAutomationPolicy(ctx, policy); err != nil {
		return mission.TaskAutomationPolicy{}, err
	}
	return policy, nil
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
	return s.AnalyticsLogsFiltered(ctx, AnalyticsLogFilter{Limit: limit})
}

func (s *MissionControlService) AnalyticsLogsFiltered(ctx context.Context, filter AnalyticsLogFilter) ([]map[string]any, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	fetchLimit := limit
	if fetchLimit < 1000 {
		fetchLimit = 1000
	}
	toolEvents, err := s.store.ListToolEvents(ctx, fetchLimit)
	if err != nil {
		return nil, err
	}
	jobRuns, err := s.store.ListJobRuns(ctx, fetchLimit)
	if err != nil {
		return nil, err
	}
	heartbeatRuns, err := s.store.ListHeartbeatRuns(ctx, fetchLimit)
	if err != nil {
		return nil, err
	}
	records := make([]map[string]any, 0, len(toolEvents)+len(jobRuns)+len(heartbeatRuns))
	filterType := strings.TrimSpace(strings.ToLower(filter.Type))
	for _, event := range toolEvents {
		record := map[string]any{
			"type":      "tool",
			"name":      event.ToolName,
			"sessionId": event.SessionID,
			"summary":   truncateLine(event.Output, 180),
			"createdAt": event.CreatedAt.UTC().Format(time.RFC3339),
		}
		if includeAnalyticsRecord(record, filterType, filter.From, filter.To) {
			records = append(records, record)
		}
	}
	for _, run := range jobRuns {
		record := map[string]any{
			"type":      "cron",
			"jobId":     run["job_id"],
			"createdAt": toRFC3339(run["run_at"]),
			"summary":   truncateLine(fmt.Sprint(run["result"]), 180),
			"error":     fmt.Sprint(run["error"]),
		}
		if includeAnalyticsRecord(record, filterType, filter.From, filter.To) {
			records = append(records, record)
		}
	}
	for _, run := range heartbeatRuns {
		record := map[string]any{
			"type":      "heartbeat",
			"status":    run.Status,
			"createdAt": run.StartedAt.UTC().Format(time.RFC3339),
			"summary":   truncateLine(run.Preview, 180),
			"error":     run.Error,
		}
		if includeAnalyticsRecord(record, filterType, filter.From, filter.To) {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return fmt.Sprint(records[i]["createdAt"]) > fmt.Sprint(records[j]["createdAt"])
	})
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *MissionControlService) AnalyticsSummary(ctx context.Context, rangeName string) (map[string]any, error) {
	_ = ctx
	normalized := strings.TrimSpace(strings.ToLower(rangeName))
	days := 7
	if normalized == "30d" {
		days = 30
		normalized = "30d"
	} else {
		normalized = "7d"
	}
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -(days - 1))
	startDay := start.Format("2006-01-02")

	usageDays, err := s.store.ListUsageDays(context.Background())
	if err != nil {
		return nil, err
	}
	tokenTotal := uint64(0)
	tokenByDay := make(map[string]uint64, days)
	for _, usage := range usageDays {
		if usage.Day < startDay {
			continue
		}
		tokenTotal += usage.TotalTokens
		tokenByDay[usage.Day] = usage.TotalTokens
	}

	logs, err := s.AnalyticsLogsFiltered(context.Background(), AnalyticsLogFilter{
		From:  &start,
		To:    &now,
		Limit: 5000,
	})
	if err != nil {
		return nil, err
	}

	totals := map[string]int{
		"tool_events":     0,
		"cron_runs":       0,
		"heartbeat_runs":  0,
		"errors":          0,
		"provider_errors": int(s.metrics.Snapshot()["provider_errors"]),
		"tool_errors":     int(s.metrics.Snapshot()["tool_errors"]),
		"heartbeat_total": int(s.metrics.Snapshot()["heartbeat_executions"]),
		"cron_total":      int(s.metrics.Snapshot()["cron_executions"]),
	}
	for _, row := range logs {
		rowType := strings.TrimSpace(strings.ToLower(fmt.Sprint(row["type"])))
		switch rowType {
		case "tool":
			totals["tool_events"]++
		case "cron":
			totals["cron_runs"]++
		case "heartbeat":
			totals["heartbeat_runs"]++
		}
		if strings.TrimSpace(fmt.Sprint(row["error"])) != "" && strings.TrimSpace(fmt.Sprint(row["error"])) != "<nil>" {
			totals["errors"]++
		}
	}

	series := make([]map[string]any, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		series = append(series, map[string]any{
			"day":         day,
			"token_total": tokenByDay[day],
		})
	}
	return map[string]any{
		"range":      normalized,
		"from":       start.Format(time.RFC3339),
		"to":         now.Format(time.RFC3339),
		"totals":     totals,
		"tokenTotal": tokenTotal,
		"series":     series,
	}, nil
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
	out.Channels.Telegram.Enabled = cfg.Channels.Telegram.Enabled
	out.Channels.Telegram.TokenSet = strings.TrimSpace(cfg.Channels.Telegram.Token) != ""
	out.Channels.Telegram.AllowFrom = cfg.Channels.Telegram.AllowFrom
	if len(cfg.Channels.Scaffolds) > 0 {
		out.Channels.Scaffolds = make(map[string]config.GenericChannelConfig, len(cfg.Channels.Scaffolds))
		for id, scaffold := range cfg.Channels.Scaffolds {
			copyScaffold := scaffold
			copyScaffold.AuthToken = ""
			out.Channels.Scaffolds[id] = copyScaffold
		}
	}
	out.Runtime.HeartbeatIntervalSec = cfg.Runtime.HeartbeatIntervalSec
	out.Runtime.MailboxSize = cfg.Runtime.MailboxSize
	out.Management.Host = cfg.Management.Host
	out.Management.Port = cfg.Management.Port
	out.Management.PublicBaseURL = cfg.Management.PublicBaseURL
	out.Management.ServeInGateway = cfg.Management.ServeInGateway
	return out
}

func (s *MissionControlService) Channels() []ChannelDescriptor {
	cfg := s.getConfig()
	out := []ChannelDescriptor{
		{
			ID:              "telegram",
			Label:           "Telegram",
			Kind:            "telegram",
			RuntimeHotApply: true,
			Config: map[string]any{
				"enabled":   cfg.Channels.Telegram.Enabled,
				"tokenSet":  strings.TrimSpace(cfg.Channels.Telegram.Token) != "",
				"allowFrom": cfg.Channels.Telegram.AllowFrom,
			},
		},
	}
	for id, scaffold := range cfg.Channels.Scaffolds {
		copyScaffold := scaffold
		copyScaffold.AuthToken = ""
		out = append(out, ChannelDescriptor{
			ID:              id,
			Label:           strings.TrimSpace(copyScaffold.Label),
			Kind:            "scaffold",
			RuntimeHotApply: false,
			Scaffold:        &copyScaffold,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *MissionControlService) TestProvider(ctx context.Context, providerName string, draft config.ProviderConfig) error {
	if s.providerTest == nil {
		return fmt.Errorf("provider test is unavailable")
	}
	return s.providerTest(ctx, providerName, draft)
}

func (s *MissionControlService) UpdateProvider(ctx context.Context, providerName string, draft config.ProviderConfig, activate bool, remove bool) (map[string]any, error) {
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
		return map[string]any{"ok": true, "runtimeApplied": false, "restartRequired": false}, nil
	}

	if err := config.ValidateProviderDraft(normalized, draft); err != nil {
		return nil, err
	}
	_ = cfg.SetProviderByName(normalized, draft)

	runtimeApplied := false
	runtimeError := ""
	if activate {
		cfg.Providers.Active = normalized
		if err := config.ValidateActiveProvider(cfg); err != nil {
			return nil, err
		}
		if s.runtime != nil {
			if err := s.runtime.ApplyProvider(ctx, normalized, draft); err != nil {
				runtimeError = err.Error()
			} else {
				runtimeApplied = true
			}
		}
	}
	if err := s.saveConfig(cfg); err != nil {
		return nil, err
	}
	out := map[string]any{
		"ok":              true,
		"runtimeApplied":  runtimeApplied,
		"restartRequired": activate && !runtimeApplied,
	}
	if runtimeError != "" {
		out["runtimeError"] = runtimeError
	}
	return out, nil
}

func (s *MissionControlService) ActivateProvider(ctx context.Context, providerName string) (map[string]any, error) {
	cfg := s.getConfig()
	normalized, ok := config.NormalizeProviderName(providerName)
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", providerName)
	}
	draft, exists := cfg.ProviderByName(normalized)
	if !exists {
		return nil, fmt.Errorf("unsupported provider %q", providerName)
	}
	return s.UpdateProvider(ctx, normalized, draft, true, false)
}

func (s *MissionControlService) UpdateTelegram(ctx context.Context, telegram config.TelegramConfig) (map[string]any, error) {
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
	runtimeApplied := false
	runtimeError := ""
	if s.runtime != nil {
		if err := s.runtime.ApplyTelegram(ctx, telegram); err != nil {
			runtimeError = err.Error()
		} else {
			runtimeApplied = true
		}
	}
	out := map[string]any{
		"ok":              true,
		"runtimeApplied":  runtimeApplied,
		"restartRequired": !runtimeApplied,
	}
	if runtimeError != "" {
		out["runtimeError"] = runtimeError
	}
	return out, nil
}

func (s *MissionControlService) UpdateChannel(ctx context.Context, channelID string, telegramCfg *config.TelegramConfig, scaffoldCfg *config.GenericChannelConfig) (map[string]any, error) {
	normalized := strings.TrimSpace(strings.ToLower(channelID))
	switch normalized {
	case "telegram":
		if telegramCfg == nil {
			return nil, fmt.Errorf("telegram payload is required")
		}
		return s.UpdateTelegram(ctx, *telegramCfg)
	default:
		if scaffoldCfg == nil {
			return nil, fmt.Errorf("scaffold payload is required")
		}
		cfg := s.getConfig()
		if cfg.Channels.Scaffolds == nil {
			cfg.Channels.Scaffolds = map[string]config.GenericChannelConfig{}
		}
		id := strings.TrimSpace(channelID)
		if id == "" {
			return nil, fmt.Errorf("channel id is required")
		}
		cfg.Channels.Scaffolds[id] = *scaffoldCfg
		if err := s.saveConfig(cfg); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "runtimeApplied": false, "restartRequired": false}, nil
	}
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
	runtimeError := ""
	if heartbeatIntervalSec != nil && s.heartbeat != nil {
		s.heartbeat.SetInterval(time.Duration(*heartbeatIntervalSec) * time.Second)
		applied = true
	}
	out := map[string]any{
		"ok":              true,
		"runtimeApplied":  applied,
		"restartRequired": mailboxSize != nil || (heartbeatIntervalSec != nil && !applied),
	}
	if heartbeatIntervalSec != nil && !applied {
		runtimeError = "heartbeat runtime is offline"
	}
	if runtimeError != "" {
		out["runtimeError"] = runtimeError
	}
	return out, nil
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

func (s *MissionControlService) SnapshotExport(ctx context.Context) (map[string]any, error) {
	settings := s.Settings()
	board, err := s.Kanban(ctx)
	if err != nil {
		return nil, err
	}
	policy, err := s.TaskPolicy(ctx)
	if err != nil {
		return nil, err
	}
	usageDays, err := s.store.ListUsageDays(ctx)
	if err != nil {
		return nil, err
	}
	heartbeatRuns, err := s.store.ListHeartbeatRuns(ctx, 5000)
	if err != nil {
		return nil, err
	}
	logs, err := s.AnalyticsLogsFiltered(ctx, AnalyticsLogFilter{Limit: 1000})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"version":       1,
		"exportedAt":    time.Now().UTC().Format(time.RFC3339),
		"settings":      settings,
		"kanban":        map[string]any{"columns": board.Columns, "tasks": board.Tasks, "policy": policy},
		"usageDays":     usageDays,
		"heartbeatRuns": heartbeatRuns,
		"logs":          logs,
	}, nil
}

func (s *MissionControlService) SnapshotImport(ctx context.Context, payload map[string]any) (map[string]any, error) {
	type importedSettings struct {
		Providers struct {
			Active string `json:"active"`
			Items  []struct {
				ID      string `json:"id"`
				APIBase string `json:"apiBase"`
				Model   string `json:"model"`
			} `json:"items"`
		} `json:"providers"`
		Channels struct {
			Telegram struct {
				Enabled   bool     `json:"enabled"`
				AllowFrom []string `json:"allowFrom"`
			} `json:"telegram"`
			Scaffolds map[string]config.GenericChannelConfig `json:"scaffolds"`
		} `json:"channels"`
		Runtime struct {
			HeartbeatIntervalSec int `json:"heartbeatIntervalSec"`
			MailboxSize          int `json:"mailboxSize"`
		} `json:"runtime"`
	}
	type importedPayload struct {
		Version  int              `json:"version"`
		Settings importedSettings `json:"settings"`
		Kanban   struct {
			Columns []mission.Column             `json:"columns"`
			Tasks   []mission.Task               `json:"tasks"`
			Policy  mission.TaskAutomationPolicy `json:"policy"`
		} `json:"kanban"`
		UsageDays     []mission.UsageDay     `json:"usageDays"`
		HeartbeatRuns []mission.HeartbeatRun `json:"heartbeatRuns"`
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var in importedPayload
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("invalid snapshot payload: %w", err)
	}
	if in.Version <= 0 {
		return nil, fmt.Errorf("snapshot version is required")
	}

	cfg := s.getConfig()
	updatedProviders := 0
	for _, item := range in.Settings.Providers.Items {
		id, ok := config.NormalizeProviderName(item.ID)
		if !ok {
			continue
		}
		currentProvider, exists := cfg.ProviderByName(id)
		if !exists {
			continue
		}
		currentProvider.APIBase = strings.TrimSpace(item.APIBase)
		currentProvider.Model = strings.TrimSpace(item.Model)
		_ = cfg.SetProviderByName(id, currentProvider)
		updatedProviders++
	}
	if active, ok := config.NormalizeProviderName(in.Settings.Providers.Active); ok {
		cfg.Providers.Active = active
	}
	if in.Settings.Runtime.HeartbeatIntervalSec > 0 {
		cfg.Runtime.HeartbeatIntervalSec = in.Settings.Runtime.HeartbeatIntervalSec
	}
	if in.Settings.Runtime.MailboxSize > 0 {
		cfg.Runtime.MailboxSize = in.Settings.Runtime.MailboxSize
	}
	// Preserve telegram token and only merge non-secret values.
	cfg.Channels.Telegram.Enabled = in.Settings.Channels.Telegram.Enabled
	cfg.Channels.Telegram.AllowFrom = in.Settings.Channels.Telegram.AllowFrom
	if cfg.Channels.Scaffolds == nil {
		cfg.Channels.Scaffolds = map[string]config.GenericChannelConfig{}
	}
	for id, scaffold := range in.Settings.Channels.Scaffolds {
		existing := cfg.Channels.Scaffolds[id]
		scaffold.AuthToken = existing.AuthToken
		cfg.Channels.Scaffolds[id] = scaffold
	}

	if err := s.saveConfig(cfg); err != nil {
		return nil, err
	}

	importedColumns := 0
	if len(in.Kanban.Columns) > 0 {
		if _, err := s.SetColumns(ctx, in.Kanban.Columns); err != nil {
			return nil, err
		}
		importedColumns = len(in.Kanban.Columns)
	}

	importedTasks := 0
	for _, task := range in.Kanban.Tasks {
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Title) == "" {
			continue
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = time.Now().UTC()
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = task.CreatedAt
		}
		if task.Version <= 0 {
			task.Version = 1
		}
		if err := s.store.PutMissionTask(ctx, task); err != nil {
			return nil, err
		}
		importedTasks++
	}

	importedUsageDays := 0
	for _, usage := range in.UsageDays {
		if strings.TrimSpace(usage.Day) == "" {
			continue
		}
		if err := s.store.PutUsageDay(ctx, usage); err != nil {
			return nil, err
		}
		importedUsageDays++
	}

	importedHeartbeatRuns := 0
	for _, run := range in.HeartbeatRuns {
		if strings.TrimSpace(run.ID) == "" {
			run.ID = "hb-import-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
		}
		if run.StartedAt.IsZero() {
			run.StartedAt = time.Now().UTC()
		}
		if run.FinishedAt.IsZero() {
			run.FinishedAt = run.StartedAt
		}
		if err := s.store.RecordHeartbeatRun(ctx, run); err != nil {
			return nil, err
		}
		importedHeartbeatRuns++
	}

	importedPolicy := false
	if in.Kanban.Policy.DedupeWindowSec > 0 || strings.TrimSpace(in.Kanban.Policy.DefaultColumnID) != "" {
		if _, err := s.SetTaskPolicy(ctx, in.Kanban.Policy); err != nil {
			return nil, err
		}
		importedPolicy = true
	}

	runtimeApplied := false
	runtimeWarnings := []string{}
	activeProvider, activeCfg := cfg.PrimaryProvider()
	if s.runtime != nil && strings.TrimSpace(activeProvider) != "" {
		if err := s.runtime.ApplyProvider(ctx, activeProvider, activeCfg); err != nil {
			runtimeWarnings = append(runtimeWarnings, "provider apply: "+err.Error())
		} else {
			runtimeApplied = true
		}
		if err := s.runtime.ApplyTelegram(ctx, cfg.Channels.Telegram); err != nil {
			runtimeWarnings = append(runtimeWarnings, "telegram apply: "+err.Error())
		}
	}
	if s.heartbeat != nil && cfg.Runtime.HeartbeatIntervalSec > 0 {
		s.heartbeat.SetInterval(time.Duration(cfg.Runtime.HeartbeatIntervalSec) * time.Second)
		runtimeApplied = true
	}

	return map[string]any{
		"ok":                    true,
		"version":               in.Version,
		"runtimeApplied":        runtimeApplied,
		"updatedProviders":      updatedProviders,
		"importedColumns":       importedColumns,
		"importedTasks":         importedTasks,
		"importedPolicy":        importedPolicy,
		"importedUsageDays":     importedUsageDays,
		"importedHeartbeatRuns": importedHeartbeatRuns,
		"warnings":              runtimeWarnings,
	}, nil
}

func includeAnalyticsRecord(record map[string]any, filterType string, from, to *time.Time) bool {
	rowType := strings.TrimSpace(strings.ToLower(fmt.Sprint(record["type"])))
	if filterType != "" && rowType != filterType {
		return false
	}
	created := parseAnalyticsTime(fmt.Sprint(record["createdAt"]))
	if from != nil && created.Before(*from) {
		return false
	}
	if to != nil && created.After(*to) {
		return false
	}
	return true
}

func parseAnalyticsTime(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
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
