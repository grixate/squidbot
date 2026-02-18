package telemetry

import "testing"

func TestPrometheusTextIsDeterministicAndSorted(t *testing.T) {
	snapshot := map[string]uint64{
		"z_metric": 3,
		"a_metric": 1,
		"m_metric": 2,
	}
	got := PrometheusText(snapshot)
	want := "# TYPE squidbot_a_metric gauge\n" +
		"squidbot_a_metric 1\n" +
		"# TYPE squidbot_m_metric gauge\n" +
		"squidbot_m_metric 2\n" +
		"# TYPE squidbot_z_metric gauge\n" +
		"squidbot_z_metric 3\n"
	if got != want {
		t.Fatalf("unexpected prometheus output:\n%s", got)
	}
}
