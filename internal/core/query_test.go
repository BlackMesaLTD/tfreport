package core

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

// --- Path parser tests ---

func TestParsePath_Empty(t *testing.T) {
	p, err := ParsePath("")
	if err != nil {
		t.Fatalf("empty path: %v", err)
	}
	if p != nil {
		t.Errorf("empty path = %v, want nil", p)
	}
}

func TestParsePath_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want Path
	}{
		{"resource", Path{KindResource}},
		{"report > resource", Path{KindReport, KindResource}},
		{"module_call>module_instance>resource", Path{KindModuleCall, KindModuleInstance, KindResource}},
		{"  report   >   module_call  ", Path{KindReport, KindModuleCall}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParsePath(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestParsePath_Errors(t *testing.T) {
	cases := []struct {
		in      string
		wantSub string
	}{
		{"bogus", "unknown kind"},
		{"report > > resource", "empty step"},
		{">resource", "empty step"},
		{"resource >", "empty step"},
		{"module_call > module_garbage", "unknown kind"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			_, err := ParsePath(c.in)
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err %q missing %q", err, c.wantSub)
			}
		})
	}
}

func TestPath_String(t *testing.T) {
	p := Path{KindModuleCall, KindResource, KindAttribute}
	if got := p.String(); got != "module_call > resource > attribute" {
		t.Errorf("String() = %q", got)
	}
}

// --- Query engine tests ---

func TestQuery_NilScope_ReturnsNil(t *testing.T) {
	if got := Query(nil, Path{KindResource}); got != nil {
		t.Errorf("nil scope = %v, want nil", got)
	}
}

func TestQuery_EmptyPath_ReturnsNil(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	if got := Query(tree.Root, nil); got != nil {
		t.Errorf("empty path = %v, want nil", got)
	}
}

func TestQuery_FindAllResources(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	got := Query(tree.Root, MustParsePath("resource"))
	if len(got) != 4 {
		t.Errorf("resource query found %d, want 4", len(got))
	}
	for _, n := range got {
		if n.Kind != KindResource {
			t.Errorf("non-resource in result: %v", n.Kind)
		}
	}
}

func TestQuery_FindAllAttributes(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	got := Query(tree.Root, MustParsePath("attribute"))
	// synthReport has 2+2+1+1 = 6 changed attributes
	if len(got) != 6 {
		t.Errorf("attribute query found %d, want 6", len(got))
	}
}

func TestQuery_DescendantChain(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	// `resource > attribute` — every attribute lives under a resource,
	// so the count should still equal the attribute total.
	got := Query(tree.Root, MustParsePath("resource > attribute"))
	if len(got) != 6 {
		t.Errorf("resource > attribute found %d, want 6", len(got))
	}

	// `module_instance > resource` — resources under module instances,
	// excluding root-module resources (the rg). synthReport has 3 such.
	got = Query(tree.Root, MustParsePath("module_instance > resource"))
	if len(got) != 3 {
		t.Errorf("module_instance > resource found %d, want 3", len(got))
	}
}

func TestQuery_PathOrderMatters(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	// Reverse-order path has no descendant chain match.
	if got := Query(tree.Root, MustParsePath("attribute > resource")); len(got) != 0 {
		t.Errorf("attribute > resource found %d, want 0", len(got))
	}
}

func TestQuery_KeyChange(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	got := Query(tree.Root, MustParsePath("key_change"))
	if len(got) != 1 {
		t.Errorf("key_change found %d, want 1", len(got))
	}
}

// --- Filter tests ---

func TestFilter_NilExprPassesThrough(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	got, err := Filter(resources, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(resources) {
		t.Errorf("len(got) = %d, want %d", len(got), len(resources))
	}
}

func TestFilter_WhereExpression(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))

	// Only replace/delete — impact critical or high
	got, err := Filter(resources, MustParseExpr(`self.impact == "critical" || self.impact == "high"`), nil)
	if err != nil {
		t.Fatal(err)
	}
	// synthReport has one replace (critical) and zero high → 1
	if len(got) != 1 {
		t.Errorf("filter critical/high got %d, want 1", len(got))
	}
}

func TestFilter_NonBoolExprErrors(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	_, err := Filter(resources, MustParseExpr(`"not a bool"`), nil)
	if err == nil {
		t.Fatal("want error on non-bool filter expr")
	}
}

