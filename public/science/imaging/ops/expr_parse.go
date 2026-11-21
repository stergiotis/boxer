package ops

import (
	"slices"
	"strconv"
	"strings"
)

// The expression grammar of the expr operator (ADR-0400 §SD10), parsed by
// hand: a lexer over bytes and a recursive descent with one function per
// precedence level. The EBNF is in the package documentation.

// exprOpE is what a node of an expression computes.
type exprOpE uint8

const (
	exprOpNumber exprOpE = iota + 1
	exprOpName
	exprOpNeg
	exprOpNot
	exprOpAdd
	exprOpSub
	exprOpMul
	exprOpDiv
	exprOpLt
	exprOpLe
	exprOpGt
	exprOpGe
	exprOpEq
	exprOpNe
	exprOpAnd
	exprOpOr
	exprOpAbs
	exprOpSqrt
	exprOpExp
	exprOpLog
	exprOpLog10
	exprOpPow
	exprOpMin
	exprOpMax
	exprOpClamp
	exprOpWhere
	exprOpIsValid
)

// exprFunctions are the functions an expression may call, with their arity.
var exprFunctions = map[string]struct {
	op    exprOpE
	arity int
}{
	"abs": {exprOpAbs, 1}, "sqrt": {exprOpSqrt, 1}, "exp": {exprOpExp, 1}, "log": {exprOpLog, 1},
	"log10": {exprOpLog10, 1}, "pow": {exprOpPow, 2}, "min": {exprOpMin, 2}, "max": {exprOpMax, 2},
	"clamp": {exprOpClamp, 3}, "where": {exprOpWhere, 3}, "isvalid": {exprOpIsValid, 1},
}

