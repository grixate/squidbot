package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/budget"
	"github.com/grixate/squidbot/internal/config"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/subagent"
)

func writeTestConfig(t *testing.T, cfg config.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	return path
}

func baseTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = filepath.Join(t.TempDir(), "workspace")
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "squidbot.db")
	return cfg
}

func TestAgentCommandRequiresProviderSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := writeTestConfig(t, baseTestConfig(t))
	cmd := agentCmd(configPath, log.New(io.Discard, "", 0))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"-m", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "provider setup incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayCommandRequiresProviderSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := writeTestConfig(t, baseTestConfig(t))
	cmd := gatewayCmd(configPath, log.New(io.Discard, "", 0))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "provider setup incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCronRunCommandRequiresProviderSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := writeTestConfig(t, baseTestConfig(t))
	cmd := cronCmd(configPath, log.New(io.Discard, "", 0))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"run", "job-1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "provider setup incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOnboardStatusDoctorCommandsRemainRunnable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := baseTestConfig(t)
	configPath := writeTestConfig(t, cfg)

	onboard := onboardCmd(configPath)
	onboard.SilenceUsage = true
	onboard.SilenceErrors = true
	onboard.SetOut(io.Discard)
	onboard.SetErr(io.Discard)
	onboard.SetArgs([]string{
		"--non-interactive",
		"--provider", "gemini",
		"--api-key", "sk-gemini",
		"--model", "gemini-3.0-pro",
	})
	if err := onboard.Execute(); err != nil {
		t.Fatalf("onboard should succeed: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if loaded.Providers.Active != config.ProviderGemini {
		t.Fatalf("unexpected active provider: %s", loaded.Providers.Active)
	}

	status := statusCmd(configPath)
	status.SilenceUsage = true
	status.SilenceErrors = true
	status.SetOut(io.Discard)
	status.SetErr(io.Discard)
	if err := status.Execute(); err != nil {
		t.Fatalf("status should run: %v", err)
	}

	invalidConfigPath := writeTestConfig(t, baseTestConfig(t))
	doctor := doctorCmd(invalidConfigPath)
	doctor.SilenceUsage = true
	doctor.SilenceErrors = true
	doctor.SetOut(io.Discard)
	doctor.SetErr(io.Discard)
	err = doctor.Execute()
	if err == nil {
		t.Fatal("doctor should report configuration issues")
	}
	if !strings.Contains(err.Error(), "doctor checks failed") {
		t.Fatalf("unexpected doctor error: %v", err)
	}
}

func TestRootCommandDoesNotPrintBannerOnNoArgs(t *testing.T) {
	cmd := newRootCmd(log.New(io.Discard, "", 0))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("root command should run: %v", err)
	}
	output := out.String()
	if strings.Contains(output, ".oooo.o") {
		t.Fatalf("did not expect banner output, got: %q", output)
	}
	if !strings.Contains(output, "Usage:") {
		t.Fatalf("expected root help output, got: %q", output)
	}
}

func TestOnboardCommandPrintsBanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := writeTestConfig(t, baseTestConfig(t))

	onboard := onboardCmd(configPath)
	onboard.SilenceUsage = true
	onboard.SilenceErrors = true
	var out bytes.Buffer
	onboard.SetOut(&out)
	onboard.SetErr(io.Discard)
	onboard.SetArgs([]string{
		"--non-interactive",
		"--provider", "gemini",
		"--api-key", "sk-gemini",
		"--model", "gemini-3.0-pro",
	})

	if err := onboard.Execute(); err != nil {
		t.Fatalf("onboard should succeed: %v", err)
	}
	if !strings.Contains(out.String(), ".oooo.o") {
		t.Fatalf("expected banner output for onboard, got: %q", out.String())
	}
}

func TestOnboardCommandPersistsTelegramFlagsNonInteractive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := writeTestConfig(t, baseTestConfig(t))

	onboard := onboardCmd(configPath)
	onboard.SilenceUsage = true
	onboard.SilenceErrors = true
	onboard.SetOut(io.Discard)
	onboard.SetErr(io.Discard)
	onboard.SetArgs([]string{
		"--non-interactive",
		"--provider", "gemini",
		"--api-key", "sk-gemini",
		"--model", "gemini-3.0-pro",
		"--telegram-enabled",
		"--telegram-token", "bot-token",
		"--telegram-allow-from", "123,@alice",
		"--telegram-allow-from", "@Alice",
	})

	if err := onboard.Execute(); err != nil {
		t.Fatalf("onboard should succeed: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if !loaded.Channels.Telegram.Enabled {
		t.Fatal("expected telegram to be enabled")
	}
	if loaded.Channels.Telegram.Token != "bot-token" {
		t.Fatalf("unexpected telegram token: %q", loaded.Channels.Telegram.Token)
	}
	wantAllow := []string{"123", "@alice"}
	if !reflect.DeepEqual(loaded.Channels.Telegram.AllowFrom, wantAllow) {
		t.Fatalf("unexpected telegram allow list: %#v", loaded.Channels.Telegram.AllowFrom)
	}
}

