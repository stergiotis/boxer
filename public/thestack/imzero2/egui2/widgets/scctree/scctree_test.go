package scctree

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// findChild returns the named child of n, or nil if not present.
func findChild(n *layout.Node, name string) *layout.Node {
	for _, ch := range n.Children {
		if ch.Name == name {
			return ch
		}
	}
	return nil
}

// pathFromRoot looks up a nested child by /-separated path. Returns nil if any
// segment is missing.
func pathFromRoot(root *layout.Node, segments ...string) *layout.Node {
	cur := root
	for _, s := range segments {
		cur = findChild(cur, s)
		if cur == nil {
			return nil
		}
	}
	return cur
}

func TestWeightAccessors(t *testing.T) {
	f := &SccFile{
		Lines:      100,
		Code:       80,
		Complexity: 12,
		Bytes:      4096,
	}
	cases := []struct {
		name string
		w    Weight
		want float64
	}{
		{"WeightComplexity", WeightComplexity, 12},
		{"WeightCode", WeightCode, 80},
		{"WeightLines", WeightLines, 100},
		{"WeightBytes", WeightBytes, 4096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.w(f); got != tc.want {
				t.Errorf("%s(%+v): got %v want %v", tc.name, f, got, tc.want)
			}
		})
	}
}

func TestBuildTree_BasicNesting(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "./src/foo/a.go", Code: 10},
			{Location: "./src/foo/b.go", Code: 20},
			{Location: "./src/bar/c.go", Code: 5},
			{Location: "./root.go", Code: 1},
		}},
	}
	root := BuildTree(groups, "myrepo", WeightCode)

	if root.Name != "myrepo" {
		t.Errorf("root.Name: got %q want %q", root.Name, "myrepo")
	}
	if got := pathFromRoot(root, "root.go"); got == nil || got.Size != 1 {
		t.Errorf("expected root.go leaf with Size=1; got %+v", got)
	}
	if got := pathFromRoot(root, "src", "foo", "a.go"); got == nil || got.Size != 10 {
		t.Errorf("expected src/foo/a.go leaf with Size=10; got %+v", got)
	}
	if got := pathFromRoot(root, "src", "foo", "b.go"); got == nil || got.Size != 20 {
		t.Errorf("expected src/foo/b.go leaf with Size=20; got %+v", got)
	}
	if got := pathFromRoot(root, "src", "bar", "c.go"); got == nil || got.Size != 5 {
		t.Errorf("expected src/bar/c.go leaf with Size=5; got %+v", got)
	}
	// Directories should be reused, not duplicated.
	srcNode := findChild(root, "src")
	if srcNode == nil {
		t.Fatal("missing src directory under root")
	}
	if got := len(srcNode.Children); got != 2 {
		t.Errorf("src should have 2 children (foo, bar); got %d", got)
	}
}

func TestBuildTree_ZeroAndNegativeWeightFilesAreDropped(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "src/keep.go", Code: 5},
			{Location: "src/drop_zero.go", Code: 0},
			{Location: "src/drop_negative.go", Code: -3},
		}},
	}
	root := BuildTree(groups, "r", WeightCode)
	src := findChild(root, "src")
	if src == nil {
		t.Fatal("missing src directory")
	}
	if got := len(src.Children); got != 1 {
		t.Errorf("src should have 1 child (zero-weight + negative-weight dropped); got %d", got)
	}
	if got := src.Children[0].Name; got != "keep.go" {
		t.Errorf("remaining child should be keep.go; got %q", got)
	}
}

func TestBuildTree_DotAndEmptyPathsSkipped(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: ".", Code: 1},
			{Location: "./", Code: 1}, // filepath.Clean(".") == "."
			{Location: "real.go", Code: 1},
		}},
	}
	root := BuildTree(groups, "r", WeightCode)
	if got := len(root.Children); got != 1 {
		t.Errorf("only real.go should reach the tree; got %d children", got)
	}
}

func TestBuildTree_DotSlashPrefixStripped(t *testing.T) {
	// Both forms should produce the same nesting; not two parallel "src" dirs.
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "./src/a.go", Code: 1},
			{Location: "src/b.go", Code: 1},
		}},
	}
	root := BuildTree(groups, "r", WeightCode)
	if got := len(root.Children); got != 1 {
		t.Errorf("./-prefixed paths should share parent with unprefixed; got %d top-level children", got)
	}
	src := root.Children[0]
	if got := len(src.Children); got != 2 {
		t.Errorf("src should hold both a.go and b.go; got %d children", got)
	}
}

func TestBuildTree_EmptyInput_ReturnsBareRoot(t *testing.T) {
	root := BuildTree(nil, "empty", WeightCode)
	if root.Name != "empty" {
		t.Errorf("root.Name: got %q want %q", root.Name, "empty")
	}
	if len(root.Children) != 0 {
		t.Errorf("empty input root.Children: got %d want 0", len(root.Children))
	}
}

