package bad

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var ErrOne = eh.Errorf("one") // want a CS019 finding here

var (
	ErrTwo   = eh.New("two")                            // want a CS019 finding here
	errThree = eb.Build().Str("k", "v").Errorf("three") // want a CS019 finding here
)
