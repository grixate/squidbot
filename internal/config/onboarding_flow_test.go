package config

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunOnboardingInteractiveGeminiFlashWithVerification(t *testing.T) {
	t.Setenv("SQUIDBOT_TEST_GEMINI_KEY", "sk-gemini")
	input := strings.NewReader("4\nenv:SQUIDBOT_TEST_GEMINI_KEY\n\n2\nn\ny\n")
	var output strings.Builder
	runCalled := false

	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		In:  input,
		Out: &output,
		LookPath: func(file string) (string, error) {
			if file == "gemini" {
				return "/usr/bin/gemini", nil
			}
			return "", errors.New("not found")
		},
		RunCommand: func(_ context.Context, name string, args []string, env map[string]string) (string, error) {
			runCalled = true
			if name != "gemini" {
				t.Fatalf("unexpected command: %s", name)
			}
			if len(args) != 6 {
				t.Fatalf("unexpected args length: %d", len(args))
			}
			if args[0] != "-p" || args[2] != "--model" || args[4] != "--output-format" {
				t.Fatalf("unexpected args: %#v", args)
			}
			if args[3] != "gemini-3.0-flash" {
				t.Fatalf("unexpected model arg: %s", args[3])
			}
			if env["GEMINI_API_KEY"] != "sk-gemini" {
				t.Fatalf("unexpected api key in env")
			}
			return `{"status":"ok"}`, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Provider != ProviderGemini {
		t.Fatalf("unexpected provider: %s", result.Provider)
	}
	if result.Config.Providers.Active != ProviderGemini {
		t.Fatalf("unexpected active provider: %s", result.Config.Providers.Active)
	}
	if result.Config.Providers.Gemini.Model != "gemini-3.0-flash" {
		t.Fatalf("unexpected gemini model: %s", result.Config.Providers.Gemini.Model)
	}
	if result.Config.Providers.Gemini.APIBase != ProviderDefaultAPIBase(ProviderGemini) {
		t.Fatalf("unexpected gemini api base: %s", result.Config.Providers.Gemini.APIBase)
	}
	if !result.GeminiCLIVerified {
		t.Fatalf("expected gemini CLI verification success")
	}
	if !result.GeminiCLIVerifyRan {
		t.Fatalf("expected gemini CLI verification to run")
	}
	if !runCalled {
		t.Fatalf("expected gemini command to be executed")
	}
}

func TestRunOnboardingNonInteractiveRequiresInputs(t *testing.T) {
	_, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:       ProviderGemini,
		NonInteractive: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires api key ref") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOnboardingNonInteractiveOllamaModelRequired(t *testing.T) {
	_, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:       ProviderOllama,
		NonInteractive: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires model") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOnboardingNonInteractiveOllamaSuccess(t *testing.T) {
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:       ProviderOllama,
		Model:          "llama3.1:8b",
		NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Providers.Active != ProviderOllama {
		t.Fatalf("unexpected active provider: %s", result.Config.Providers.Active)
	}
	if result.Config.Providers.Ollama.APIBase != ProviderDefaultAPIBase(ProviderOllama) {
		t.Fatalf("unexpected ollama api base: %s", result.Config.Providers.Ollama.APIBase)
	}
}

func TestRunOnboardingNonInteractiveMoonshotSuccess(t *testing.T) {
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:       "moonshot",
		APIKeyRef:      "sk-test",
		NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Providers.Active != "moonshot" {
		t.Fatalf("unexpected active provider: %s", result.Config.Providers.Active)
	}
	providerCfg, _ := result.Config.ProviderByName("moonshot")
	if providerCfg.APIBase != ProviderDefaultAPIBase("moonshot") {
		t.Fatalf("unexpected moonshot api base: %s", providerCfg.APIBase)
	}
}

func TestRunOnboardingNonInteractiveMiniMaxSuccess(t *testing.T) {
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:       "minimax",
		APIKeyRef:      "sk-test",
		NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Providers.Active != "minimax" {
		t.Fatalf("unexpected active provider: %s", result.Config.Providers.Active)
	}
	providerCfg, _ := result.Config.ProviderByName("minimax")
	if providerCfg.APIBase != ProviderDefaultAPIBase("minimax") {
		t.Fatalf("unexpected minimax api base: %s", providerCfg.APIBase)
	}
}

func TestRunOnboardingNonInteractiveOpenAICodexWarnsWhenUnauthed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:       ProviderOpenAICodex,
		NonInteractive: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Providers.Active != ProviderOpenAICodex {
		t.Fatalf("unexpected provider: %s", result.Config.Providers.Active)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "provider login openai-codex") {
		t.Fatalf("expected codex login warning, got %#v", result.Warnings)
	}
}

