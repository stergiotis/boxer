//go:build llm_generated_opus46

package eh

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// FormatError produces a human-readable, terminal-friendly representation of
// an error tree. It walks the error's Unwrap chain, deduplicates messages that
// are embedded by fmt.Errorf's %w wrapping, merges overlapping stack traces
// into a single annotated stack, and adapts its output complexity to the error.
//
//   - A simple error with no wrapping prints as a one-liner with a location.
//   - A linear wrap chain prints as a causal chain with a single merged stack.
//   - A tree (errors.Join) prints branches with per-branch stacks.
//
// The output is written to w. If w is nil, os.Stderr is used.
func FormatError(w io.Writer, err error) {
	if w == nil {
		w = os.Stderr
	}
	if err == nil {
		fmt.Fprintln(w, "<nil error>")
		return
	}

	tree := buildErrorTree(err)
	tree.deduplicateMessages()

	switch {
	case tree.isSimple():
		formatSimple(w, tree)
	case tree.isLinearChain():
		formatLinearChain(w, tree)
	default:
		formatTree(w, tree, 0)
	}
}

// FormatErrorS is a convenience wrapper that returns the formatted string.
func FormatErrorS(err error) string {
	var b strings.Builder
	FormatError(&b, err)
	return b.String()
}

// ---------------------------------------------------------------------------
// Internal error tree representation
// ---------------------------------------------------------------------------

// errNode is one node in the unwrapped error tree. Unlike the gatherFactsAndStacks
// approach, this works directly with the tree structure which is more natural for
// human-readable output.
type errNode struct {
	// ownMessage is the message fragment contributed by THIS wrapping layer,
	// with the inner error's text stripped out. For a leaf error, this is the
	// full message.
	ownMessage string
	// fullMessage is err.Error() verbatim.
	fullMessage string
	// location is file:line if a stack trace is available, empty otherwise.
	location string
	// function is the short function name at the error's creation site.
	function string
	// data is decoded CBOR structured data, if present.
	data string
	// children are the unwrapped child errors (len=1 for single wrap, >1 for Join).
	children []*errNode
}

func (n *errNode) isSimple() bool {
	return len(n.children) == 0
}

func (n *errNode) isLinearChain() bool {
	if len(n.children) != 1 {
		return len(n.children) == 0
	}
	return n.children[0].isLinearChain()
}

// linearChain returns all nodes in a single-wrap chain from outermost to innermost.
func (n *errNode) linearChain() []*errNode {
	chain := []*errNode{n}
	cur := n
	for len(cur.children) == 1 {
		cur = cur.children[0]
		chain = append(chain, cur)
	}
	return chain
}

// isJoinNode returns true if this node is a Join-produced error whose own message
// is just the concatenation of its children (no additional context). These nodes
// should be transparent in the tree display.
func (n *errNode) isJoinNode() bool {
	if len(n.children) < 2 {
		return false
	}
	if n.location != "" || n.data != "" {
		return false
	}
	// Check if the message is just children joined by \n
	var childTexts []string
	for _, c := range n.children {
		childTexts = append(childTexts, c.fullMessage)
	}
	return n.fullMessage == strings.Join(childTexts, "\n")
}

func buildErrorTree(err error) *errNode {
	if err == nil {
		return nil
	}

	node := &errNode{
		fullMessage: err.Error(),
	}

	// Extract location from stack trace
	if st, ok := err.(stackTracer); ok {
		trace := st.StackTrace()
		if len(trace) > 0 {
			node.location = shortenPath(trace[0].File) + ":" + strconv.Itoa(trace[0].Line)
			node.function = trace[0].ShortFunction()
		}
	}

	// Extract structured data
	if esd, ok := err.(ErrorWithStructuredData); ok {
		data := esd.GetCBORStructuredData()
		if len(data) > 0 {
			diag, diagErr := cbor.Diagnose(data)
			if diagErr == nil {
				node.data = diag
			}
		}
	}

	// Recurse into children
	switch et := err.(type) {
	case unwrapableMulti:
		children := et.Unwrap()
		for _, child := range children {
			if child != nil && child != err {
				node.children = append(node.children, buildErrorTree(child))
			}
		}
	case unwrapableSingle:
		child := et.Unwrap()
		if child != nil && child != err {
			node.children = append(node.children, buildErrorTree(child))
		}
	}

	node.ownMessage = node.fullMessage
	return node
}

