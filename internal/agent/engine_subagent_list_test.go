package agent

import (
	"testing"

	"github.com/grixate/squidbot/internal/subagent"
)

func TestNormalizeSubagentListFilter(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantStatus subagent.Status
		wantActive bool
		wantErr    bool
	}{
		{name: "empty", in: "", wantStatus: "", wantActive: false},
		{name: "all", in: "all", wantStatus: "", wantActive: false},
		{name: "active", in: "active", wantStatus: "", wantActive: true},
		{name: "running", in: "running", wantStatus: subagent.StatusRunning},
		{name: "failed", in: "failed", wantStatus: subagent.StatusFailed},
		{name: "invalid", in: "banana", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotActive, err := normalizeSubagentListFilter(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotStatus != tc.wantStatus || gotActive != tc.wantActive {
				t.Fatalf("unexpected output: status=%q active=%v", gotStatus, gotActive)
			}
		})
	}
}
