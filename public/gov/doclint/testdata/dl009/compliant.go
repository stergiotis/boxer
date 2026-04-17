package fixture

// Foo computes a Foo.
func Foo() (n int) {
	return
}

// Bar holds a bar.
type Bar struct {
	x int
}

// DoWork on Bar performs work and returns nothing.
func (inst *Bar) DoWork() {
}

// PiApprox is an approximation of pi.
const PiApprox = 3.14

// AnswerVar holds the answer.
var AnswerVar = 42

// Grouped types share one comment block: only the first name is checked.

// FirstType is the first one.
type FirstType int

// SecondType is the second one.
type SecondType int
