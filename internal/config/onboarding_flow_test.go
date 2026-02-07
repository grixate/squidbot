package config

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunOnboardingInteractiveGeminiFlashWithVerification(t *testing.T) {
	input := strings.NewReader("4\nsk-gemini\n\n2\ny\n")
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
	if !strings.Contains(err.Error(), "requires api key") {
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

func TestRunOnboardingGeminiVerificationWarningInteractive(t *testing.T) {
	input := strings.NewReader("4\nsk-gemini\n\n1\ny\n")
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
		APIKey:          "sk-gemini",
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
	input := strings.NewReader("4\nsk-gemini\n\n3\ngemini-3.0-custom\nn\n")
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