func TestRunOnboardingGeminiVerificationWarningInteractive(t *testing.T) {
	input := strings.NewReader("4\nsk-gemini\n\n1\nn\ny\n")
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		In:  input,
		Out: &strings.Builder{},
		LookPath: func(file string) (string, error) {
			return "/usr/bin/gemini", nil
		},
		RunCommand: func(_ context.Context, name string, args []string, env map[string]string) (string, error) {
			return "", errors.New("verification failed")
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warning for failed verification")
	}
	if result.GeminiCLIVerified {
		t.Fatal("verification should not be marked as passed")
	}
}

func TestRunOnboardingGeminiVerificationStrictNonInteractive(t *testing.T) {
	_, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:        ProviderGemini,
		APIKeyRef:       "sk-gemini",
		Model:           "gemini-3.0-pro",
		NonInteractive:  true,
		VerifyGeminiCLI: true,
		LookPath: func(file string) (string, error) {
			return "/usr/bin/gemini", nil
		},
		RunCommand: func(_ context.Context, name string, args []string, env map[string]string) (string, error) {
			return "", errors.New("verification failed")
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunOnboardingInteractiveGeminiCustomModel(t *testing.T) {
	input := strings.NewReader("4\nsk-gemini\n\n3\ngemini-3.0-custom\nn\nn\n")
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		In:  input,
		Out: &strings.Builder{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Providers.Gemini.Model != "gemini-3.0-custom" {
		t.Fatalf("unexpected model: %s", result.Config.Providers.Gemini.Model)
	}
	if result.GeminiCLIVerifyRan {
		t.Fatal("did not expect verification to run")
	}
}

func TestRunOnboardingInteractiveTelegramConfig(t *testing.T) {
	input := strings.NewReader("5\n\n\n\ny\nbot-token\n123,@Alice,@alice,456\n")
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		In:  input,
		Out: &strings.Builder{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Config.Channels.Telegram.Enabled {
		t.Fatal("expected telegram to be enabled")
	}
	if result.Config.Channels.Telegram.TokenRef != "bot-token" {
		t.Fatalf("unexpected telegram token: %q", result.Config.Channels.Telegram.TokenRef)
	}
	wantAllow := []string{"123", "@Alice", "456"}
	if !reflect.DeepEqual(result.Config.Channels.Telegram.AllowFrom, wantAllow) {
		t.Fatalf("unexpected allow list: %#v", result.Config.Channels.Telegram.AllowFrom)
	}
}

func TestRunOnboardingInteractiveTelegramEnabledRequiresToken(t *testing.T) {
	input := strings.NewReader("5\n\n\n\ny\n\n\n")
	_, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		In:  input,
		Out: &strings.Builder{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "telegram enabled requires tokenRef") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOnboardingInteractiveTelegramDisablePreservesTokenAndAllowList(t *testing.T) {
	cfg := Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.TokenRef = "existing-token"
	cfg.Channels.Telegram.AllowFrom = []string{"123", "@user"}

	input := strings.NewReader("\n\n\nn\n")
	result, err := RunOnboarding(context.Background(), cfg, OnboardingOptions{
		Provider: ProviderOllama,
		In:       input,
		Out:      &strings.Builder{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Channels.Telegram.Enabled {
		t.Fatal("expected telegram to be disabled")
	}
	if result.Config.Channels.Telegram.TokenRef != "existing-token" {
		t.Fatalf("unexpected token: %q", result.Config.Channels.Telegram.TokenRef)
	}
	wantAllow := []string{"123", "@user"}
	if !reflect.DeepEqual(result.Config.Channels.Telegram.AllowFrom, wantAllow) {
		t.Fatalf("unexpected allow list: %#v", result.Config.Channels.Telegram.AllowFrom)
	}
}

func TestRunOnboardingNonInteractiveTelegramEnabledRequiresToken(t *testing.T) {
	_, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:           ProviderOllama,
		Model:              "llama3.1:8b",
		NonInteractive:     true,
		TelegramEnabledSet: true,
		TelegramEnabled:    true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "telegram enabled requires tokenRef") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunOnboardingNonInteractiveTelegramExplicitOverrides(t *testing.T) {
	result, err := RunOnboarding(context.Background(), Default(), OnboardingOptions{
		Provider:             ProviderOllama,
		Model:                "llama3.1:8b",
		NonInteractive:       true,
		TelegramEnabledSet:   true,
		TelegramEnabled:      true,
		TelegramTokenRefSet:  true,
		TelegramTokenRef:     "bot-token",
		TelegramAllowFromSet: true,
		TelegramAllowFrom:    []string{"123,@alice", "@Alice", "456", ""},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Config.Channels.Telegram.Enabled {
		t.Fatal("expected telegram to be enabled")
	}
	if result.Config.Channels.Telegram.TokenRef != "bot-token" {
		t.Fatalf("unexpected token: %q", result.Config.Channels.Telegram.TokenRef)
	}
	wantAllow := []string{"123", "@alice", "456"}
	if !reflect.DeepEqual(result.Config.Channels.Telegram.AllowFrom, wantAllow) {
		t.Fatalf("unexpected allow list: %#v", result.Config.Channels.Telegram.AllowFrom)
	}
}
