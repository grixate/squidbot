package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/branch"
	"github.com/grixate/squidbot/internal/budget"
	"github.com/grixate/squidbot/internal/subagent"
)

func TestBranchTools(t *testing.T) {
	spawnTool := NewBranchSpawnTool(nil)
	if _, err := spawnTool.Execute(context.Background(), json.RawMessage(`{"prompt":"x"}`)); err == nil {
		t.Fatal("expected missing manager error")
	}

	var spawnReq BranchSpawnRequest
	spawnTool = NewBranchSpawnTool(func(ctx context.Context, req BranchSpawnRequest) (branch.BranchRun, error) {
		spawnReq = req
		return branch.BranchRun{ID: "b1", Status: branch.StatusSucceeded, Description: req.Description, Conclusion: "done", Model: "m"}, nil
	})
	spawnTool.SetContext("session-1")
	if _, err := spawnTool.Execute(context.Background(), json.RawMessage(`{"prompt":" "}`)); err == nil {
		t.Fatal("expected prompt required error")
	}
	res, err := spawnTool.Execute(context.Background(), json.RawMessage(`{"prompt":"analyze","description":"desc","wait":true,"timeout_sec":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if spawnReq.SessionID != "session-1" || !spawnReq.Wait || spawnReq.TimeoutSec != 5 {
		t.Fatalf("unexpected branch spawn request: %+v", spawnReq)
	}
	if !strings.Contains(res.Text, "Branch b1") {
		t.Fatalf("unexpected branch spawn text: %q", res.Text)
	}

	statusTool := NewBranchStatusTool(func(ctx context.Context, req BranchStatusRequest) (branch.BranchRun, error) {
		if req.BranchID == "" {
			return branch.BranchRun{}, errors.New("missing")
		}
		return branch.BranchRun{ID: req.BranchID, Status: branch.StatusRunning, Model: "m"}, nil
	})
	statusTool.SetContext("session-1")
	if _, err := statusTool.Execute(context.Background(), json.RawMessage(`{"branch_id":""}`)); err == nil {
		t.Fatal("expected branch id validation error")
	}
	if _, err := statusTool.Execute(context.Background(), json.RawMessage(`{"branch_id":"b1"}`)); err != nil {
		t.Fatal(err)
	}

	waitTool := NewBranchWaitTool(func(ctx context.Context, req BranchWaitRequest) (branch.BranchRun, error) {
		return branch.BranchRun{ID: req.BranchID, Status: branch.StatusFailed, Error: "boom", Model: "m"}, nil
	})
	waitTool.SetContext("session-1")
	result, err := waitTool.Execute(context.Background(), json.RawMessage(`{"branch_id":"b1","timeout_sec":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "Error: boom") {
		t.Fatalf("unexpected branch wait text: %q", result.Text)
	}
}

func TestBudgetTools(t *testing.T) {
	settings := budget.Settings{Enabled: true, Mode: budget.ModeHard, GlobalHardLimitTokens: 100, SessionHardLimitTokens: 50, SubagentRunHardLimitTokens: 25}

	statusTool := NewBudgetStatusTool(func(ctx context.Context, req BudgetStatusRequest) (BudgetStatusResponse, error) {
		if req.RunID != "run-1" {
			t.Fatalf("unexpected run id: %q", req.RunID)
		}
		return BudgetStatusResponse{Settings: settings, Scopes: []BudgetStatusScope{{Scope: "session", Used: 9, Reserved: 1}}}, nil
	})
	statusTool.SetContext("s", "c", "u")
	if _, err := statusTool.Execute(context.Background(), json.RawMessage(`{"run_id":`)); err == nil {
		t.Fatal("expected invalid json error")
	}
	statusRes, err := statusTool.Execute(context.Background(), json.RawMessage(`{"run_id":"run-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusRes.Text, "Token safety") {
		t.Fatalf("unexpected budget status text: %q", statusRes.Text)
	}

	enabledTool := NewBudgetSetEnabledTool(func(ctx context.Context, req BudgetSetEnabledRequest) (BudgetSetEnabledResponse, error) {
		settings.Enabled = req.Enabled
		return BudgetSetEnabledResponse{Settings: settings}, nil
	})
	enabledTool.SetContext("s", "c", "u")
	if _, err := enabledTool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected enabled validation error")
	}
	if _, err := enabledTool.Execute(context.Background(), json.RawMessage(`{"enabled":false}`)); err != nil {
		t.Fatal(err)
	}

	modeTool := NewBudgetSetModeTool(func(ctx context.Context, req BudgetSetModeRequest) (BudgetSetModeResponse, error) {
		settings.Mode = budget.Mode(strings.TrimSpace(req.Mode))
		return BudgetSetModeResponse{Settings: settings}, nil
	})
	modeTool.SetContext("s", "c", "u")
	if _, err := modeTool.Execute(context.Background(), json.RawMessage(`{"mode":""}`)); err == nil {
		t.Fatal("expected mode validation error")
	}
	if _, err := modeTool.Execute(context.Background(), json.RawMessage(`{"mode":"soft"}`)); err != nil {
		t.Fatal(err)
	}

	estimationTool := NewBudgetSetEstimationTool(func(ctx context.Context, req BudgetSetEstimationRequest) (BudgetSetEstimationResponse, error) {
		if req.EstimateOnMissingUsage == nil || req.EstimateCharsPerToken == nil {
			t.Fatalf("expected estimation pointers")
		}
		return BudgetSetEstimationResponse{Settings: settings}, nil
	})
	estimationTool.SetContext("s", "c", "u")
	if _, err := estimationTool.Execute(context.Background(), json.RawMessage(`{"estimate_on_missing_usage":true,"estimate_chars_per_token":4}`)); err != nil {
		t.Fatal(err)
	}

	limitsTool := NewBudgetSetLimitsTool(func(ctx context.Context, req BudgetSetLimitsRequest) (BudgetSetLimitsResponse, error) {
		if req.GlobalHardLimitTokens == nil || req.SessionHardLimitTokens == nil {
			t.Fatalf("expected limit pointers")
		}
		return BudgetSetLimitsResponse{Settings: settings}, nil
	})
	limitsTool.SetContext("s", "c", "u")
	_, err = limitsTool.Execute(context.Background(), json.RawMessage(`{"global_hard_limit_tokens":120,"global_soft_threshold_pct":90,"session_hard_limit_tokens":60,"session_soft_threshold_pct":85,"subagent_run_hard_limit_tokens":20,"subagent_run_soft_threshold_pct":70}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecallMemoryAndMessageTools(t *testing.T) {
	recallTool := NewChannelRecallTool(func(ctx context.Context, limit int) ([]string, error) {
		if limit != 12 {
			t.Fatalf("expected default limit=12, got %d", limit)
		}
		return nil, nil
	})
	res, err := recallTool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "No recent channel context found." {
		t.Fatalf("unexpected recall text: %q", res.Text)
	}

	memoryRecall := NewMemoryRecallTool(func(ctx context.Context, query string, limit int) ([]string, error) {
		if query != "q" || limit != 6 {
			t.Fatalf("unexpected memory recall args: query=%q limit=%d", query, limit)
		}
		return []string{"item1", "item2"}, nil
	})
	if _, err := memoryRecall.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected query validation error")
	}
	res, err = memoryRecall.Execute(context.Background(), json.RawMessage(`{"query":"q"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "item1") {
		t.Fatalf("unexpected memory recall text: %q", res.Text)
	}

	memorySave := NewMemorySaveTool(func(ctx context.Context, entry string) (string, error) {
		return "memory/daily/2026-02-18.md", nil
	})
	if _, err := memorySave.Execute(context.Background(), json.RawMessage(`{"entry":""}`)); err == nil {
		t.Fatal("expected entry validation error")
	}
	if _, err := memorySave.Execute(context.Background(), json.RawMessage(`{"entry":"remember"}`)); err != nil {
		t.Fatal(err)
	}

	memoryDelete := NewMemoryDeleteTool(func(ctx context.Context, query string) (int, error) {
		if query != "old" {
			t.Fatalf("unexpected delete query: %q", query)
		}
		return 2, nil
	})
	if _, err := memoryDelete.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected query validation error")
	}
	if _, err := memoryDelete.Execute(context.Background(), json.RawMessage(`{"query":"old"}`)); err != nil {
		t.Fatal(err)
	}

	messageTool := NewMessageTool(nil)
	if _, err := messageTool.Execute(context.Background(), json.RawMessage(`{"content":"hello"}`)); err == nil {
		t.Fatal("expected not configured error")
	}
	messageTool = NewMessageTool(func(ctx context.Context, channel, chatID, content string) error {
		if channel != "telegram" || chatID != "42" || content != "hello" {
			t.Fatalf("unexpected send args: %s %s %s", channel, chatID, content)
		}
		return nil
	})
	noTarget, err := messageTool.Execute(context.Background(), json.RawMessage(`{"content":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(noTarget.Text, "No target channel/chat specified") {
		t.Fatalf("expected no-target text, got %q", noTarget.Text)
	}
	messageTool.SetContext("telegram", "42", "session")
	if _, err := messageTool.Execute(context.Background(), json.RawMessage(`{"content":""}`)); err == nil {
		t.Fatal("expected content validation error")
	}
	msgRes, err := messageTool.Execute(context.Background(), json.RawMessage(`{"content":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msgRes.Text, "Message sent") {
		t.Fatalf("unexpected message result text: %q", msgRes.Text)
	}
}

func TestSubagentAndFederationTools(t *testing.T) {
	cancelTool := NewSubagentCancelTool(func(ctx context.Context, req SubagentCancelRequest) (SubagentCancelResponse, error) {
		if req.SessionID != "s" {
			t.Fatalf("unexpected session id: %q", req.SessionID)
		}
		return SubagentCancelResponse{RunID: req.RunID, Status: "cancelled"}, nil
	})
	cancelTool.SetContext("s")
	if _, err := cancelTool.Execute(context.Background(), json.RawMessage(`{"run_id":""}`)); err == nil {
		t.Fatal("expected run_id validation")
	}
	if _, err := cancelTool.Execute(context.Background(), json.RawMessage(`{"run_id":"r1"}`)); err != nil {
		t.Fatal(err)
	}

	statusTool := NewSubagentStatusTool(func(ctx context.Context, req SubagentStatusRequest) (SubagentStatusResponse, error) {
		return SubagentStatusResponse{Run: subagent.Run{ID: req.RunID, Status: subagent.StatusRunning, Attempt: 1}}, nil
	})
	statusTool.SetContext("s")
	if _, err := statusTool.Execute(context.Background(), json.RawMessage(`{"run_id":"r1"}`)); err != nil {
		t.Fatal(err)
	}

	resultTool := NewSubagentResultTool(func(ctx context.Context, req SubagentResultRequest) (SubagentResultResponse, error) {
		return SubagentResultResponse{Summary: "summary", Status: "succeeded", Attempt: 2, ArtifactPaths: []string{"a.txt"}}, nil
	})
	resultTool.SetContext("s")
	if _, err := resultTool.Execute(context.Background(), json.RawMessage(`{"run_id":"r1"}`)); err != nil {
		t.Fatal(err)
	}

	waitTool := NewSubagentWaitTool(func(ctx context.Context, req SubagentWaitRequest) (SubagentWaitResponse, error) {
		if req.TimeoutSec != 30 || req.SessionID != "s" {
			t.Fatalf("unexpected wait request: %+v", req)
		}
		return SubagentWaitResponse{Runs: []subagent.Run{{ID: "r1", Status: subagent.StatusSucceeded}}}, nil
	})
	waitTool.SetContext("s")
	if _, err := waitTool.Execute(context.Background(), json.RawMessage(`{"run_ids":[]}`)); err == nil {
		t.Fatal("expected run_ids validation")
	}
	if _, err := waitTool.Execute(context.Background(), json.RawMessage(`{"run_ids":["r1"],"timeout_sec":30}`)); err != nil {
		t.Fatal(err)
	}

	peerTool := NewFederationPeersTool(func(ctx context.Context, req FederationPeersRequest) (FederationPeersResponse, error) {
		return FederationPeersResponse{Peers: []FederationPeerInfo{{
			ID:            "peer-a",
			Enabled:       true,
			BaseURL:       "http://peer",
			Capabilities:  []string{"code"},
			Roles:         []string{"worker"},
			Priority:      1,
			MaxConcurrent: 4,
			MaxQueue:      8,
			Available:     true,
			QueueDepth:    1,
			ActiveRuns:    2,
			UpdatedAt:     time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC),
		}}}, nil
	})
	if _, err := peerTool.Execute(context.Background(), json.RawMessage(`{"bad":`)); err == nil {
		t.Fatal("expected invalid argument error")
	}
	res, err := peerTool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "peer-a") {
		t.Fatalf("unexpected federation peers text: %q", res.Text)
	}
}
