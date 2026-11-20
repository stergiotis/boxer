package eh_test

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// ExampleErrorf wraps a cause. The message carries no package or function
// prefix: the stack captured at the call site already says where it came
// from. errors.Is and errors.As see through the wrap as with fmt.Errorf.
func ExampleErrorf() {
	err := eh.Errorf("open config: %w", fs.ErrNotExist)
	fmt.Println(err)
	fmt.Println(errors.Is(err, fs.ErrNotExist))
	// Output:
	// open config: file does not exist
	// true
}

// ExampleWalkStreams reads an error chain back as data: one stream per
// distinct stack, each a list of facts. The message facts are the steps of
// the wrap chain, innermost last.
func ExampleWalkStreams() {
	inner := eh.New("disk full")
	err := eh.Errorf("flush batch: %w", inner)
	for _, s := range eh.WalkStreams(err) {
		for _, f := range s.Facts {
			if f.Msg != "" {
				fmt.Println(f.Msg)
			}
		}
	}
	// Output:
	// flush batch: disk full
	// disk full
}