func TestBuildColoredTree_LeafBucketsAndStructure(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "low.go", Code: 10, Complexity: 1},
			{Location: "high.go", Code: 10, Complexity: 100},
		}},
	}
	root, colorFn := BuildColoredTree(groups, "r", WeightCode, WeightComplexity, 5)

	if len(root.Children) != 2 {
		t.Fatalf("root should have 2 children; got %d", len(root.Children))
	}

	low := findChild(root, "low.go")
	high := findChild(root, "high.go")
	if low == nil || high == nil {
		t.Fatal("expected both low.go and high.go leaves")
	}
	bLow := colorFn(low)
	bHigh := colorFn(high)
	if bLow < 0 || bLow >= 5 {
		t.Errorf("colorFn(low) out of range [0,5): %d", bLow)
	}
	if bHigh < 0 || bHigh >= 5 {
		t.Errorf("colorFn(high) out of range [0,5): %d", bHigh)
	}
	if !(bHigh > bLow) {
		t.Errorf("higher complexity should map to higher bucket; bLow=%d bHigh=%d", bLow, bHigh)
	}
}

func TestBuildColoredTree_DirectoryWeightsAggregateDescendants(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "pkg/a.go", Code: 10, Complexity: 1},
			{Location: "pkg/b.go", Code: 10, Complexity: 3},
		}},
	}
	root, colorFn := BuildColoredTree(groups, "r", WeightCode, WeightComplexity, 8)

	pkg := findChild(root, "pkg")
	if pkg == nil {
		t.Fatal("expected pkg directory")
	}
	// Directory aggregate is the sum 1+3=4. With log-normalisation against
	// the same max (4), the directory should land in the top bucket.
	// We test via colorFn rather than poking at the (now closed-over) cw map.
	bDir := colorFn(pkg)
	bA := colorFn(findChild(pkg, "a.go"))
	bB := colorFn(findChild(pkg, "b.go"))
	if !(bDir >= bB && bDir >= bA) {
		t.Errorf("directory bucket %d should be ≥ both child buckets (a=%d, b=%d)", bDir, bA, bB)
	}
}

func TestBuildColoredTree_NegativeColorWeightClampedToZero(t *testing.T) {
	// WeightComplexity can in principle be negative if scc reports weird data;
	// the implementation clamps to 0 before normalisation.
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "low.go", Code: 1, Complexity: -5},
			{Location: "high.go", Code: 1, Complexity: 10},
		}},
	}
	root, colorFn := BuildColoredTree(groups, "r", WeightCode, WeightComplexity, 4)
	low := findChild(root, "low.go")
	high := findChild(root, "high.go")
	if colorFn(low) != 0 {
		t.Errorf("clamped-to-zero leaf should map to bucket 0; got %d", colorFn(low))
	}
	if colorFn(high) <= 0 {
		t.Errorf("positive-weight leaf should map above bucket 0; got %d", colorFn(high))
	}
}

func TestBuildColoredTree_BucketsClampedToOne(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "a.go", Code: 1, Complexity: 5},
		}},
	}
	for _, n := range []int{0, -1, -100} {
		t.Run("", func(t *testing.T) {
			root, colorFn := BuildColoredTree(groups, "r", WeightCode, WeightComplexity, n)
			leaf := findChild(root, "a.go")
			if got := colorFn(leaf); got != 0 {
				t.Errorf("buckets=%d should clamp to 1 bucket → idx 0; got %d", n, got)
			}
		})
	}
}

func TestBuildColoredTree_SizeZeroFilesSkipped(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: "kept.go", Code: 1, Complexity: 1},
			{Location: "zero.go", Code: 0, Complexity: 1},
		}},
	}
	root, _ := BuildColoredTree(groups, "r", WeightCode, WeightComplexity, 4)
	if len(root.Children) != 1 {
		t.Errorf("zero-size file should be dropped; got %d children", len(root.Children))
	}
	if root.Children[0].Name != "kept.go" {
		t.Errorf("remaining child should be kept.go; got %q", root.Children[0].Name)
	}
}

func TestBuildColoredTree_DotAndEmptyPathsSkipped(t *testing.T) {
	groups := []SccGroup{
		{Name: "Go", Files: []SccFile{
			{Location: ".", Code: 1, Complexity: 1},
			{Location: "./", Code: 1, Complexity: 1},
			{Location: "a.go", Code: 1, Complexity: 1},
		}},
	}
	root, _ := BuildColoredTree(groups, "r", WeightCode, WeightComplexity, 4)
	if got := len(root.Children); got != 1 {
		t.Errorf("only a.go should reach the colored tree; got %d", got)
	}
}