// --- GroupBy tests ---

func TestGroupBy_Nil_SingleBucket(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	got, err := GroupBy(resources, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "" || len(got[0].Nodes) != len(resources) {
		t.Errorf("nil group = %#v, want single key=\"\" with all nodes", got)
	}
}

func TestGroupBy_ByAction(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	got, err := GroupBy(resources, MustParseExpr(`self.action`), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Expected: create (2 — rg, dns), update (1 — vnet), replace (1 — nic)
	keyCounts := map[string]int{}
	for _, g := range got {
		keyCounts[g.Key] = len(g.Nodes)
	}
	want := map[string]int{"create": 2, "update": 1, "replace": 1}
	if !reflect.DeepEqual(keyCounts, want) {
		t.Errorf("groups = %v, want %v", keyCounts, want)
	}
}

func TestGroupBy_InsertionOrderPreserved(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	got, err := GroupBy(resources, MustParseExpr(`self.action`), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The first resource is the rg (create). So the first group should be "create".
	if len(got) == 0 || got[0].Key != "create" {
		t.Errorf("first group = %v, want create first (rg is visited first)", got)
	}
}

// --- SortBy tests ---

func TestSortBy_ByString_Ascending(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	got, err := SortBy(resources, MustParseExpr(`self.name`), false)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(got))
	for i, n := range got {
		names[i] = n.Name
	}
	want := make([]string, len(names))
	copy(want, names)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Errorf("ascending = %v, want %v", names, want)
	}
}

func TestSortBy_Descending(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	got, err := SortBy(resources, MustParseExpr(`self.name`), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatal("not enough data")
	}
	if got[0].Name < got[1].Name {
		t.Errorf("descending sort put %q before %q", got[0].Name, got[1].Name)
	}
}

func TestSortBy_MixedTypesErrors(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	// Heterogeneous result: name is string for some, but tack on number
	// by forcing the kind field into a comparison that won't type-check.
	resources := Query(tree.Root, MustParsePath("resource"))
	// Not truly mixed in our fixture — but an unsupported type still errors.
	_, err := SortBy(resources, MustParseExpr(`[self.name]`), false)
	if err == nil {
		t.Fatal("want error for list type")
	}
}

// --- Limit tests ---

func TestLimit(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	if got := Limit(resources, 0); len(got) != len(resources) {
		t.Errorf("limit 0 dropped nodes")
	}
	if got := Limit(resources, 2); len(got) != 2 {
		t.Errorf("limit 2 = %d, want 2", len(got))
	}
	if got := Limit(resources, 9999); len(got) != len(resources) {
		t.Errorf("limit 9999 changed length")
	}
}

// --- Composition smoke test ---

func TestQueryCompose_FilterSortLimit(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	nodes := Query(tree.Root, MustParsePath("resource"))

	filtered, err := Filter(nodes, MustParseExpr(`self.impact != "none"`), nil)
	if err != nil {
		t.Fatal(err)
	}
	sorted, err := SortBy(filtered, MustParseExpr(`self.impact`), true) // descending so critical comes first... alphabetically
	if err != nil {
		t.Fatal(err)
	}
	limited := Limit(sorted, 2)
	if len(limited) != 2 {
		t.Errorf("final len = %d, want 2", len(limited))
	}
}

func TestGroupBy_NumberKey(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))

	// Group by number of changed attrs.
	got, err := GroupBy(resources, MustParseExpr(`count(self.changed_attrs)`), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Expected groups: "2" (rg, vnet — 2 attrs each), "1" (dns, nic — 1 attr each)
	keyCounts := map[string]int{}
	for _, g := range got {
		keyCounts[g.Key] = len(g.Nodes)
	}
	want := map[string]int{"2": 2, "1": 2}
	if !reflect.DeepEqual(keyCounts, want) {
		t.Errorf("group counts = %v, want %v", keyCounts, want)
	}
}

func TestFilter_WithExtras(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	resources := Query(tree.Root, MustParsePath("resource"))
	extras := map[string]cty.Value{
		"wanted_action": cty.StringVal("replace"),
	}
	got, err := Filter(resources, MustParseExpr(`self.action == wanted_action`), extras)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d, want 1 (only the nic replaces)", len(got))
	}
}
