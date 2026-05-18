package bad

import (
	"os"
	"syscall"
)

func leaks() {
	_ = os.Getenv("FOO")              // want CS011 here
	_, _ = os.LookupEnv("BAR")        // want CS011 here
	_ = os.Environ()                  // want CS011 here
	_, _ = syscall.Getenv("BAZ")      // want CS011 here
}

func suppressed() {
	_ = os.Getenv("FOO") //boxer:lint disable=CS011 reason="testdata coverage of suppression"
}