func TestBuildColoredTree_BucketIndexAlwaysWithinRange(t *testing.T) {
	// Property: across a heavy-tailed input, no bucket exceeds buckets-1.
	const buckets = 7
	files := make([]SccFile, 0, 50)
	for i := 1; i <= 50; i++ {
		files = append(files, SccFile{
			Location:   "f.go", // same path → all leaves share parent
			Code:       1,
			Complexity: int64(i * i * i), // cubic tail
		})
	}
	groups := []SccGroup{{Name: "Go", Files: files}}
	root, colorFn := BuildColoredTree(groups, "r", WeightCode, WeightComplexity, buckets)

	var walk func(n *layout.Node)
	walk = func(n *layout.Node) {
		b := colorFn(n)
		if b < 0 || b >= buckets {
			t.Errorf("bucket out of range for %q: %d (buckets=%d)", n.Name, b, buckets)
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(root)
}

func TestRepoRoot_ResolvesGitToplevel(t *testing.T) {
	// Skip cleanly when this isn't a git checkout (CI sometimes runs in a
	// shallow tarball). When git IS available we expect a non-empty path.
	root, err := RepoRoot()
	if err != nil {
		t.Skipf("git not available or not in a repo: %v", err)
	}
	if root == "" {
		t.Error("RepoRoot returned empty string with no error")
	}
}

func TestIsTest(t *testing.T) {
	cases := []struct {
		name string
		f    SccFile
		want bool
	}{
		// Go.
		{"go _test.go", SccFile{Filename: "foo_test.go", Extension: "go", Location: "pkg/foo_test.go"}, true},
		{"go plain", SccFile{Filename: "foo.go", Extension: "go", Location: "pkg/foo.go"}, false},
		{"go latest.go is not a test", SccFile{Filename: "latest.go", Extension: "go", Location: "pkg/latest.go"}, false},
		{"go testing.go is not a test", SccFile{Filename: "testing.go", Extension: "go", Location: "pkg/testing.go"}, false},
		// Python.
		{"py *_test.py", SccFile{Filename: "thing_test.py", Extension: "py", Location: "a/thing_test.py"}, true},
		{"py test_*.py", SccFile{Filename: "test_thing.py", Extension: "py", Location: "a/test_thing.py"}, true},
		{"py conftest.py", SccFile{Filename: "conftest.py", Extension: "py", Location: "a/conftest.py"}, true},
		{"py greatest.py is not a test", SccFile{Filename: "greatest.py", Extension: "py", Location: "a/greatest.py"}, false},
		// Ruby.
		{"rb *_test.rb", SccFile{Filename: "foo_test.rb", Extension: "rb", Location: "a/foo_test.rb"}, true},
		{"rb *_spec.rb", SccFile{Filename: "foo_spec.rb", Extension: "rb", Location: "a/foo_spec.rb"}, true},
		// C / C++.
		{"cc _test.cc", SccFile{Filename: "foo_test.cc", Extension: "cc", Location: "a/foo_test.cc"}, true},
		{"cpp _test.cpp", SccFile{Filename: "foo_test.cpp", Extension: "cpp", Location: "a/foo_test.cpp"}, true},
		// JavaScript / TypeScript.
		{"js .test.js", SccFile{Filename: "foo.test.js", Extension: "js", Location: "a/foo.test.js"}, true},
		{"ts .spec.ts", SccFile{Filename: "foo.spec.ts", Extension: "ts", Location: "a/foo.spec.ts"}, true},
		{"tsx .test.tsx", SccFile{Filename: "Comp.test.tsx", Extension: "tsx", Location: "a/Comp.test.tsx"}, true},
		{"ts plain", SccFile{Filename: "foo.ts", Extension: "ts", Location: "a/foo.ts"}, false},
		// Directory conventions.
		{"java under src/test", SccFile{Filename: "FooTest.java", Extension: "java", Location: "src/test/java/FooTest.java"}, true},
		{"rust tests/ dir", SccFile{Filename: "integration.rs", Extension: "rs", Location: "tests/integration.rs"}, true},
		{"go testdata/ dir", SccFile{Filename: "golden.go", Extension: "go", Location: "pkg/testdata/golden.go"}, true},
		{"js __tests__/ dir", SccFile{Filename: "comp.js", Extension: "js", Location: "src/__tests__/comp.js"}, true},
		{"spec/ dir", SccFile{Filename: "thing.rb", Extension: "rb", Location: "spec/thing.rb"}, true},
		// False-positive guards.
		{"java FooTest outside a test dir (filename pattern skipped)", SccFile{Filename: "FooTest.java", Extension: "java", Location: "src/main/java/FooTest.java"}, false},
		{"contest/ is not a test dir", SccFile{Filename: "a.go", Extension: "go", Location: "contest/a.go"}, false},
		{"root-level plain file", SccFile{Filename: "x.go", Extension: "go", Location: "x.go"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTest(&tc.f); got != tc.want {
				t.Errorf("IsTest(%q @ %q): got %v want %v", tc.f.Filename, tc.f.Location, got, tc.want)
			}
		})
	}
	if IsTest(nil) {
		t.Error("IsTest(nil): got true want false")
	}
}

func TestComplexityPalette_NineStops(t *testing.T) {
	// Sanity: the palette length is the canonical 9-stop ColorBrewer RdYlGn.
	if got := len(ComplexityPalette); got != 9 {
		t.Errorf("ComplexityPalette length: got %d want 9", got)
	}
	seen := make(map[uint32]bool, len(ComplexityPalette))
	for _, c := range ComplexityPalette {
		if seen[c] {
			t.Errorf("duplicate palette colour 0x%08x", c)
		}
		seen[c] = true
		if c&0xff == 0 {
			t.Errorf("palette entry has zero alpha: 0x%08x", c)
		}
	}
}
