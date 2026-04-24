package core

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

func TestParseExpr_Success(t *testing.T) {
	e, err := ParseExpr(`1 + 2`, "test.yaml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if e.Source() != "1 + 2" {
		t.Errorf("Source() = %q", e.Source())
	}
}

func TestParseExpr_FailureCarriesPosition(t *testing.T) {
	_, err := ParseExpr(`1 +* 2`, "test.yaml:42")
	if err == nil {
		t.Fatal("expected parse error")
	}
	// hcl.Diagnostics should carry the filename we supplied.
	diags, ok := err.(hcl.Diagnostics)
	if !ok {
		t.Fatalf("err type = %T, want hcl.Diagnostics", err)
	}
	if len(diags) == 0 {
		t.Fatal("no diagnostics returned")
	}
	got := diags[0].Subject.Filename
	if got != "test.yaml:42" {
		t.Errorf("diag filename = %q, want %q", got, "test.yaml:42")
	}
}

func TestEval_ArithmeticWithoutSelf(t *testing.T) {
	v, err := Eval(MustParseExpr("10 + 20 * 2"), nil, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got, _ := v.AsBigFloat().Int64()
	if got != 50 {
		t.Errorf("value = %d, want 50", got)
	}
}

func TestEval_SelfBoundToNode(t *testing.T) {
	tree := BuildTree(synthReport("sub-a"))

	// self.name and self.kind on the report root
	v, err := Eval(MustParseExpr(`self.kind`), tree.Root, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := v.AsString(); got != string(KindReport) {
		t.Errorf("self.kind = %q, want %q", got, KindReport)
	}

	// Aggregates accessible via Node field schema
	v, err = Eval(MustParseExpr(`self.resource_count`), tree.Root, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got, _ := v.AsBigFloat().Int64()
	if got != 4 {
		t.Errorf("self.resource_count = %d, want 4", got)
	}

	// max_impact string compare
	b, err := EvalBool(MustParseExpr(`self.max_impact == "critical"`), tree.Root, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !b {
		t.Error("expected max_impact == critical for synthReport")
	}
}

func TestEval_ActionCountsMap(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	b, err := EvalBool(MustParseExpr(`self.action_counts.create > 0`), tree.Root, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !b {
		t.Error("expected self.action_counts.create > 0")
	}
}

func TestEval_CountFunction(t *testing.T) {
	tree := BuildTree(synthReport("x"))
	// count(self.changed_attrs) should equal number of unique attrs (5)
	v, err := Eval(MustParseExpr(`count(self.changed_attrs)`), tree.Root, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got, _ := v.AsBigFloat().Int64()
	if got != 5 {
		t.Errorf("count = %d, want 5", got)
	}
}

func TestEval_CountOnBadTypeErrors(t *testing.T) {
	_, err := Eval(MustParseExpr(`count(42)`), nil, nil)
	if err == nil {
		t.Fatal("expected error counting a number")
	}
}

func TestEval_Contains(t *testing.T) {
	// Terraform-stdlib-compatible: contains([list], value) -> bool.
	cases := []struct {
		expr string
		want bool
	}{
		{`contains(["critical", "high"], "critical")`, true},
		{`contains(["critical", "high"], "medium")`, false},
		{`contains([], "anything")`, false},
		{`contains(["a", "b", "c"], "b")`, true},
	}
	for _, c := range cases {
		v, err := Eval(MustParseExpr(c.expr), nil, nil)
		if err != nil {
			t.Fatalf("eval %q: %v", c.expr, err)
		}
		if v.True() != c.want {
			t.Errorf("%s = %v, want %v", c.expr, v.True(), c.want)
		}
	}
}

func TestEval_ContainsOnBadTypeErrors(t *testing.T) {
	_, err := Eval(MustParseExpr(`contains("not a list", "x")`), nil, nil)
	if err == nil {
		t.Fatal("expected error for non-collection first arg")
	}
}

func TestEval_FingerprintStable(t *testing.T) {
	e := MustParseExpr(`fingerprint("abc")`)
	a, _ := Eval(e, nil, nil)
	b, _ := Eval(e, nil, nil)
	if a.AsString() != b.AsString() {
		t.Errorf("fingerprint not stable: %q vs %q", a.AsString(), b.AsString())
	}
	if len(a.AsString()) != 64 {
		t.Errorf("fingerprint length = %d, want 64 (sha256 hex)", len(a.AsString()))
	}
}

func TestEval_IsRoot_Depth(t *testing.T) {
	tree := BuildTree(synthReport("x"))

	// Root is root
	b, err := EvalBool(MustParseExpr(`is_root(self)`), tree.Root, nil)
	if err != nil || !b {
		t.Errorf("is_root(root) = %v err=%v, want true", b, err)
	}

	// A deep node is not root
	deep := tree.Find(KindResource)
	if deep == nil {
		t.Fatal("no resource node")
	}
	b, err = EvalBool(MustParseExpr(`is_root(self)`), deep, nil)
	if err != nil || b {
		t.Errorf("is_root(resource) = %v err=%v, want false", b, err)
	}

	// depth() function form. tree.Find returns the first resource in
	// pre-order; in synthReport that's the root-module rg at depth 1.
	v, err := Eval(MustParseExpr(`depth(self)`), deep, nil)
	if err != nil {
		t.Fatalf("eval depth: %v", err)
	}
	got, _ := v.AsBigFloat().Int64()
	if got < 1 {
		t.Errorf("depth(resource) = %d, want >= 1", got)
	}

	// And a resource inside a nested module should be deeper.
	var nested *Node
	tree.Walk(func(n *Node) bool {
		if n.Kind == KindResource && strings.Contains(n.Name, "virtual_network.hub") {
			nested = n
			return false
		}
		return true
	})
	if nested == nil {
		t.Fatal("nested vnet resource not found")
	}
	v, err = Eval(MustParseExpr(`depth(self)`), nested, nil)
	if err != nil {
		t.Fatalf("eval nested depth: %v", err)
	}
	got, _ = v.AsBigFloat().Int64()
	// Report -> ModuleCall(platform) -> ModuleInstance("") -> ModuleCall(vnet) -> ModuleInstance("") -> Resource = depth 5
	if got != 5 {
		t.Errorf("depth(nested resource) = %d, want 5", got)
	}
}

func TestEval_Extras(t *testing.T) {
	extras := map[string]cty.Value{
		"threshold": cty.NumberIntVal(3),
	}
	tree := BuildTree(synthReport("x"))
	b, err := EvalBool(MustParseExpr(`self.resource_count > threshold`), tree.Root, extras)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !b {
		t.Error("expected self.resource_count > 3")
	}
}

func TestEval_ResourceKindExtraFields(t *testing.T) {
	tree := BuildTree(synthReport("x"))

	// Find the replace resource (nic)
	var nic *Node
	tree.Walk(func(n *Node) bool {
		if n.Kind == KindResource && strings.Contains(n.Name, "network_interface") {
			nic = n
			return false
		}
		return true
	})
	if nic == nil {
		t.Fatal("nic node not found")
	}

	b, err := EvalBool(MustParseExpr(`self.action == "replace" && self.impact == "critical"`), nic, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !b {
		t.Error("expected nic to be action=replace impact=critical")
	}
}

func TestEvalBool_TypeMismatch(t *testing.T) {
	_, err := EvalBool(MustParseExpr(`"not a bool"`), nil, nil)
	if err == nil {
		t.Fatal("expected error on non-bool result")
	}
}

func TestEval_PanicRecovery(t *testing.T) {
	expr, err := ParseExpr(`boom("x")`, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = safeEval(expr, makeEvalCtxWithPanicFunc())
	if err == nil {
		t.Fatal("expected error from panicking function")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("error %q does not mention panic", err)
	}
}

// makeEvalCtxWithPanicFunc registers the panicking function in an
// EvalContext for the panic-recovery test.
func makeEvalCtxWithPanicFunc() *hcl.EvalContext {
	funcs := DefaultFunctions()
	funcs["boom"] = panickingFunc()
	return &hcl.EvalContext{Functions: funcs}
}

func TestEval_NilExpr(t *testing.T) {
	_, err := Eval(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil expression")
	}
}

func TestEval_NilNode(t *testing.T) {
	// Expressions that don't reference self should still work.
	v, err := Eval(MustParseExpr(`1 + 1`), nil, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got, _ := v.AsBigFloat().Int64()
	if got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}

func TestNodeValue_NilReturnsNilVal(t *testing.T) {
	if v := NodeValue(nil); v != cty.NilVal {
		t.Errorf("NodeValue(nil) = %#v, want cty.NilVal", v)
	}
}
