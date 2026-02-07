package main

import (
	"bufio"
	"context"
	"fmt"
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
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	migratepkg "github.com/grixate/squidbot/internal/storage/migrate"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	var configPath string
	root := &cobra.Command{
		Use:   "squidbot",
		Short: "squidbot - Go-native personal AI assistant",
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path")

	root.AddCommand(onboardCmd(configPath))
	root.AddCommand(statusCmd(configPath))
	root.AddCommand(agentCmd(configPath, logger))
	root.AddCommand(gatewayCmd(configPath, logger))
	root.AddCommand(telegramCmd(configPath))
	root.AddCommand(cronCmd(configPath, logger))
	root.AddCommand(doctorCmd(configPath))
	root.AddCommand(migrateCmd(configPath))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	return cfg, nil
}

func onboardCmd(configPath string) *cobra.Command {
	return &cobra.Command{
		Use:   "onboard",
		Short: "Initialize squidbot config and workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			if err := config.Save(configPath, cfg); err != nil {
				return err
			}
			if err := config.EnsureFilesystem(cfg); err != nil {
				return err
			}
			fmt.Printf("Created config at %s\n", config.ConfigPath())
			fmt.Printf("Workspace ready at %s\n", config.WorkspacePath(cfg))
			fmt.Println("Next: set API key in ~/.squidbot/config.json and run `squidbot agent -m \"hello\"`")
			return nil
		},
	}
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
	return &cobra.Command{
		Use:   "gateway",
		Short: "Start squidbot gateway (telegram + cron + heartbeat)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
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
			providerName, _ := cfg.PrimaryProvider()
			if providerName == "" {
				problems = append(problems, "No provider API key configured (openrouter/anthropic/openai)")
			}
			if cfg.Channels.Telegram.Enabled && strings.TrimSpace(cfg.Channels.Telegram.Token) == "" {
				problems = append(problems, "Telegram enabled but token missing")
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

func migrateCmd(configPath string) *cobra.Command {
	var legacyHome string
	var mergeConfig bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Import legacy data from ~/.nanobot into squidbot",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(configPath)
			if err != nil {
				return err
			}
			if err := config.EnsureFilesystem(cfg); err != nil {
				return err
			}

			store, err := storepkg.Open(cfg.Storage.DBPath)
			if err != nil {
				return err
			}
			defer store.Close()

			report, err := migratepkg.ImportLegacy(context.Background(), legacyHome, resolvedConfigPath(configPath), cfg, store, mergeConfig)
			if err != nil {
				return err
			}

			fmt.Println("Migration completed")
			fmt.Printf("- Sessions imported: %d\n", report.SessionsImported)
			fmt.Printf("- Turns imported: %d\n", report.TurnsImported)
			fmt.Printf("- Jobs imported: %d\n", report.JobsImported)
			fmt.Printf("- Workspace files copied: %d\n", report.FilesCopied)
			fmt.Printf("- Config merged: %v\n", report.ConfigMerged)
			return nil
		},
	}

	home, _ := os.UserHomeDir()
	defaultLegacy := filepath.Join(home, ".nanobot")
	cmd.Flags().StringVar(&legacyHome, "from-legacy-home", defaultLegacy, "Legacy home directory to import from")
	cmd.Flags().BoolVar(&mergeConfig, "merge-config", true, "Merge legacy config values into squidbot config when target fields are empty")
	return cmd
}
