package config

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/grixate/squidbot/internal/catalog"
)

type OnboardingOptions struct {
	Provider             string
	APIKey               string
	APIBase              string
	Model                string
	NonInteractive       bool
	VerifyGeminiCLI      bool
	TelegramEnabledSet   bool
	TelegramEnabled      bool
	TelegramTokenSet     bool
	TelegramToken        string
	TelegramAllowFromSet bool
	TelegramAllowFrom    []string
	ChannelEnabledIDs    []string
	ChannelEndpoints     []string
	ChannelAuthTokens    []string

	In  io.Reader
	Out io.Writer

	LookPath   func(file string) (string, error)
	RunCommand func(ctx context.Context, name string, args []string, env map[string]string) (string, error)
}

type OnboardingResult struct {
	Config             Config
	Provider           string
	Warnings           []string
	GeminiCLIVerified  bool
	GeminiCLIVerifyRan bool
}

func RunOnboarding(ctx context.Context, cfg Config, opts OnboardingOptions) (OnboardingResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	reader := bufio.NewReader(readerOrStdin(opts.In))

	providerName, providerCfg, err := resolveOnboardingProvider(cfg, opts, reader, out)
	if err != nil {
		return OnboardingResult{}, err
	}

	providerCfg = applyExplicitOnboardingOverrides(providerCfg, opts)
	if err := fillOnboardingProviderConfig(providerName, &providerCfg, opts, reader, out); err != nil {
		return OnboardingResult{}, err
	}

	if err := fillOnboardingTelegramConfig(&cfg, opts, reader, out); err != nil {
		return OnboardingResult{}, err
	}

	nextCfg, err := ApplyOnboardingInput(cfg, OnboardingInput{
		Provider:       providerName,
		ProviderConfig: providerCfg,
		Telegram:       cfg.Channels.Telegram,
	})
	if err != nil {
		return OnboardingResult{}, err
	}
	cfg = nextCfg

	result := OnboardingResult{
		Config:   cfg,
		Provider: providerName,
	}
	shouldVerify, err := shouldRunGeminiVerification(providerName, opts, reader, out)
	if err != nil {
		return OnboardingResult{}, err
	}
	if shouldVerify {
		result.GeminiCLIVerifyRan = true
		if err := verifyGeminiCLI(ctx, providerCfg, opts); err != nil {
			if opts.NonInteractive && opts.VerifyGeminiCLI {
				return OnboardingResult{}, err
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("Gemini CLI verification failed: %v", err))
		} else {
			result.GeminiCLIVerified = true
		}
	}

	return result, nil
}

func resolveOnboardingProvider(cfg Config, opts OnboardingOptions, reader *bufio.Reader, out io.Writer) (string, ProviderConfig, error) {
	selected := strings.TrimSpace(opts.Provider)
	if selected == "" {
		if opts.NonInteractive {
			selected = strings.TrimSpace(cfg.Providers.Active)
			if selected == "" {
				return "", ProviderConfig{}, fmt.Errorf("non-interactive onboarding requires --provider")
			}
		} else {
			p, err := promptProviderSelection(reader, out)
			if err != nil {
				return "", ProviderConfig{}, err
			}
			selected = p
		}
	}

	normalized, ok := NormalizeProviderName(selected)
	if !ok {
		return "", ProviderConfig{}, fmt.Errorf("unsupported provider %q (supported: %s)", selected, strings.Join(SupportedProviders(), ", "))
	}
	current, _ := cfg.ProviderByName(normalized)
	return normalized, current, nil
}

func applyExplicitOnboardingOverrides(providerCfg ProviderConfig, opts OnboardingOptions) ProviderConfig {
	if value := strings.TrimSpace(opts.APIKey); value != "" {
		providerCfg.APIKey = value
	}
	if value := strings.TrimSpace(opts.APIBase); value != "" {
		providerCfg.APIBase = value
	}
	if value := strings.TrimSpace(opts.Model); value != "" {
		providerCfg.Model = value
	}
	return providerCfg
}

