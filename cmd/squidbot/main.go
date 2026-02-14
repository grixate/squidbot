package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/app"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/cron"
	"github.com/grixate/squidbot/internal/memory"
	"github.com/grixate/squidbot/internal/skills"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/subagent"
)

const squidbotRomanBanner = "                                 o8o        .o8   .o8                     .\n" +
	"                                 `\"'       \"888  \"888                   .o8\n" +
	" .oooo.o  .ooooo oo oooo  oooo  oooo   .oooo888   888oooo.   .ooooo.  .o888oo\n" +
	"d88(  \"8 d88' `888  `888  `888  `888  d88' `888   d88' `88b d88' `88b   888\n" +
	"`\"Y88b.  888   888   888   888   888  888   888   888   888 888   888   888\n" +
	"o.  )88b 888   888   888   888   888  888   888   888   888 888   888   888 .\n" +
	"8\"\"888P' `V8bod888   `V88V\"V8P' o888o `Y8bod88P\"  `Y8bod8P' `Y8bod8P'   \"888\"\n" +
	"               888.\n" +
	"               8P'\n" +
	"               \""

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	root := newRootCmd(logger)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd(logger *log.Logger) *cobra.Command {
	var configPath string
	root := &cobra.Command{
		Use:   "squidbot",
		Short: "squidbot - Go-native personal AI assistant",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path")

	root.AddCommand(onboardCmd(configPath))
	root.AddCommand(statusCmd(configPath))
	root.AddCommand(agentCmd(configPath, logger))
	root.AddCommand(gatewayCmd(configPath, logger))
	root.AddCommand(telegramCmd(configPath))
	root.AddCommand(cronCmd(configPath, logger))
	root.AddCommand(subagentsCmd(configPath))
	root.AddCommand(doctorCmd(configPath))
	return root
}

func printBanner(w io.Writer) {
	fmt.Fprintln(w, squidbotRomanBanner)
}

func resolvedConfigPath(path string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}
	return config.ConfigPath()
}

func loadCfg(path string) (config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return cfg, err
	}
	cfg.Agents.Defaults.Workspace = config.WorkspacePath(cfg)
	if cfg.Storage.DBPath == "" {
		cfg.Storage.DBPath = config.DataRoot() + "/squidbot.db"
	}
	if strings.TrimSpace(cfg.Memory.IndexPath) == "" {
		cfg.Memory.IndexPath = filepath.Join(config.DataRoot(), "memory_index.db")
	}
	if len(cfg.Skills.Paths) == 0 {
		cfg.Skills.Paths = []string{filepath.Join(cfg.Agents.Defaults.Workspace, "skills")}
	}
	return cfg, nil
}

func onboardCmd(configPath string) *cobra.Command {
	var providerName string
	var apiKey string
	var apiBase string
	var model string
	var nonInteractive bool
	var verifyGeminiCLI bool
	var telegramEnabled bool
	var telegramToken string
	var telegramAllowFrom []string
	var channelEnabledIDs []string
	var channelEndpoints []string
	var channelAuthTokens []string

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Initialize squidbot config and workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			printBanner(cmd.OutOrStdout())
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}

			result, err := config.RunOnboarding(cmd.Context(), cfg, config.OnboardingOptions{
				Provider:             providerName,
				APIKey:               apiKey,
				APIBase:              apiBase,
				Model:                model,
				NonInteractive:       nonInteractive,
				VerifyGeminiCLI:      verifyGeminiCLI,
				TelegramEnabledSet:   cmd.Flags().Changed("telegram-enabled"),
				TelegramEnabled:      telegramEnabled,
				TelegramTokenSet:     cmd.Flags().Changed("telegram-token"),
				TelegramToken:        telegramToken,
				TelegramAllowFromSet: cmd.Flags().Changed("telegram-allow-from"),
				TelegramAllowFrom:    telegramAllowFrom,
				ChannelEnabledIDs:    channelEnabledIDs,
				ChannelEndpoints:     channelEndpoints,
				ChannelAuthTokens:    channelAuthTokens,
				In:                   cmd.InOrStdin(),
				Out:                  cmd.OutOrStdout(),
			})
			if err != nil {
				return err
			}
			if err := config.Save(configPath, result.Config); err != nil {
				return err
			}
			if err := config.EnsureFilesystem(result.Config); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved config at %s\n", resolvedConfigPath(configPath))
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace ready at %s\n", config.WorkspacePath(result.Config))
			fmt.Fprintf(cmd.OutOrStdout(), "Active provider: %s\n", result.Provider)
			if result.GeminiCLIVerifyRan && result.GeminiCLIVerified {
				fmt.Fprintln(cmd.OutOrStdout(), "Gemini CLI verification passed")
			}
			for _, warning := range result.Warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", warning)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Next: run `squidbot agent -m \"hello\"`")
			return nil
		},
	}
	cmd.Flags().StringVar(&providerName, "provider", "", "Provider id")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Provider API key")
	cmd.Flags().StringVar(&apiBase, "api-base", "", "Provider API base URL")
	cmd.Flags().StringVar(&model, "model", "", "Provider model")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable prompts and require explicit inputs")
	cmd.Flags().BoolVar(&verifyGeminiCLI, "verify-gemini-cli", false, "Verify Gemini CLI connectivity during onboarding")
	cmd.Flags().BoolVar(&telegramEnabled, "telegram-enabled", false, "Enable Telegram channel")
	cmd.Flags().StringVar(&telegramToken, "telegram-token", "", "Telegram bot token")
	cmd.Flags().StringSliceVar(&telegramAllowFrom, "telegram-allow-from", nil, "Telegram allow list entry (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&channelEnabledIDs, "channel-enable", nil, "Enable channel id (repeatable)")
	cmd.Flags().StringSliceVar(&channelEndpoints, "channel-endpoint", nil, "Channel endpoint in id=url form (repeatable)")
	cmd.Flags().StringSliceVar(&channelAuthTokens, "channel-auth-token", nil, "Channel auth token in id=token form (repeatable)")
	return cmd
}

