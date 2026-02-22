package process

import "testing"

func TestNormalizeAndString(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{in: "channel", want: Channel},
		{in: "BRANCH", want: Branch},
		{in: " worker ", want: Worker},
		{in: "compactor", want: Compactor},
		{in: "cortex", want: Cortex},
		{in: "unknown", want: Channel},
		{in: "", want: Channel},
	}

	for _, tc := range cases {
		if got := Normalize(tc.in); got != tc.want {
			t.Fatalf("Normalize(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}

	if got := Kind("BRANCH").String(); got != string(Branch) {
		t.Fatalf("unexpected String() value: %q", got)
	}
	if got := Kind("unknown").String(); got != string(Channel) {
		t.Fatalf("unexpected fallback String() value: %q", got)
	}
}