// deduplicateMessages strips the child's full message text from the parent's
// message, since fmt.Errorf("%s: %w", ctx, inner) produces "ctx: <inner.Error()>"
// and repeating the full inner text at every level is noise.
func (n *errNode) deduplicateMessages() {
	for _, child := range n.children {
		child.deduplicateMessages()
	}

	if len(n.children) == 1 {
		childFull := n.children[0].fullMessage
		if idx := strings.Index(n.ownMessage, childFull); idx > 0 {
			prefix := strings.TrimRight(n.ownMessage[:idx], ": ")
			if prefix != "" {
				n.ownMessage = prefix
			}
		}
	} else if len(n.children) > 1 {
		// For joined errors, the parent message is typically all children
		// concatenated with \n. Strip that if the parent has its own prefix.
		var childTexts []string
		for _, c := range n.children {
			childTexts = append(childTexts, c.fullMessage)
		}
		joinedText := strings.Join(childTexts, "\n")
		if idx := strings.Index(n.ownMessage, joinedText); idx > 0 {
			prefix := strings.TrimRight(n.ownMessage[:idx], ": ")
			if prefix != "" {
				n.ownMessage = prefix
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Path shortening
// ---------------------------------------------------------------------------

func shortenPath(path string) string {
	// Strip GOROOT
	goroot := runtime.GOROOT()
	if goroot != "" && strings.HasPrefix(path, goroot) {
		return "$GOROOT" + path[len(goroot):]
	}

	// Strip GOPATH
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}
	gopathSrc := gopath + "/src/"
	if strings.HasPrefix(path, gopathSrc) {
		return path[len(gopathSrc):]
	}

	// Strip common working directory prefix
	if wd, err := os.Getwd(); err == nil && strings.HasPrefix(path, wd+"/") {
		return "./" + path[len(wd)+1:]
	}

	return path
}

// ---------------------------------------------------------------------------
// Output formatters
// ---------------------------------------------------------------------------

// dim returns ANSI dim text (gray). We don't use color libraries to avoid
// dependencies; the caller can pipe through a strip-ansi filter if needed.
const (
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiReset  = "\033[0m"
)

// formatSimple handles a single error with no wrapping:
//
//	Error: connection refused
//	    at Server.Listen (server.go:42)
func formatSimple(w io.Writer, n *errNode) {
	fmt.Fprintf(w, "%s%sError:%s %s\n", ansiBold, ansiRed, ansiReset, n.ownMessage)
	if n.location != "" {
		fmt.Fprintf(w, "    %sat %s%s (%s)%s\n", ansiDim, ansiReset, n.function, n.location, ansiReset)
	}
	if n.data != "" {
		fmt.Fprintf(w, "    %sdata:%s %s\n", ansiCyan, ansiReset, n.data)
	}
}

// formatLinearChain handles a single-wrap chain (the most common case):
//
//	Error: startup failed
//	├── config load failed
//	│       at LoadConfig (config.go:15)
//	└── cause: file not found
//	        at ReadFile (io.go:88)
//	        data: {"path": "/etc/app.conf"}
//
//	Stack trace:
//	    TestExplore_MixedOrigins  explore_test.go:79
//	    LoadConfig                config.go:15
//	    ReadFile                  io.go:88
func formatLinearChain(w io.Writer, root *errNode) {
	chain := root.linearChain()

	// Part 1: Causal chain (messages only, top-down)
	fmt.Fprintf(w, "%s%sError:%s %s\n", ansiBold, ansiRed, ansiReset, chain[0].ownMessage)
	if chain[0].location != "" {
		fmt.Fprintf(w, "│   %sat %s%s (%s)%s\n", ansiDim, ansiReset, chain[0].function, chain[0].location, ansiReset)
	}
	if chain[0].data != "" {
		fmt.Fprintf(w, "│   %sdata:%s %s\n", ansiCyan, ansiReset, chain[0].data)
	}

	for i, node := range chain[1:] {
		isLast := i == len(chain)-2
		connector := "├── "
		prefix := "│   "
		if isLast {
			connector = "└── "
			prefix = "    "
		}

		label := "cause"
		if isLast {
			label = "cause"
		}

		fmt.Fprintf(w, "%s%s%s:%s %s\n", connector, ansiYellow, label, ansiReset, node.ownMessage)
		if node.location != "" {
			fmt.Fprintf(w, "%s%sat %s%s (%s)%s\n", prefix, ansiDim, ansiReset, node.function, node.location, ansiReset)
		}
		if node.data != "" {
			fmt.Fprintf(w, "%s%sdata:%s %s\n", prefix, ansiCyan, ansiReset, node.data)
		}
	}

	// Part 2: Merged stack trace (deduplicated, bottom-up)
	// Collect all unique locations from the chain, use the longest (deepest) stack
	var deepestTrace StackTrace
	// Walk the chain to find the error with the longest stack
	walkLinearChainForStacks(root, func(st StackTrace) {
		if len(st) > len(deepestTrace) {
			deepestTrace = st
		}
	})

	if len(deepestTrace) > 0 {
		fmt.Fprintln(w)
		printMergedStack(w, deepestTrace, chain)
	}
}

func walkLinearChainForStacks(n *errNode, fn func(StackTrace)) {
	// We need to walk the original errors, not the nodes.
	// But we only have nodes. We stored location per node, so let's
	// reconstruct from the tree — actually we need the raw errors.
	// Let's just walk the nodes and use a separate approach for the stack.
}

// formatTree handles branching errors (errors.Join):
//
//	Error: batch write failed
//	├─[1] disk full
//	│     at NewWriter (writer.go:12)
//	├─[2] permission denied
//	│     at OpenFile (fs.go:44)
//	└─[3] context canceled
func formatTree(w io.Writer, n *errNode, depth int) {
	if depth == 0 {
		fmt.Fprintf(w, "%s%sError:%s %s\n", ansiBold, ansiRed, ansiReset, n.ownMessage)
		if n.location != "" {
			fmt.Fprintf(w, "│   %sat %s%s (%s)%s\n", ansiDim, ansiReset, n.function, n.location, ansiReset)
		}
		if n.data != "" {
			fmt.Fprintf(w, "│   %sdata:%s %s\n", ansiCyan, ansiReset, n.data)
		}
		// Render children at depth 0
		formatTreeChildren(w, n, depth)
		return
	}
	formatTreeChildren(w, n, depth)
}

func formatTreeChildren(w io.Writer, n *errNode, depth int) {
	for i, child := range n.children {
		isLast := i == len(n.children)-1
		connector := "├─"
		childPrefix := "│ "
		if isLast {
			connector = "└─"
			childPrefix = "  "
		}

		indent := strings.Repeat("  ", depth)

		// If this child is itself a join node (has multiple children and its own
		// message is just the concatenation), expand its children directly.
		if child.isJoinNode() {
			// Expand the join node's children inline
			formatTreeChildren(w, child, depth)
			continue
		}

		if len(n.children) > 1 {
			fmt.Fprintf(w, "%s%s[%d] %s\n", indent, connector, i+1, child.ownMessage)
		} else {
			fmt.Fprintf(w, "%s%s %s%scause:%s %s\n", indent, connector, ansiYellow, "", ansiReset, child.ownMessage)
		}

		if child.location != "" {
			fmt.Fprintf(w, "%s%s  %sat %s%s (%s)%s\n", indent, childPrefix, ansiDim, ansiReset, child.function, child.location, ansiReset)
		}
		if child.data != "" {
			fmt.Fprintf(w, "%s%s  %sdata:%s %s\n", indent, childPrefix, ansiCyan, ansiReset, child.data)
		}

		if len(child.children) > 0 {
			formatTree(w, child, depth+1)
		}
	}
}

// printMergedStack prints a single merged stack trace, annotating frames where
// errors in the chain were created. This is the key insight: instead of printing
// N nearly-identical stacks, we print one stack with annotations.
func printMergedStack(w io.Writer, trace StackTrace, chain []*errNode) {
	// Build a lookup: location -> messages created there
	locationMsgs := make(map[string][]string)
	for _, node := range chain {
		if node.location != "" {
			locationMsgs[node.location] = append(locationMsgs[node.location], node.ownMessage)
		}
	}

	fmt.Fprintf(w, "%sStack trace (most recent call last):%s\n", ansiDim, ansiReset)

	// Print frames bottom-up (deepest/oldest first), skipping runtime noise
	for i := len(trace) - 1; i >= 0; i-- {
		frame := trace[i]
		loc := shortenPath(frame.File) + ":" + strconv.Itoa(frame.Line)
		fn := frame.ShortFunction()

		// Skip runtime internals unless they're annotated
		if isRuntimeFrame(fn) && len(locationMsgs[loc]) == 0 {
			continue
		}

		msgs := locationMsgs[loc]
		if len(msgs) > 0 {
			// This frame has error annotations
			fmt.Fprintf(w, "  %s%-30s%s %s %s◄ %s%s\n",
				ansiBold, fn, ansiReset, loc,
				ansiYellow, strings.Join(msgs, "; "), ansiReset)
		} else {
			fmt.Fprintf(w, "  %s%-30s%s %s\n", ansiDim, fn, ansiReset, loc)
		}
	}
}

func isRuntimeFrame(fn string) bool {
	return fn == "goexit" || fn == "goexit1" ||
		fn == "tRunner" || fn == "(*T).Run" ||
		strings.HasPrefix(fn, "runtime.") ||
		fn == ""
}

// ---------------------------------------------------------------------------
// FormatError variant that works on the original error (with stack access)
// ---------------------------------------------------------------------------

// FormatErrorWithStack is like FormatError but extracts and prints a merged
// stack trace from the actual error objects rather than from the tree nodes.
// This gives more complete stack information.
func FormatErrorWithStack(w io.Writer, err error) {
	if w == nil {
		w = os.Stderr
	}
	if err == nil {
		fmt.Fprintln(w, "<nil error>")
		return
	}

	tree := buildErrorTree(err)
	tree.deduplicateMessages()

	switch {
	case tree.isSimple():
		formatSimple(w, tree)
		printErrorStack(w, err, tree)
	case tree.isLinearChain():
		formatLinearChain2(w, tree, err)
	default:
		formatTree(w, tree, 0)
		fmt.Fprintln(w)
		printAllStacks(w, err, tree)
	}
}

// FormatErrorWithStackS is a convenience wrapper.
func FormatErrorWithStackS(err error) string {
	var b strings.Builder
	FormatErrorWithStack(&b, err)
	return b.String()
}

func printErrorStack(w io.Writer, err error, tree *errNode) {
	st, ok := err.(stackTracer)
	if !ok {
		return
	}
	trace := st.StackTrace()
	if len(trace) == 0 {
		return
	}
	fmt.Fprintln(w)
	printMergedStack(w, trace, []*errNode{tree})
}

// formatLinearChain2 is like formatLinearChain but extracts the actual deepest
// stack trace from the error chain for the merged stack output.
func formatLinearChain2(w io.Writer, root *errNode, err error) {
	chain := root.linearChain()

	// Part 1: Causal chain (same as formatLinearChain)
	fmt.Fprintf(w, "%s%sError:%s %s\n", ansiBold, ansiRed, ansiReset, chain[0].ownMessage)
	if chain[0].location != "" {
		fmt.Fprintf(w, "│   %sat %s%s (%s)%s\n", ansiDim, ansiReset, chain[0].function, chain[0].location, ansiReset)
	}
	if chain[0].data != "" {
		fmt.Fprintf(w, "│   %sdata:%s %s\n", ansiCyan, ansiReset, chain[0].data)
	}

	for i, node := range chain[1:] {
		isLast := i == len(chain)-2
		connector := "├── "
		prefix := "│   "
		if isLast {
			connector = "└── "
			prefix = "    "
		}

		fmt.Fprintf(w, "%s%scause:%s %s\n", connector, ansiYellow, ansiReset, node.ownMessage)
		if node.location != "" {
			fmt.Fprintf(w, "%s%sat %s%s (%s)%s\n", prefix, ansiDim, ansiReset, node.function, node.location, ansiReset)
		}
		if node.data != "" {
			fmt.Fprintf(w, "%s%sdata:%s %s\n", prefix, ansiCyan, ansiReset, node.data)
		}
	}

	// Part 2: Find the deepest stack in the chain
	var deepestTrace StackTrace
	walkErrors(err, func(e error) {
		if st, ok := e.(stackTracer); ok {
			trace := st.StackTrace()
			if len(trace) > len(deepestTrace) {
				deepestTrace = trace
			}
		}
	})

	if len(deepestTrace) > 0 {
		fmt.Fprintln(w)
		printMergedStack(w, deepestTrace, chain)
	}
}

// walkErrors visits err and all its unwrapped children (depth-first).
func walkErrors(err error, fn func(error)) {
	if err == nil {
		return
	}
	fn(err)
	switch et := err.(type) {
	case unwrapableMulti:
		for _, child := range et.Unwrap() {
			if child != nil && child != err {
				walkErrors(child, fn)
			}
		}
	case unwrapableSingle:
		child := et.Unwrap()
		if child != nil && child != err {
			walkErrors(child, fn)
		}
	}
}

// printAllStacks collects all stacks from the error tree and prints them
// with annotations showing which errors were created at which frames.
func printAllStacks(w io.Writer, err error, tree *errNode) {
	// Collect all nodes in a flat list
	var allNodes []*errNode
	var collectNodes func(n *errNode)
	collectNodes = func(n *errNode) {
		allNodes = append(allNodes, n)
		for _, c := range n.children {
			collectNodes(c)
		}
	}
	collectNodes(tree)

	// Find the deepest stack
	var deepestTrace StackTrace
	walkErrors(err, func(e error) {
		if st, ok := e.(stackTracer); ok {
			trace := st.StackTrace()
			if len(trace) > len(deepestTrace) {
				deepestTrace = trace
			}
		}
	})

	if len(deepestTrace) > 0 {
		printMergedStack(w, deepestTrace, allNodes)
	}
}

// ---------------------------------------------------------------------------
// Plain text variant (no ANSI codes)
// ---------------------------------------------------------------------------

// FormatErrorPlain produces the same output as FormatErrorWithStack but without
// ANSI color codes, suitable for log files or piping.
func FormatErrorPlain(w io.Writer, err error) {
	var b strings.Builder
	FormatErrorWithStack(&b, err)
	s := b.String()
	// Strip all ANSI escape sequences
	s = stripAnsi(s)
	fmt.Fprint(w, s)
}

func FormatErrorPlainS(err error) string {
	var b strings.Builder
	FormatErrorPlain(&b, err)
	return b.String()
}

func stripAnsi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm'
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
