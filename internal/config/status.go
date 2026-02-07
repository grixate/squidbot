package config

import "os"

type Status struct {
	ConfigPath  string
	ConfigOK    bool
	Workspace   string
	WorkspaceOK bool
	DataRoot    string
	DataRootOK  bool
}

func BuildStatus(cfg Config) Status {
	status := Status{
		ConfigPath: ConfigPath(),
		Workspace:  WorkspacePath(cfg),
		DataRoot:   DataRoot(),
	}
	if _, err := os.Stat(status.ConfigPath); err == nil {
		status.ConfigOK = true
	}
	if _, err := os.Stat(status.Workspace); err == nil {
		status.WorkspaceOK = true
	}
	if _, err := os.Stat(status.DataRoot); err == nil {
		status.DataRootOK = true
	}
	return status
}
