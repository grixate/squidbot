package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/management"
	"github.com/grixate/squidbot/internal/setupauth"
)

const (
	onboardingModeCLI          = "cli"
	onboardingModeWeb          = "web"
	webOnboardingShutdownGrace = 2 * time.Second
)

type managementRunConfig struct {
	requireSetupToken bool
	remote            bool
	host              string
	port              int
	publicBaseURL     string
	autoExitOnSetup   bool
}

func resolveOnboardingMode(mode string, nonInteractive bool, in io.Reader, out io.Writer) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		if nonInteractive {
			return onboardingModeCLI, nil
		}
		reader := bufio.NewReader(readerOrStdin(in))
		return promptOnboardingMode(reader, out)
	}
	switch mode {
	case onboardingModeCLI, onboardingModeWeb:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported onboarding mode %q (expected %q or %q)", mode, onboardingModeCLI, onboardingModeWeb)
	}
}

func promptOnboardingMode(reader *bufio.Reader, out io.Writer) (string, error) {
	fmt.Fprintln(out, "Choose onboarding mode:")
	fmt.Fprintln(out, "  1) CLI")
	fmt.Fprintln(out, "  2) Web UI")
	for {
		fmt.Fprint(out, "Mode [1-2]: ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		switch strings.TrimSpace(strings.ToLower(line)) {
		case "1", "cli":
			return onboardingModeCLI, nil
		case "2", "web", "web ui", "ui":
			return onboardingModeWeb, nil
		default:
			fmt.Fprintln(out, "Invalid choice. Enter 1 or 2.")
			if err == io.EOF {
				return "", fmt.Errorf("onboarding mode is required")
			}
		}
	}
}

func collectManagementPassword(in io.Reader, out io.Writer, minLength int, passwordFlag, passwordConfirmFlag string, nonInteractive bool) (string, error) {
	passwordFlag = strings.TrimSpace(passwordFlag)
	passwordConfirmFlag = strings.TrimSpace(passwordConfirmFlag)
	if nonInteractive {
		if passwordFlag == "" || passwordConfirmFlag == "" {
			return "", fmt.Errorf("non-interactive CLI onboarding requires --password and --password-confirm")
		}
		if err := validateManagementPassword(passwordFlag, passwordConfirmFlag, minLength); err != nil {
			return "", err
		}
		return passwordFlag, nil
	}

	reader := bufio.NewReader(readerOrStdin(in))
	useSuggested, err := promptYesNoLocal(reader, out, "Generate a strong password suggestion?", true)
	if err != nil {
		return "", err
	}
	if useSuggested {
		for {
			suggested, genErr := setupauth.GeneratePassword()
			if genErr != nil {
				fmt.Fprintf(out, "Warning: failed to generate password suggestion: %v\n", genErr)
				break
			}
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Suggested management password:")
			fmt.Fprintf(out, "  %s\n", formatSuggestedPassword(suggested))
			fmt.Fprintln(out, "Save this now. It will not be shown again.")
			if err := waitForEnter(reader, out, "Press enter after you have saved this password"); err != nil {
				return "", err
			}
			useIt, useErr := promptYesNoLocal(reader, out, "Use this password?", true)
			if useErr != nil {
				return "", useErr
			}
			if useIt {
				return suggested, nil
			}
			regenerate, regenerateErr := promptYesNoLocal(reader, out, "Generate another password?", true)
			if regenerateErr != nil {
				return "", regenerateErr
			}
			if regenerate {
				continue
			}
			break
		}
	}

	for {
		password, err := promptLineLocal(reader, out, "Management password", "")
		if err != nil {
			return "", err
		}
		confirm, err := promptLineLocal(reader, out, "Confirm management password", "")
		if err != nil {
			return "", err
		}
		if err := validateManagementPassword(password, confirm, minLength); err != nil {
			fmt.Fprintf(out, "Invalid password: %v\n", err)
			continue
		}
		return password, nil
	}
}

func validateManagementPassword(password, confirm string, minLength int) error {
	password = strings.TrimSpace(password)
	confirm = strings.TrimSpace(confirm)
	if len(password) < minLength {
		return fmt.Errorf("password must be at least %d characters", minLength)
	}
	if password != confirm {
		return fmt.Errorf("password confirmation does not match")
	}
	return nil
}

