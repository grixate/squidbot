package process

import "strings"

type Kind string

const (
	Channel   Kind = "channel"
	Branch    Kind = "branch"
	Worker    Kind = "worker"
	Compactor Kind = "compactor"
	Cortex    Kind = "cortex"
)

func Normalize(raw string) Kind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(Branch):
		return Branch
	case string(Worker):
		return Worker
	case string(Compactor):
		return Compactor
	case string(Cortex):
		return Cortex
	default:
		return Channel
	}
}

func (k Kind) String() string {
	normalized := Normalize(string(k))
	return string(normalized)
}