func fillOnboardingProviderConfig(providerName string, providerCfg *ProviderConfig, opts OnboardingOptions, reader *bufio.Reader, out io.Writer) error {
	requiredAPIKey, requiredModel, _ := ProviderRequirements(providerName)

	if opts.NonInteractive {
		if requiredAPIKey && strings.TrimSpace(providerCfg.APIKey) == "" {
			return fmt.Errorf("provider %q requires api key in non-interactive mode (--api-key)", providerName)
		}
		if requiredModel && strings.TrimSpace(providerCfg.Model) == "" {
			return fmt.Errorf("provider %q requires model in non-interactive mode (--model)", providerName)
		}
		return nil
	}

	if requiredAPIKey {
		value, err := promptLine(reader, out, fmt.Sprintf("Enter %s API key", providerLabel(providerName)), providerCfg.APIKey)
		if err != nil {
			return err
		}
		providerCfg.APIKey = strings.TrimSpace(value)
		if providerCfg.APIKey == "" {
			return fmt.Errorf("provider %q requires api key", providerName)
		}
	} else {
		value, err := promptLine(reader, out, fmt.Sprintf("Enter %s API key (optional)", providerLabel(providerName)), providerCfg.APIKey)
		if err != nil {
			return err
		}
		providerCfg.APIKey = strings.TrimSpace(value)
	}

	if defaultBase := ProviderDefaultAPIBase(providerName); defaultBase != "" {
		value, err := promptLine(reader, out, fmt.Sprintf("API base URL for %s", providerLabel(providerName)), defaultString(providerCfg.APIBase, defaultBase))
		if err != nil {
			return err
		}
		providerCfg.APIBase = strings.TrimSpace(value)
	}

	if providerName == ProviderGemini {
		model, err := promptGeminiModel(reader, out, providerCfg.Model)
		if err != nil {
			return err
		}
		providerCfg.Model = model
	} else if requiredModel {
		modelDefault := defaultString(providerCfg.Model, ProviderDefaultModel(providerName))
		value, err := promptLine(reader, out, fmt.Sprintf("Model for %s", providerLabel(providerName)), modelDefault)
		if err != nil {
			return err
		}
		providerCfg.Model = strings.TrimSpace(value)
		if providerCfg.Model == "" {
			return fmt.Errorf("provider %q requires model", providerName)
		}
	} else if providerName == ProviderOpenAI {
		value, err := promptLine(reader, out, "Model for OpenAI (optional)", providerCfg.Model)
		if err != nil {
			return err
		}
		providerCfg.Model = strings.TrimSpace(value)
	}

	return nil
}

func fillOnboardingTelegramConfig(cfg *Config, opts OnboardingOptions, reader *bufio.Reader, out io.Writer) error {
	if opts.NonInteractive {
		applyExplicitTelegramOnboardingOverrides(cfg, opts)
		applyExplicitChannelOverrides(cfg, opts)
		migrateLegacyChannels(cfg)
		return nil
	}

	defaultEnabled := cfg.Channels.Telegram.Enabled
	if opts.TelegramEnabledSet {
		defaultEnabled = opts.TelegramEnabled
	}
	enabled, err := promptYesNo(reader, out, "Enable Telegram channel?", defaultEnabled)
	if err != nil {
		return err
	}
	if !enabled {
		cfg.Channels.Telegram.Enabled = false
		return nil
	}

	tokenDefault := cfg.Channels.Telegram.Token
	if opts.TelegramTokenSet {
		tokenDefault = strings.TrimSpace(opts.TelegramToken)
	}
	token, err := promptLine(reader, out, "Telegram bot token", tokenDefault)
	if err != nil {
		return err
	}

	allowDefaultValues := cfg.Channels.Telegram.AllowFrom
	if opts.TelegramAllowFromSet {
		allowDefaultValues = normalizeAllowFrom(opts.TelegramAllowFrom)
	}
	allowListPromptDefault := strings.Join(allowDefaultValues, ",")
	allowListInput, err := promptLine(reader, out, "Telegram allow list (comma-separated, optional)", allowListPromptDefault)
	if err != nil {
		return err
	}

	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = strings.TrimSpace(token)
	cfg.Channels.Telegram.AllowFrom = normalizeAllowFrom([]string{allowListInput})
	migrateLegacyChannels(cfg)
	return nil
}