// ExpressionFunctions lists the names of the functions an expression may
// call, sorted; no input may take one of them as its name.
func ExpressionFunctions() []string {
	names := make([]string, 0, len(exprFunctions))
	for n := range exprFunctions {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

var exprBinary = map[exprOpE]struct {
	symbol string
	prec   int
}{
	exprOpOr: {"||", precOr}, exprOpAnd: {"&&", precAnd},
	exprOpLt: {"<", precCmp}, exprOpLe: {"<=", precCmp}, exprOpGt: {">", precCmp}, exprOpGe: {">=", precCmp},
	exprOpEq: {"==", precCmp}, exprOpNe: {"!=", precCmp},
	exprOpAdd: {"+", precSum}, exprOpSub: {"-", precSum}, exprOpMul: {"*", precTerm}, exprOpDiv: {"/", precTerm},
}

const (
	precOr = iota + 1
	precAnd
	precCmp
	precSum
	precTerm
	precUnary
	precPrimary
)

// Bounds that keep a parse and the compiled program small whatever the
// input: the text, the number of nodes and how deeply they nest.
const (
	maxExprNodes = 1024
	maxExprDepth = 64
)

type exprNode struct {
	op   exprOpE
	num  float64
	name string
	args []*exprNode
	pos  int
}

// Expression is a parsed expression. Its [Expression.String] is the
// canonical spelling: one space around each binary operator, none after a
// unary one or inside parentheses, a comma and a space between arguments,
// numbers as the shortest decimal that reads back to them, and parentheses
// only where precedence needs them. Two texts with one canonical spelling
// are one expression.
type Expression struct {
	root *exprNode
}

// ParseError is where an expression stops parsing and what was expected
// there.
type ParseError struct {
	// Column is the 1-based byte offset in the text.
	Column   int
	Expected string
	Found    string
}

func (inst *ParseError) Error() string {
	return "column " + strconv.Itoa(inst.Column) + ": expected " + inst.Expected + ", found " + inst.Found
}

// ParseExpression parses an expression of the grammar in the package
// documentation. A text that does not parse is a [*ParseError].
func ParseExpression(src string) (e Expression, err error) {
	if len(src) > maxTextBytes {
		return e, &ParseError{Column: maxTextBytes + 1, Expected: "at most " + strconv.Itoa(maxTextBytes) + " bytes", Found: "more"}
	}
	p := exprParser{src: src}
	if err = p.next(); err != nil {
		return e, err
	}
	root, err := p.parseOr(0)
	if err != nil {
		return e, err
	}
	if p.tok.kind != tokEnd {
		return e, p.fail("an operator or the end")
	}
	return Expression{root: root}, nil
}

// IsZero reports whether the expression holds nothing.
func (inst Expression) IsZero() bool { return inst.root == nil }

// Names returns the input names the expression reads, sorted, each once.
func (inst Expression) Names() []string {
	var names []string
	var walk func(n *exprNode)
	walk = func(n *exprNode) {
		if n.op == exprOpName && !slices.Contains(names, n.name) {
			names = append(names, n.name)
		}
		for _, a := range n.args {
			walk(a)
		}
	}
	if inst.root != nil {
		walk(inst.root)
	}
	slices.Sort(names)
	return names
}

// String is the canonical spelling.
func (inst Expression) String() string {
	if inst.root == nil {
		return ""
	}
	var sb strings.Builder
	writeExpr(&sb, inst.root, 0)
	return sb.String()
}

func nodePrec(n *exprNode) int {
	switch n.op {
	case exprOpNeg, exprOpNot:
		return precUnary
	}
	if b, ok := exprBinary[n.op]; ok {
		return b.prec
	}
	return precPrimary
}

// writeExpr writes n, in parentheses when its precedence is below min.
func writeExpr(sb *strings.Builder, n *exprNode, min int) {
	prec := nodePrec(n)
	if prec < min {
		sb.WriteByte('(')
		defer sb.WriteByte(')')
	}
	switch n.op {
	case exprOpNumber:
		sb.WriteString(strconv.FormatFloat(n.num, 'g', -1, 64))
	case exprOpName:
		sb.WriteString(n.name)
	case exprOpNeg:
		sb.WriteByte('-')
		writeExpr(sb, n.args[0], precUnary)
	case exprOpNot:
		sb.WriteByte('!')
		writeExpr(sb, n.args[0], precUnary)
	default:
		if b, ok := exprBinary[n.op]; ok {
			// Left-associative operators take a left operand of their own
			// level unparenthesised; a comparison takes neither, since
			// comparisons do not chain.
			left := b.prec
			if b.prec == precCmp {
				left = b.prec + 1
			}
			writeExpr(sb, n.args[0], left)
			sb.WriteString(" " + b.symbol + " ")
			writeExpr(sb, n.args[1], b.prec+1)
			return
		}
		sb.WriteString(n.name)
		sb.WriteByte('(')
		for i, a := range n.args {
			if i > 0 {
				sb.WriteString(", ")
			}
			writeExpr(sb, a, 0)
		}
		sb.WriteByte(')')
	}
}

type tokE uint8

const (
	tokEnd tokE = iota + 1
	tokNumber
	tokName
	tokSymbol
)

type exprToken struct {
	kind tokE
	text string
	num  float64
	pos  int
}

type exprParser struct {
	src   string
	pos   int
	tok   exprToken
	nodes int
}

func (inst *exprParser) fail(expected string) error {
	found := "the end"
	switch inst.tok.kind {
	case tokNumber:
		found = "number " + inst.tok.text
	case tokName:
		found = "name " + inst.tok.text
	case tokSymbol:
		found = "'" + inst.tok.text + "'"
	}
	return &ParseError{Column: inst.tok.pos + 1, Expected: expected, Found: found}
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

// next reads the next token.
func (inst *exprParser) next() error {
	src := inst.src
	for inst.pos < len(src) && (src[inst.pos] == ' ' || src[inst.pos] == '\t' || src[inst.pos] == '\n' || src[inst.pos] == '\r') {
		inst.pos++
	}
	start := inst.pos
	if start == len(src) {
		inst.tok = exprToken{kind: tokEnd, pos: start}
		return nil
	}
	c := src[start]
	switch {
	case isLetter(c):
		for inst.pos < len(src) && (isLetter(src[inst.pos]) || isDigit(src[inst.pos])) {
			inst.pos++
		}
		inst.tok = exprToken{kind: tokName, text: src[start:inst.pos], pos: start}
		return nil
	case isDigit(c) || c == '.':
		return inst.number(start)
	}
	for _, sym := range []string{"<=", ">=", "==", "!=", "&&", "||", "+", "-", "*", "/", "(", ")", ",", "<", ">", "!"} {
		if strings.HasPrefix(src[start:], sym) {
			inst.pos += len(sym)
			inst.tok = exprToken{kind: tokSymbol, text: sym, pos: start}
			return nil
		}
	}
	found := "'" + string(c) + "'"
	if c < 0x20 || c >= 0x7f {
		found = "byte 0x" + strconv.FormatUint(uint64(c), 16)
	}
	return &ParseError{Column: start + 1, Expected: "a number, a name, an operator or a parenthesis", Found: found}
}

// number reads digits [ "." digits ] [ exponent ], or "." digits [ exponent ].
func (inst *exprParser) number(start int) error {
	src := inst.src
	digits := func() int {
		n := 0
		for inst.pos < len(src) && isDigit(src[inst.pos]) {
			inst.pos++
			n++
		}
		return n
	}
	whole := digits()
	frac := 0
	if inst.pos < len(src) && src[inst.pos] == '.' {
		inst.pos++
		frac = digits()
	}
	if whole == 0 && frac == 0 {
		return &ParseError{Column: start + 1, Expected: "a digit", Found: "'.'"}
	}
	if inst.pos < len(src) && (src[inst.pos] == 'e' || src[inst.pos] == 'E') {
		inst.pos++
		if inst.pos < len(src) && (src[inst.pos] == '+' || src[inst.pos] == '-') {
			inst.pos++
		}
		if digits() == 0 {
			return &ParseError{Column: inst.pos + 1, Expected: "the digits of an exponent", Found: foundAt(src, inst.pos)}
		}
	}
	if inst.pos < len(src) && (isLetter(src[inst.pos]) || src[inst.pos] == '.') {
		return &ParseError{Column: inst.pos + 1, Expected: "an operator after a number", Found: foundAt(src, inst.pos)}
	}
	text := src[start:inst.pos]
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || !isFinite(v) {
		return &ParseError{Column: start + 1, Expected: "a number a float64 holds", Found: "number " + text}
	}
	inst.tok = exprToken{kind: tokNumber, text: text, num: v, pos: start}
	return nil
}

func foundAt(src string, pos int) string {
	if pos >= len(src) {
		return "the end"
	}
	return "'" + string(src[pos]) + "'"
}

func (inst *exprParser) node(op exprOpE, pos int, args ...*exprNode) (*exprNode, error) {
	inst.nodes++
	if inst.nodes > maxExprNodes {
		return nil, &ParseError{Column: pos + 1, Expected: "at most " + strconv.Itoa(maxExprNodes) + " terms", Found: "more"}
	}
	return &exprNode{op: op, pos: pos, args: args}, nil
}

func (inst *exprParser) isSymbol(s string) bool {
	return inst.tok.kind == tokSymbol && inst.tok.text == s
}

func (inst *exprParser) deeper(depth int) error {
	if depth > maxExprDepth {
		return &ParseError{Column: inst.tok.pos + 1, Expected: "at most " + strconv.Itoa(maxExprDepth) + " levels of nesting", Found: "more"}
	}
	return nil
}

// binaryLevel parses operand { op operand } for the operators of one
// left-associative level.
func (inst *exprParser) binaryLevel(depth int, ops map[string]exprOpE, operand func(int) (*exprNode, error)) (*exprNode, error) {
	left, err := operand(depth)
	if err != nil {
		return nil, err
	}
	for inst.tok.kind == tokSymbol {
		op, ok := ops[inst.tok.text]
		if !ok {
			break
		}
		pos := inst.tok.pos
		if err = inst.next(); err != nil {
			return nil, err
		}
		right, err := operand(depth)
		if err != nil {
			return nil, err
		}
		if left, err = inst.node(op, pos, left, right); err != nil {
			return nil, err
		}
	}
	return left, nil
}

func (inst *exprParser) parseOr(depth int) (*exprNode, error) {
	if err := inst.deeper(depth); err != nil {
		return nil, err
	}
	return inst.binaryLevel(depth, exprOrOps, inst.parseAnd)
}

func (inst *exprParser) parseAnd(depth int) (*exprNode, error) {
	return inst.binaryLevel(depth, exprAndOps, inst.parseCmp)
}

var (
	exprOrOps   = map[string]exprOpE{"||": exprOpOr}
	exprAndOps  = map[string]exprOpE{"&&": exprOpAnd}
	exprSumOps  = map[string]exprOpE{"+": exprOpAdd, "-": exprOpSub}
	exprTermOps = map[string]exprOpE{"*": exprOpMul, "/": exprOpDiv}
)

var exprComparisons = map[string]exprOpE{"<": exprOpLt, "<=": exprOpLe, ">": exprOpGt, ">=": exprOpGe, "==": exprOpEq, "!=": exprOpNe}

func (inst *exprParser) parseCmp(depth int) (*exprNode, error) {
	left, err := inst.parseSum(depth)
	if err != nil {
		return nil, err
	}
	if inst.tok.kind != tokSymbol {
		return left, nil
	}
	op, ok := exprComparisons[inst.tok.text]
	if !ok {
		return left, nil
	}
	pos := inst.tok.pos
	if err = inst.next(); err != nil {
		return nil, err
	}
	right, err := inst.parseSum(depth)
	if err != nil {
		return nil, err
	}
	if inst.tok.kind == tokSymbol {
		if _, chained := exprComparisons[inst.tok.text]; chained {
			return nil, inst.fail("no second comparison: comparisons do not chain, join them with &&")
		}
	}
	return inst.node(op, pos, left, right)
}

func (inst *exprParser) parseSum(depth int) (*exprNode, error) {
	return inst.binaryLevel(depth, exprSumOps, inst.parseTerm)
}

func (inst *exprParser) parseTerm(depth int) (*exprNode, error) {
	return inst.binaryLevel(depth, exprTermOps, inst.parseUnary)
}

func (inst *exprParser) parseUnary(depth int) (*exprNode, error) {
	if inst.isSymbol("-") || inst.isSymbol("!") {
		op, pos := exprOpNeg, inst.tok.pos
		if inst.tok.text == "!" {
			op = exprOpNot
		}
		if err := inst.deeper(depth + 1); err != nil {
			return nil, err
		}
		if err := inst.next(); err != nil {
			return nil, err
		}
		operand, err := inst.parseUnary(depth + 1)
		if err != nil {
			return nil, err
		}
		return inst.node(op, pos, operand)
	}
	return inst.parsePrimary(depth)
}

func (inst *exprParser) parsePrimary(depth int) (*exprNode, error) {
	tok := inst.tok
	switch {
	case tok.kind == tokNumber:
		n, err := inst.node(exprOpNumber, tok.pos)
		if err != nil {
			return nil, err
		}
		n.num = tok.num
		return n, inst.next()
	case tok.kind == tokName:
		if err := inst.next(); err != nil {
			return nil, err
		}
		fn, isFunc := exprFunctions[tok.text]
		if !inst.isSymbol("(") {
			if isFunc {
				return nil, inst.fail("'(' and the arguments of " + tok.text)
			}
			n, err := inst.node(exprOpName, tok.pos)
			if err != nil {
				return nil, err
			}
			n.name = tok.text
			return n, nil
		}
		if !isFunc {
			return nil, &ParseError{Column: tok.pos + 1, Expected: "one of the functions " + strings.Join(ExpressionFunctions(), ", "), Found: "name " + tok.text}
		}
		return inst.parseCall(depth, tok, fn.op, fn.arity)
	case inst.isSymbol("("):
		if err := inst.next(); err != nil {
			return nil, err
		}
		n, err := inst.parseOr(depth + 1)
		if err != nil {
			return nil, err
		}
		if !inst.isSymbol(")") {
			return nil, inst.fail("')'")
		}
		return n, inst.next()
	}
	return nil, inst.fail("an operand: a number, a name, a function call or '('")
}

func (inst *exprParser) parseCall(depth int, name exprToken, op exprOpE, arity int) (*exprNode, error) {
	// The current token is the opening parenthesis.
	args := make([]*exprNode, 0, arity)
	for {
		if err := inst.next(); err != nil {
			return nil, err
		}
		a, err := inst.parseOr(depth + 1)
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if inst.isSymbol(",") {
			if len(args) == arity {
				return nil, inst.fail("')': " + name.text + " takes " + strconv.Itoa(arity) + " argument" + plural(arity))
			}
			continue
		}
		if !inst.isSymbol(")") {
			return nil, inst.fail("',' or ')'")
		}
		if len(args) != arity {
			return nil, inst.fail("',': " + name.text + " takes " + strconv.Itoa(arity) + " argument" + plural(arity))
		}
		break
	}
	n, err := inst.node(op, name.pos, args...)
	if err != nil {
		return nil, err
	}
	n.name = name.text
	return n, inst.next()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
