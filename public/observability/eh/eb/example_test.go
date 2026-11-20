package eb_test

import (
	"fmt"
	"io/fs"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ExampleBuild attaches structured fields to an error. The values travel as
// data beside the message, not inside it: Error() is the message alone, and
// a log sink or eh.WalkStreams reads the fields back by name.
func ExampleBuild() {
	err := eb.Build().
		Str("path", "/etc/app.toml").
		Int("attempt", 3).
		Errorf("open config: %w", fs.ErrNotExist)
	fmt.Println(err)
	// Output:
	// open config: file does not exist
}

// ExampleErrorBuilder_WithoutStack builds an error that captures no stack,
// for a hot path or for an error that is a value rather than a failure. Its
// facts land in the "no-stack" stream.
func ExampleErrorBuilder_WithoutStack() {
	err := eb.Build().WithoutStack().Str("key", "k1").Errorf("cache miss")
	for _, s := range eh.WalkStreams(err) {
		fmt.Println(s.Name)
	}
	// Output:
	// no-stack
}
