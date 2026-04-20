package blocks

import (
	"testing"
)

func TestParseArgs_happy(t *testing.T) {
	args, err := ParseArgs("test", "group", "module_type", "max", "10", "hide_empty", "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["group"] != "module_type" {
		t.Errorf("group = %v, want module_type", args["group"])
	}
	if args["max"] != 10 {
		t.Errorf("max = %v (%T), want int 10", args["max"], args["max"])
	}
	if args["hide_empty"] != true {
		t.Errorf("hide_empty = %v, want true", args["hide_empty"])
	}
}

func TestParseArgs_oddCount(t *testing.T) {
	_, err := ParseArgs("test", "key_without_value")
	if err == nil {
		t.Fatal("expected error for odd argument count")
	}
}

func TestParseArgs_nonStringKey(t *testing.T) {
	_, err := ParseArgs("test", 42, "value")
	if err == nil {
		t.Fatal("expected error for non-string key")
	}
}

func TestParseArgs_preservesNonStringValues(t *testing.T) {
	args, err := ParseArgs("test", "count", 5, "ratio", 3.14)
	if err != nil {
		t.Fatal(err)
	}
	if args["count"] != 5 {
		t.Errorf("count = %v, want 5", args["count"])
	}
	if args["ratio"] != 3.14 {
		t.Errorf("ratio = %v, want 3.14", args["ratio"])
	}
}

func TestArgCSV(t *testing.T) {
	args := map[string]any{"impact": "critical, high ,medium"}
	got := ArgCSV(args, "impact")
	want := []string{"critical", "high", "medium"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestArgCSV_missing(t *testing.T) {
	if got := ArgCSV(nil, "x"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestArgInt_default(t *testing.T) {
	if got := ArgInt(nil, "x", 50); got != 50 {
		t.Errorf("got %d, want 50", got)
	}
}

func TestArgBool(t *testing.T) {
	if got := ArgBool(map[string]any{"k": "true"}, "k", false); !got {
		t.Error("want true")
	}
	if got := ArgBool(map[string]any{"k": false}, "k", true); got {
		t.Error("want false")
	}
}