func promptLineLocal(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
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

func promptYesNoLocal(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	defaultToken := "y/N"
	defaultValue := "n"
	if defaultYes {
		defaultToken = "Y/n"
		defaultValue = "y"
	}
	for {
		answer, err := promptLineLocal(reader, out, fmt.Sprintf("%s (%s)", label, defaultToken), defaultValue)
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

func waitForEnter(reader *bufio.Reader, out io.Writer, label string) error {
	fmt.Fprintf(out, "%s: ", label)
	_, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

func formatSuggestedPassword(password string) string {
	if password == "" {
		return ""
	}
	parts := make([]string, 0, (len(password)+3)/4)
	for start := 0; start < len(password); start += 4 {
		end := start + 4
		if end > len(password) {
			end = len(password)
		}
		parts = append(parts, password[start:end])
	}
	return strings.Join(parts, "-")
}

func validateOnboardModeFlags(mode string, remote bool, manageHost string, managePort int, managePublicURL string, password, passwordConfirm string, nonInteractive bool) error {
	if mode == onboardingModeCLI {
		if remote || strings.TrimSpace(manageHost) != "" || managePort > 0 || strings.TrimSpace(managePublicURL) != "" {
			return fmt.Errorf("web management flags require --mode web")
		}
		if !nonInteractive && (strings.TrimSpace(password) != "" || strings.TrimSpace(passwordConfirm) != "") {
			return fmt.Errorf("--password and --password-confirm are only supported with --non-interactive --mode cli")
		}
		return nil
	}
	if strings.TrimSpace(password) != "" || strings.TrimSpace(passwordConfirm) != "" {
		return fmt.Errorf("--password and --password-confirm are only supported with --mode cli")
	}
	return nil
}

func startManagementServer(cmd *cobra.Command, logger *log.Logger, configPath string, runCfg managementRunConfig) error {
	configPath = currentConfigPath(configPath)
	cfg, err := loadCfg(configPath)
	if err != nil {
		return err
	}

	publicURL := strings.TrimSpace(runCfg.publicBaseURL)
	if publicURL != "" {
		if err := validatePublicURL(publicURL); err != nil {
			return err
		}
	}

	host := strings.TrimSpace(runCfg.host)
	if host == "" {
		if runCfg.remote {
			host = "0.0.0.0"
		} else {
			host = strings.TrimSpace(cfg.Management.Host)
		}
	}
	if runCfg.remote && !managementHostIsRemote(host) {
		return fmt.Errorf("--remote requires a non-loopback management host")
	}

	effectiveRemote := runCfg.remote || managementHostIsRemote(host)
	requireToken := runCfg.requireSetupToken || effectiveRemote
	setupWasIncomplete := !config.IsSetupComplete(cfg)

	server, err := management.NewServer(cfg, management.Options{
		ConfigPath:        configPath,
		RequireSetupToken: requireToken,
		Host:              host,
		Port:              runCfg.port,
		PublicBaseURL:     publicURL,
		Logger:            logger,
	})
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if runCfg.autoExitOnSetup && setupWasIncomplete {
		go func() {
			<-server.SetupCompleted()
			time.Sleep(webOnboardingShutdownGrace)
			cancel()
		}()
	}

	printManagementAccessInfo(cmd.OutOrStdout(), server.DisplayURLs(), server.SetupToken(), requireToken, effectiveRemote)
	fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl+C to stop.")

	if err := server.Start(ctx); err != nil {
		return err
	}

	if runCfg.autoExitOnSetup && setupWasIncomplete {
		select {
		case <-server.SetupCompleted():
			fmt.Fprintln(cmd.OutOrStdout(), "Browser onboarding completed.")
			fmt.Fprintf(cmd.OutOrStdout(), "Saved config at %s\n", resolvedConfigPath(configPath))
			fmt.Fprintln(cmd.OutOrStdout(), "Next: run `squidbot agent -m \"hello\"`")
		default:
		}
	}

	return nil
}

func printManagementAccessInfo(out io.Writer, urls management.ManagementURLs, setupToken string, tokenEnabled bool, remote bool) {
	fmt.Fprintf(out, "Local URL: %s\n", urls.LocalURL)
	fmt.Fprintf(out, "Bind URL: %s\n", urls.BindURL)
	if urls.RemoteURL != "" {
		fmt.Fprintf(out, "Remote URL: %s\n", urls.RemoteURL)
	} else if urls.RemoteURLHint != "" {
		fmt.Fprintf(out, "Remote URL: %s\n", urls.RemoteURLHint)
	}
	if tokenEnabled && setupToken != "" {
		fmt.Fprintf(out, "Setup URL (local): %s?setup_token=%s\n", urls.LocalURL, setupToken)
		if urls.RemoteURL != "" {
			fmt.Fprintf(out, "Setup URL (remote): %s?setup_token=%s\n", urls.RemoteURL, setupToken)
		}
		fmt.Fprintf(out, "Setup token: %s\n", setupToken)
	}
	if remote && (urls.RemoteURL == "" || strings.HasPrefix(urls.RemoteURL, "http://")) {
		fmt.Fprintln(out, "Warning: remote onboarding over plain HTTP exposes credentials and session cookies in transit.")
		fmt.Fprintln(out, "Use this only for temporary bootstrap on a trusted path.")
	}
	if remote && urls.RemoteURL == "" {
		fmt.Fprintln(out, "Remote access note: substitute the machine's reachable host or IP when opening the bind URL remotely.")
	}
}

func validatePublicURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid --manage-public-url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("--manage-public-url must use http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("--manage-public-url must include a host")
	}
	return nil
}

func managementHostIsRemote(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	switch strings.ToLower(host) {
	case "", "127.0.0.1", "localhost", "::1":
		return false
	default:
		return true
	}
}

func applyManagementPassword(cfg *config.Config, password string) error {
	hash, err := setupauth.HashPassword(password)
	if err != nil {
		return err
	}
	cfg.Auth.PasswordHash = hash
	cfg.Auth.PasswordUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}

func readerOrStdin(in io.Reader) io.Reader {
	if in != nil {
		return in
	}
	return os.Stdin
}
