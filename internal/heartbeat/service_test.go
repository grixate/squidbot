package heartbeat

import "testing"

func TestIsEmpty(t *testing.T) {
	if !isEmpty("# Header\n\n<!-- comment -->\n") {
		t.Fatal("expected empty content to be treated as empty")
	}
	if isEmpty("# Header\n- [ ]") == false {
		t.Fatal("checkbox-only content should be treated as empty")
	}
	if isEmpty("# Header\n- check inbox") {
		t.Fatal("actionable line should not be empty")
	}
}