func applyExplicitTelegramOnboardingOverrides(cfg *Config, opts OnboardingOptions) {
	if opts.TelegramEnabledSet {
		cfg.Channels.Telegram.Enabled = opts.TelegramEnabled
	}
	if opts.TelegramTokenSet {
		cfg.Channels.Telegram.Token = strings.TrimSpace(opts.TelegramToken)
	}
	if opts.TelegramAllowFromSet {
		cfg.Channels.Telegram.AllowFrom = normalizeAllowFrom(opts.TelegramAllowFrom)
	}
	migrateLegacyChannels(cfg)
}

func applyExplicitChannelOverrides(cfg *Config, opts OnboardingOptions) {
	if cfg.Channels.Registry == nil {
		cfg.Channels.Registry = map[string]GenericChannelConfig{}
	}
	for _, raw := range opts.ChannelEnabledIDs {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		current := cfg.Channels.Registry[id]
		current.Enabled = true
		cfg.Channels.Registry[id] = current
	}
	for _, raw := range opts.ChannelEndpoints {
		id, value, ok := splitKV(raw)
		if !ok {
			continue
		}
		current := cfg.Channels.Registry[id]
		current.Endpoint = value
		cfg.Channels.Registry[id] = current
	}
	for _, raw := range opts.ChannelAuthTokens {
		id, value, ok := splitKV(raw)
		if !ok {
			continue
		}
		current := cfg.Channels.Registry[id]
		current.AuthToken = value
		cfg.Channels.Registry[id] = current
	}
}

func validateTelegramOnboarding(cfg Config) error {
	migrateLegacyChannels(&cfg)
	for channelID, channel := range cfg.Channels.Registry {
		if !channel.Enabled {
			continue
		}
		if channelID == "telegram" && strings.TrimSpace(channel.Token) == "" {
			return fmt.Errorf("telegram enabled requires token")
		}
		profile, ok := ChannelProfile(channelID)
		if ok && profile.Kind == "plugin" && strings.TrimSpace(channel.Endpoint) == "" {
			return fmt.Errorf("channel %q requires endpoint", channelID)
		}
	}
	return nil
}

func shouldRunGeminiVerification(providerName string, opts OnboardingOptions, reader *bufio.Reader, out io.Writer) (bool, error) {
	if providerName != ProviderGemini {
		return false, nil
	}
	if opts.VerifyGeminiCLI {
		return true, nil
	}
	if opts.NonInteractive {
		return false, nil
	}

	return promptYesNo(reader, out, "Verify Gemini CLI connectivity now?", false)
}

func verifyGeminiCLI(ctx context.Context, providerCfg ProviderConfig, opts OnboardingOptions) error {
	if strings.TrimSpace(providerCfg.APIKey) == "" {
		return fmt.Errorf("cannot verify Gemini CLI without api key")
	}

	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if _, err := lookPath("gemini"); err != nil {
		return fmt.Errorf("gemini CLI not found in PATH")
	}

	runCommand := opts.RunCommand
	if runCommand == nil {
		runCommand = defaultRunCommand
	}

	model := strings.TrimSpace(providerCfg.Model)
	if model == "" {
		model = ProviderDefaultModel(ProviderGemini)
	}
	_, err := runCommand(ctx, "gemini", []string{"-p", "Reply with OK", "--model", model, "--output-format", "json"}, map[string]string{
		"GEMINI_API_KEY": providerCfg.APIKey,
	})
	if err != nil {
		return err
	}
	return nil
}

func defaultRunCommand(ctx context.Context, name string, args []string, env map[string]string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("%w: %s", err, text)
		}
		return "", err
	}
	return text, nil
}