func statusCmd(configPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show squidbot status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			st := config.BuildStatus(cfg)
			fmt.Printf("Config: %s [%v]\n", st.ConfigPath, st.ConfigOK)
			fmt.Printf("Workspace: %s [%v]\n", st.Workspace, st.WorkspaceOK)
			fmt.Printf("Data root: %s [%v]\n", st.DataRoot, st.DataRootOK)
			fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)
			if providerName, _ := cfg.PrimaryProvider(); providerName != "" {
				fmt.Printf("Detected provider: %s\n", providerName)
			}
			fmt.Printf("Active provider: %s\n", cfg.Providers.Active)
			if err := config.ValidateActiveProvider(cfg); err != nil {
				fmt.Printf("Provider ready: false (%v)\n", err)
			} else {
				fmt.Println("Provider ready: true")
			}
			fmt.Printf("Storage backend: %s\n", cfg.Storage.Backend)
			fmt.Printf("Telegram enabled: %v\n", cfg.Channels.Telegram.Enabled)
			return nil
		},
	}
}

func agentCmd(configPath string, logger *log.Logger) *cobra.Command {
	var message string
	var sessionID string
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Chat with squidbot directly",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				return fmt.Errorf("provider setup incomplete: %w. Run `squidbot onboard`", err)
			}
			if err := config.EnsureFilesystem(cfg); err != nil {
				return err
			}
			runtime, err := app.BuildRuntime(cfg, logger)
			if err != nil {
				return err
			}
			defer runtime.Shutdown()

			if strings.TrimSpace(sessionID) == "" {
				sessionID = "cli:default"
			}

			if strings.TrimSpace(message) != "" {
				resp, err := runtime.Engine.Ask(context.Background(), agent.InboundMessage{
					SessionID: sessionID,
					RequestID: "",
					Channel:   "cli",
					ChatID:    "direct",
					SenderID:  "user",
					Content:   message,
					CreatedAt: time.Now().UTC(),
				})
				if err != nil {
					return err
				}
				fmt.Println(resp)
				return nil
			}

			fmt.Println("Interactive mode (Ctrl+C to exit)")
			reader := bufio.NewReader(os.Stdin)
			stopAsync := make(chan struct{})
			go func() {
				for {
					select {
					case <-stopAsync:
						return
					case msg := <-runtime.Engine.Outbound():
						if msg.Channel != "cli" {
							continue
						}
						source, _ := msg.Metadata["source"].(string)
						if source != "subagent" {
							continue
						}
						msgSession, _ := msg.Metadata["session_id"].(string)
						if strings.TrimSpace(msgSession) != "" && msgSession != sessionID {
							continue
						}
						fmt.Printf("\n\n[async]\n%s\n\nYou: ", msg.Content)
					}
				}
			}()
			defer close(stopAsync)
			for {
				fmt.Print("You: ")
				line, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				resp, err := runtime.Engine.Ask(context.Background(), agent.InboundMessage{
					SessionID: sessionID,
					Channel:   "cli",
					ChatID:    "direct",
					SenderID:  "user",
					Content:   line,
					CreatedAt: time.Now().UTC(),
				})
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					continue
				}
				fmt.Printf("\nsquidbot: %s\n\n", resp)
			}
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Message to send")
	cmd.Flags().StringVarP(&sessionID, "session", "s", "cli:default", "Session ID")
	return cmd
}

