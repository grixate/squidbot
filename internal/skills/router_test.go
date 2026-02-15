package skills

import (
	"testing"

	"github.com/grixate/squidbot/internal/config"
)

func TestRouteSkillsExplicitTakesPriorityAndCaps(t *testing.T) {
	cfg := config.Default()
	cfg.Skills.MaxActive = 2
	cfg.Skills.MatchThreshold = 20
	snapshot := IndexSnapshot{Skills: []SkillDescriptor{
		{ID: "aws-guard", Name: "AWS Guard", Description: "Secure aws changes", Tags: []string{"aws"}, Valid: true},
		{ID: "ui-audit", Name: "UI Audit", Description: "Inspect frontend UX", Tags: []string{"ui"}, Valid: true},
		{ID: "docker-maint", Name: "Docker Maint", Description: "Docker operations", Tags: []string{"docker"}, Valid: true},
	}}

	result := routeSkills(ActivationRequest{Query: "please do $ui-audit and aws infra changes"}, snapshot, cfg)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected activation errors: %v", result.Errors)
	}
	if len(result.Activated) != 2 {
		t.Fatalf("expected 2 activated skills due to cap, got %d", len(result.Activated))
	}
	if result.Activated[0].Skill.Descriptor.ID != "ui-audit" {
		t.Fatalf("expected explicit skill first, got %s", result.Activated[0].Skill.Descriptor.ID)
	}
}

func TestRouteSkillsExplicitMissingFails(t *testing.T) {
	cfg := config.Default()
	snapshot := IndexSnapshot{Skills: []SkillDescriptor{{ID: "known", Name: "Known", Valid: true}}}
	result := routeSkills(ActivationRequest{Query: "run $missing"}, snapshot, cfg)
	if len(result.Errors) == 0 {
		t.Fatal("expected explicit missing error")
	}
}

func TestRouteSkillsThresholdSkipsLowScore(t *testing.T) {
	cfg := config.Default()
	cfg.Skills.MatchThreshold = 300
	snapshot := IndexSnapshot{Skills: []SkillDescriptor{{ID: "planner", Name: "Planner", Description: "execution planning", Valid: true}}}
	result := routeSkills(ActivationRequest{Query: "plan this"}, snapshot, cfg)
	if len(result.Activated) != 0 {
		t.Fatalf("expected no activated skills due to threshold, got %d", len(result.Activated))
	}
	if len(result.Skipped) > 0 && result.Skipped[0].Score <= 0 {
		t.Fatalf("unexpected non-positive skip score diagnostics: %#v", result.Skipped)
	}
}

func TestRouteSkillsIncludesBreakdownDiagnostics(t *testing.T) {
	cfg := config.Default()
	cfg.Skills.MatchThreshold = 1
	snapshot := IndexSnapshot{Skills: []SkillDescriptor{
		{
			ID:          "aws-guard",
			Name:        "AWS Guard",
			Description: "secure aws infra changes",
			Tags:        []string{"aws", "security"},
			Examples:    []string{"rotate iam keys"},
			Valid:       true,
		},
	}}
	result := routeSkills(ActivationRequest{Query: "aws security rotate keys with $aws-guard"}, snapshot, cfg)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected activation errors: %v", result.Errors)
	}
	if len(result.Activated) != 1 {
		t.Fatalf("expected one activation, got %d", len(result.Activated))
	}
	b := result.Activated[0].Breakdown
	if b.Total <= 0 || b.ExplicitBonus == 0 || b.TagBonus == 0 {
		t.Fatalf("expected populated breakdown, got %#v", b)
	}
	if len(result.Diagnostics.Ranked) == 0 || result.Diagnostics.Ranked[0].Breakdown.Total == 0 {
		t.Fatalf("expected ranked diagnostics with breakdown, got %#v", result.Diagnostics.Ranked)
	}
}

func TestRouteSkillsStopwordOnlyQueryDoesNotActivate(t *testing.T) {
	cfg := config.Default()
	cfg.Skills.MatchThreshold = 15
	snapshot := IndexSnapshot{Skills: []SkillDescriptor{
		{ID: "planner", Name: "Planner", Description: "plan tasks and work", Examples: []string{"create a plan"}, Valid: true},
	}}
	result := routeSkills(ActivationRequest{Query: "please do this and help me"}, snapshot, cfg)
	if len(result.Activated) != 0 {
		t.Fatalf("expected no activation for stopword-only query, got %#v", result.Activated)
	}
}
