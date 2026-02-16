package contextctrl

type Stage int

const (
	StageNone Stage = iota
	Stage1
	Stage2
	Stage3
)

type Thresholds struct {
	Stage1Pct int
	Stage2Pct int
	Stage3Pct int
}

func (t Thresholds) Normalized() Thresholds {
	out := t
	if out.Stage1Pct <= 0 {
		out.Stage1Pct = 70
	}
	if out.Stage2Pct <= 0 {
		out.Stage2Pct = 82
	}
	if out.Stage3Pct <= 0 {
		out.Stage3Pct = 90
	}
	if out.Stage1Pct > out.Stage2Pct {
		out.Stage2Pct = out.Stage1Pct
	}
	if out.Stage2Pct > out.Stage3Pct {
		out.Stage3Pct = out.Stage2Pct
	}
	if out.Stage3Pct > 100 {
		out.Stage3Pct = 100
	}
	if out.Stage2Pct > 100 {
		out.Stage2Pct = out.Stage3Pct
	}
	if out.Stage1Pct > 100 {
		out.Stage1Pct = out.Stage2Pct
	}
	return out
}

func SelectStage(utilizationPct int, thresholds Thresholds) Stage {
	normalized := thresholds.Normalized()
	switch {
	case utilizationPct >= normalized.Stage3Pct:
		return Stage3
	case utilizationPct >= normalized.Stage2Pct:
		return Stage2
	case utilizationPct >= normalized.Stage1Pct:
		return Stage1
	default:
		return StageNone
	}
}

func UtilizationPct(promptTokens, outputReserveTokens, contextWindowTokens int) int {
	window := maxInt(contextWindowTokens, 1)
	used := maxInt(promptTokens, 0) + maxInt(outputReserveTokens, 0)
	return (used * 100) / window
}

func NextStricterStage(stage Stage) Stage {
	switch stage {
	case StageNone:
		return Stage1
	case Stage1:
		return Stage2
	case Stage2:
		return Stage3
	default:
		return Stage3
	}
}
