package telemetry

import (
	"fmt"
	"sort"
	"strings"
)

func PrometheusText(snapshot map[string]uint64) string {
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		metric := "squidbot_" + key
		b.WriteString(fmt.Sprintf("# TYPE %s gauge\n", metric))
		b.WriteString(fmt.Sprintf("%s %d\n", metric, snapshot[key]))
	}
	return b.String()
}
