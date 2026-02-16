package contextctrl

import (
	"testing"

	"github.com/grixate/squidbot/internal/provider"
)

func TestEstimateMessagesTokensMonotonic(t *testing.T) {
	base := []provider.Message{
		{Role: "system", Content: "You are squidbot."},
		{Role: "user", Content: "hello"},
	}
	expanded := append([]provider.Message{}, base...)
	expanded = append(expanded, provider.Message{Role: "assistant", Content: "longer answer with more detail"})

	baseTokens := EstimateMessagesTokens(base, 4.0)
	expandedTokens := EstimateMessagesTokens(expanded, 4.0)
	if expandedTokens <= baseTokens {
		t.Fatalf("expected expanded estimate > base estimate, base=%d expanded=%d", baseTokens, expandedTokens)
	}
}

func TestUtilizationAndStageSelection(t *testing.T) {
	thresholds := Thresholds{Stage1Pct: 70, Stage2Pct: 82, Stage3Pct: 90}
	stage := SelectStage(UtilizationPct(5000, 1000, 10000), thresholds)
	if stage != StageNone {
		t.Fatalf("expected stage none, got %v", stage)
	}
	stage = SelectStage(UtilizationPct(6200, 1000, 10000), thresholds)
	if stage != Stage1 {
		t.Fatalf("expected stage1, got %v", stage)
	}
	stage = SelectStage(UtilizationPct(7300, 1000, 10000), thresholds)
	if stage != Stage2 {
		t.Fatalf("expected stage2, got %v", stage)
	}
	stage = SelectStage(UtilizationPct(8100, 1000, 10000), thresholds)
	if stage != Stage3 {
		t.Fatalf("expected stage3, got %v", stage)
	}
}

func TestNextStricterStage(t *testing.T) {
	if NextStricterStage(StageNone) != Stage1 {
		t.Fatal("expected none->stage1")
	}
	if NextStricterStage(Stage1) != Stage2 {
		t.Fatal("expected stage1->stage2")
	}
	if NextStricterStage(Stage2) != Stage3 {
		t.Fatal("expected stage2->stage3")
	}
	if NextStricterStage(Stage3) != Stage3 {
		t.Fatal("expected stage3->stage3")
	}
}