func gatewayCmd(configPath string, logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start squidbot gateway (telegram + cron + heartbeat)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				return fmt.Errorf("provider setup incomplete: %w. Run `squidbot onboard`", err)
			}
			if err := config.EnsureFilesystem(cfg); err != nil {
				return err
			}
			runtime, err := app.BuildRuntime(cfg, logger)
			if err != nil {
				return err
			}
			defer runtime.Shutdown()

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			fmt.Println("squidbot gateway started")
			return runtime.StartGateway(ctx)
		},
	}
	return cmd
}

func telegramCmd(configPath string) *cobra.Command {
	root := &cobra.Command{Use: "telegram", Short: "Telegram channel commands"}
	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show telegram configuration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			tokenSet := strings.TrimSpace(cfg.Channels.Telegram.Token) != ""
			fmt.Printf("Enabled: %v\n", cfg.Channels.Telegram.Enabled)
			fmt.Printf("Token set: %v\n", tokenSet)
			fmt.Printf("Allow list size: %d\n", len(cfg.Channels.Telegram.AllowFrom))
			return nil
		},
	})
	return root
}

func cronCmd(configPath string, logger *log.Logger) *cobra.Command {
	root := &cobra.Command{Use: "cron", Short: "Manage scheduled jobs"}
	var includeDisabled bool
	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			jobs, err := service.List(context.Background(), includeDisabled)
			if err != nil {
				return err
			}
			if len(jobs) == 0 {
				fmt.Println("No jobs")
				return nil
			}
			for _, job := range jobs {
				next := ""
				if job.State.NextRunAt != nil {
					next = job.State.NextRunAt.Format(time.RFC3339)
				}
				fmt.Printf("%s\t%s\t%v\t%s\n", job.ID, job.Name, job.Enabled, next)
			}
			return nil
		},
	})
	root.PersistentFlags().BoolVarP(&includeDisabled, "all", "a", false, "Include disabled jobs")

	var name, msg, cronExpr, at string
	var every int64
	var deliver bool
	var to, channel string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a scheduled job",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			job := cron.Job{
				ID:      fmt.Sprintf("job-%d", time.Now().UnixNano()),
				Name:    name,
				Enabled: true,
				Payload: cron.JobPayload{Message: msg, Deliver: deliver, Channel: channel, To: to},
			}
			switch {
			case every > 0:
				job.Schedule = cron.JobSchedule{Kind: cron.ScheduleEvery, Every: every * 1000}
			case strings.TrimSpace(cronExpr) != "":
				job.Schedule = cron.JobSchedule{Kind: cron.ScheduleCron, Expr: cronExpr}
			case strings.TrimSpace(at) != "":
				parsed, parseErr := time.Parse(time.RFC3339, at)
				if parseErr != nil {
					return parseErr
				}
				job.Schedule = cron.JobSchedule{Kind: cron.ScheduleAt, At: &parsed}
			default:
				return fmt.Errorf("provide --every, --cron, or --at")
			}
			if err := service.Put(context.Background(), job); err != nil {
				return err
			}
			fmt.Printf("Added job %s (%s)\n", job.Name, job.ID)
			return nil
		},
	}
	add.Flags().StringVarP(&name, "name", "n", "", "Job name")
	add.Flags().StringVarP(&msg, "message", "m", "", "Message payload")
	add.Flags().Int64VarP(&every, "every", "e", 0, "Run every N seconds")
	add.Flags().StringVarP(&cronExpr, "cron", "c", "", "Cron expression")
	add.Flags().StringVar(&at, "at", "", "Run once at RFC3339 time")
	add.Flags().BoolVarP(&deliver, "deliver", "d", false, "Deliver response to channel")
	add.Flags().StringVar(&channel, "channel", "telegram", "Delivery channel")
	add.Flags().StringVar(&to, "to", "", "Delivery target chat ID")
	_ = add.MarkFlagRequired("name")
	_ = add.MarkFlagRequired("message")
	root.AddCommand(add)

	root.AddCommand(&cobra.Command{
		Use:   "remove <job_id>",
		Short: "Remove a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			return service.Remove(context.Background(), args[0])
		},
	})

	var disable bool
	enable := &cobra.Command{
		Use:   "enable <job_id>",
		Short: "Enable or disable a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			service := cron.NewService(store, nil, nil)
			return service.Enable(context.Background(), args[0], !disable)
		},
	}
	enable.Flags().BoolVar(&disable, "disable", false, "Disable instead of enable")
	root.AddCommand(enable)

	var force bool
	run := &cobra.Command{
		Use:   "run <job_id>",
		Short: "Run a job now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				return fmt.Errorf("provider setup incomplete: %w. Run `squidbot onboard`", err)
			}
			runtime, err := app.BuildRuntime(cfg, logger)
			if err != nil {
				return err
			}
			defer runtime.Shutdown()
			return runtime.Cron.RunNow(context.Background(), args[0], force)
		},
	}
	run.Flags().BoolVarP(&force, "force", "f", false, "Run even if disabled")
	root.AddCommand(run)

	return root
}

