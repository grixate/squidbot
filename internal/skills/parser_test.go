package skills

import (
	"strings"
	"testing"
)

func TestParseSkillDocumentFrontMatter(t *testing.T) {
	content := `---
name: AWS Guard

description: Secure AWS changes
tags:
  - aws
  - security
aliases: [infra-aws, aws-safe]
examples:
  - rotate iam keys
references:
  - references/aws.md
tools:
  - exec
version: "1.0.0"
owner: platform
---
# AWS Guard
Follow strict controls.`
	desc := parseSkillDocument("/tmp/SKILL.md", "/tmp", "dir", []byte(content))
	if !desc.Valid {
		t.Fatalf("expected valid descriptor, got errors: %v", desc.Errors)
	}
	if desc.ID != "aws-guard" {
		t.Fatalf("unexpected id: %s", desc.ID)
	}
	if desc.Name != "AWS Guard" {
		t.Fatalf("unexpected name: %s", desc.Name)
	}
	if desc.Description != "Secure AWS changes" {
		t.Fatalf("unexpected description: %s", desc.Description)
	}
	if len(desc.Tags) != 2 || desc.Tags[0] != "aws" {
		t.Fatalf("unexpected tags: %#v", desc.Tags)
	}
	if _, ok := desc.Extra["owner"]; !ok {
		t.Fatalf("expected unknown key in extra map, got %#v", desc.Extra)
	}
}

func TestParseSkillDocumentMalformedFrontMatterMarksInvalid(t *testing.T) {
	content := "---\nname bad\n---\n# Skill\nhello"
	desc := parseSkillDocument("/tmp/SKILL.md", "/tmp", "dir", []byte(content))
	if desc.Valid {
		t.Fatal("expected descriptor to be invalid")
	}
	if len(desc.Errors) == 0 {
		t.Fatal("expected parser errors")
	}
}

func TestParseSkillDocumentFallbacks(t *testing.T) {
	content := "# Planner\n\nDesign execution plans."
	desc := parseSkillDocument("/tmp/planner/SKILL.md", "/tmp/planner", "dir", []byte(content))
	if !desc.Valid {
		t.Fatalf("expected valid descriptor, got errors: %v", desc.Errors)
	}
	if !strings.Contains(strings.ToLower(desc.Description), "design") {
		t.Fatalf("expected description fallback from body, got: %s", desc.Description)
	}
	if desc.ID != "planner" {
		t.Fatalf("unexpected fallback id: %s", desc.ID)
	}
}
