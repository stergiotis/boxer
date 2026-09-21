package progressbar

import (
	"time"

	"github.com/stergiotis/boxer/public/hmi/progressest"
)

// The estimator and the duration/byte spellings live in hmi/progressest, so
// callers without a terminal (keelson tasks, imzero2 widgets) share them.
// These names keep the CLI bar's API. The estimator itself is spelled
// progressest.Estimator everywhere, including here: CODINGSTANDARDS
// "Typing → No Aliases" rules out re-declaring it under a second name, and a
// named type would shed the methods the caller wants.

func NewEstimator() *progressest.Estimator { return progressest.NewEstimator() }

func FormatDuration(d time.Duration) string { return progressest.FormatDuration(d) }

func FormatETA(d time.Duration) string { return progressest.FormatETA(d) }

func FormatBytes(b int64) string { return progressest.FormatBytes(b) }