func subagentsCmd(configPath string) *cobra.Command {
	root := &cobra.Command{Use: "subagents", Short: "Inspect and manage subagent runs"}
	var sessionID string
	var status string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List subagent runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			var runs []subagent.Run
			if strings.TrimSpace(status) != "" {
				runs, err = store.ListSubagentRunsByStatus(ctx, subagent.Status(strings.TrimSpace(strings.ToLower(status))), limit)
			} else {
				runs, err = store.ListSubagentRunsBySession(ctx, strings.TrimSpace(sessionID), limit)
			}
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				fmt.Println("No subagent runs")
				return nil
			}
			for _, run := range runs {
				fmt.Printf("%s\t%s\tattempt %d/%d\t%s\n", run.ID, run.Status, run.Attempt, run.MaxAttempts, run.Task)
			}
			return nil
		},
	}
	list.Flags().StringVar(&sessionID, "session", "", "Filter by session ID")
	list.Flags().StringVar(&status, "status", "", "Filter by status (queued|running|succeeded|failed|timed_out|cancelled)")
	list.Flags().IntVar(&limit, "limit", 50, "Max number of runs to return")
	root.AddCommand(list)

	show := &cobra.Command{
		Use:   "show <run_id>",
		Short: "Show one subagent run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			run, err := store.GetSubagentRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			payload, err := json.MarshalIndent(run, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(payload))
			return nil
		},
	}
	root.AddCommand(show)

	cancel := &cobra.Command{
		Use:   "cancel <run_id>",
		Short: "Cancel a queued or running subagent run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()
			ctx := context.Background()
			run, err := store.GetSubagentRun(ctx, args[0])
			if err != nil {
				return err
			}
			if run.Status.Terminal() {
				fmt.Printf("Run %s already terminal: %s\n", run.ID, run.Status)
				return nil
			}
			now := time.Now().UTC()
			if err := store.PutKV(ctx, subagent.CancelSignalNamespace, run.ID, []byte(now.Format(time.RFC3339Nano))); err != nil {
				return err
			}
			run.Status = subagent.StatusCancelled
			run.Error = "cancelled via CLI"
			run.FinishedAt = &now
			if err := store.PutSubagentRun(ctx, run); err != nil {
				return err
			}
			if err := store.AppendSubagentEvent(ctx, subagent.Event{
				RunID:     run.ID,
				Status:    subagent.StatusCancelled,
				Message:   "run cancelled via CLI",
				Attempt:   run.Attempt,
				CreatedAt: now,
			}); err != nil {
				return err
			}
			fmt.Printf("Run %s marked as cancelled\n", run.ID)
			return nil
		},
	}
	root.AddCommand(cancel)

	return root
}

func doctorCmd(configPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run configuration and dependency checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			problems := []string{}
			if err := config.ValidateActiveProvider(cfg); err != nil {
				problems = append(problems, err.Error())
			}
			if cfg.Channels.Telegram.Enabled && strings.TrimSpace(cfg.Channels.Telegram.Token) == "" {
				problems = append(problems, "Telegram enabled but token missing")
			}
			workspace := config.WorkspacePath(cfg)
			required := []string{
				filepath.Join(workspace, "AGENTS.md"),
				filepath.Join(workspace, "SOUL.md"),
				filepath.Join(workspace, "USER.md"),
				filepath.Join(workspace, "TOOLS.md"),
				filepath.Join(workspace, "HEARTBEAT.md"),
				filepath.Join(workspace, "memory", "MEMORY.md"),
			}
			for _, path := range required {
				if _, statErr := os.Stat(path); statErr != nil {
					problems = append(problems, "missing workspace file: "+path)
				}
			}
			if _, readErr := os.ReadFile(filepath.Join(workspace, "HEARTBEAT.md")); readErr != nil {
				problems = append(problems, "heartbeat file not readable: "+readErr.Error())
			}
			mem := memory.NewManager(cfg)
			if err := mem.EnsureIndex(cmd.Context()); err != nil {
				problems = append(problems, "memory index unavailable: "+err.Error())
			}
			discovery := skills.Discover(cfg)
			fmt.Printf("Skills discovered: %d\n", len(discovery.Skills))
			for _, warning := range discovery.Warnings {
				fmt.Printf("Skill warning: %s\n", warning)
			}
			if len(problems) == 0 {
				fmt.Println("Doctor checks passed")
				return nil
			}
			fmt.Println("Doctor found issues:")
			for _, p := range problems {
				fmt.Println("-", p)
			}
			return fmt.Errorf("doctor checks failed")
		},
	}
}