func TestRootCommandDoesNotPrintBannerOnRootHelp(t *testing.T) {
	cmd := newRootCmd(log.New(io.Discard, "", 0))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("root help should run: %v", err)
	}
	if strings.Contains(out.String(), ".oooo.o") {
		t.Fatalf("did not expect banner output for root help, got: %q", out.String())
	}
}

func TestRootCommandDoesNotPrintBannerForSubcommand(t *testing.T) {
	cmd := newRootCmd(log.New(io.Discard, "", 0))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"doctor", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("subcommand help should run: %v", err)
	}
	if strings.Contains(out.String(), ".oooo.o") {
		t.Fatalf("did not expect banner output for subcommand, got: %q", out.String())
	}
}

func TestRootCommandDoesNotExposeManage(t *testing.T) {
	cmd := newRootCmd(log.New(io.Discard, "", 0))
	for _, sub := range cmd.Commands() {
		if sub.Name() == "manage" {
			t.Fatal("expected manage command to be removed")
		}
	}
}

func TestGatewayCommandDoesNotExposeWithManageFlag(t *testing.T) {
	cmd := gatewayCmd("", log.New(io.Discard, "", 0))
	if cmd.Flags().Lookup("with-manage") != nil {
		t.Fatal("expected --with-manage flag to be removed")
	}
}

func TestSubagentsCancelWritesExternalCancelSignal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := baseTestConfig(t)
	configPath := writeTestConfig(t, cfg)
	store, err := storepkg.Open(cfg.Storage.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	run := subagent.Run{
		ID:          "run-cancel-signal",
		SessionID:   "cli:default",
		Channel:     "cli",
		ChatID:      "direct",
		Task:        "long task",
		Status:      subagent.StatusRunning,
		CreatedAt:   now,
		StartedAt:   &now,
		TimeoutSec:  30,
		MaxAttempts: 1,
	}
	if err := store.PutSubagentRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := subagentsCmd(configPath)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cancel", "run-cancel-signal"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cancel command failed: %v", err)
	}

	store, err = storepkg.Open(cfg.Storage.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	updated, err := store.GetSubagentRun(ctx, "run-cancel-signal")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != subagent.StatusCancelled {
		t.Fatalf("expected cancelled status, got %s", updated.Status)
	}
	if _, err := store.GetKV(ctx, subagent.CancelSignalNamespace, "run-cancel-signal"); err != nil {
		t.Fatalf("expected cancel signal in kv: %v", err)
	}
}

func TestBudgetCommandsPersistOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := baseTestConfig(t)
	configPath := writeTestConfig(t, cfg)

	setMode := budgetCmd(configPath)
	setMode.SilenceUsage = true
	setMode.SilenceErrors = true
	setMode.SetOut(io.Discard)
	setMode.SetErr(io.Discard)
	setMode.SetArgs([]string{"set-mode", "hard"})
	if err := setMode.Execute(); err != nil {
		t.Fatalf("set-mode command failed: %v", err)
	}

	disable := budgetCmd(configPath)
	disable.SilenceUsage = true
	disable.SilenceErrors = true
	disable.SetOut(io.Discard)
	disable.SetErr(io.Discard)
	disable.SetArgs([]string{"disable"})
	if err := disable.Execute(); err != nil {
		t.Fatalf("disable command failed: %v", err)
	}

	store, err := storepkg.Open(cfg.Storage.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	override, err := store.GetTokenSafetyOverride(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if override.Settings.Mode != budget.ModeHard {
		t.Fatalf("expected hard mode override, got %s", override.Settings.Mode)
	}
	if override.Settings.Enabled {
		t.Fatal("expected token safety to be disabled by override")
	}
}

func TestOnboardCommandDoesNotExposeWebMode(t *testing.T) {
	cmd := onboardCmd("")
	if cmd.Flags().Lookup("mode") != nil {
		t.Fatal("expected --mode flag to be removed")
	}
}