func promptProviderSelection(reader *bufio.Reader, out io.Writer) (string, error) {
	type item struct {
		Name  string
		Label string
	}
	preferredOrder := []string{
		ProviderOpenRouter,
		ProviderAnthropic,
		ProviderOpenAI,
		ProviderGemini,
		ProviderOllama,
		ProviderLMStudio,
	}
	seen := map[string]struct{}{}
	providers := make([]string, 0, len(SupportedProviders()))
	for _, providerID := range preferredOrder {
		if _, ok := seen[providerID]; ok {
			continue
		}
		seen[providerID] = struct{}{}
		providers = append(providers, providerID)
	}
	for _, providerID := range SupportedProviders() {
		if _, ok := seen[providerID]; ok {
			continue
		}
		seen[providerID] = struct{}{}
		providers = append(providers, providerID)
	}
	items := make([]item, 0, len(providers))
	for _, providerID := range providers {
		items = append(items, item{Name: providerID, Label: providerLabel(providerID)})
	}
	fmt.Fprintln(out, "Choose a provider:")
	for idx, it := range items {
		fmt.Fprintf(out, "  %d) %s\n", idx+1, it.Label)
	}

	for {
		fmt.Fprintf(out, "Provider [1-%d]: ", len(items))
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if num, convErr := strconv.Atoi(trimmed); convErr == nil && num >= 1 && num <= len(items) {
			return items[num-1].Name, nil
		}
		normalized, ok := NormalizeProviderName(trimmed)
		if ok {
			return normalized, nil
		}
		fmt.Fprintln(out, "Invalid choice. Enter a number or provider id.")
		if err == io.EOF {
			return "", fmt.Errorf("provider selection required")
		}
	}
}

func promptGeminiModel(reader *bufio.Reader, out io.Writer, existing string) (string, error) {
	defaultModel := defaultString(existing, ProviderDefaultModel(ProviderGemini))
	fmt.Fprintln(out, "Select Gemini model:")
	fmt.Fprintln(out, "  1) gemini-3.0-pro")
	fmt.Fprintln(out, "  2) gemini-3.0-flash")
	fmt.Fprintln(out, "  3) custom")
	for {
		choice, err := promptLine(reader, out, "Model choice", "1")
		if err != nil {
			return "", err
		}
		switch strings.TrimSpace(choice) {
		case "1":
			return "gemini-3.0-pro", nil
		case "2":
			return "gemini-3.0-flash", nil
		case "3":
			custom, customErr := promptLine(reader, out, "Custom Gemini model", defaultModel)
			if customErr != nil {
				return "", customErr
			}
			custom = strings.TrimSpace(custom)
			if custom == "" {
				return "", fmt.Errorf("custom Gemini model cannot be empty")
			}
			return custom, nil
		default:
			fmt.Fprintln(out, "Invalid choice. Pick 1, 2, or 3.")
		}
	}
}

func promptLine(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	if strings.TrimSpace(defaultValue) != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return defaultValue, nil
	}
	return trimmed, nil
}

func promptYesNo(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	defaultToken := "y/N"
	defaultValue := "n"
	if defaultYes {
		defaultToken = "Y/n"
		defaultValue = "y"
	}
	for {
		answer, err := promptLine(reader, out, fmt.Sprintf("%s (%s)", label, defaultToken), defaultValue)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Please answer y or n.")
		}
	}
}

func providerLabel(providerName string) string {
	if profile, ok := catalog.ProviderByID(providerName); ok {
		return profile.Label
	}
	switch providerName {
	case ProviderOpenRouter:
		return "OpenRouter"
	case ProviderAnthropic:
		return "Anthropic"
	case ProviderOpenAI:
		return "OpenAI"
	case ProviderGemini:
		return "Gemini"
	case ProviderOllama:
		return "Ollama"
	case ProviderLMStudio:
		return "LM Studio"
	default:
		return providerName
	}
}

func defaultString(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

func normalizeAllowFrom(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(strings.TrimPrefix(trimmed, "@"))
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, trimmed)
		}
	}
	return out
}

func splitKV(value string) (string, string, bool) {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	id := strings.ToLower(strings.TrimSpace(parts[0]))
	val := strings.TrimSpace(parts[1])
	if id == "" || val == "" {
		return "", "", false
	}
	return id, val, true
}

func readerOrStdin(in io.Reader) io.Reader {
	if in != nil {
		return in
	}
	return os.Stdin
}
