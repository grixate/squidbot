package skills

import (
	"testing"

	"github.com/grixate/squidbot/internal/config"
)

func TestApplyPolicyFilterGlobalAndChannel(t *testing.T) {
	cfg := config.Default()
	cfg.Skills.Policy.Allow = []string{"aws-guard", "ui-audit"}
	cfg.Skills.Policy.Deny = []string{"ui-audit"}
	cfg.Skills.Policy.Channels["telegram"] = config.SkillsChannelPolicyConfig{Allow: []string{"aws-guard"}}

	input := []SkillDescriptor{
		{ID: "aws-guard", Name: "AWS Guard", Valid: true},
		{ID: "ui-audit", Name: "UI Audit", Valid: true},
		{ID: "docker-maint", Name: "Docker", Valid: true},
	}
	allowed, denied := applyPolicyFilter(cfg, "telegram", input)
	if len(allowed) != 1 || allowed[0].ID != "aws-guard" {
		t.Fatalf("unexpected allowed skills: %#v", allowed)
	}
	if denied["ui-audit"] == "" {
		t.Fatalf("expected deny reason for ui-audit, got %#v", denied)
	}
}
