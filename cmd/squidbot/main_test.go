package main

import (
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grixate/squidbot/internal/config"
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
