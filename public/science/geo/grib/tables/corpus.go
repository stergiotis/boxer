package tables

import "github.com/stergiotis/boxer/public/config/env"

// CorpusDir names the directory the table generator and the reader's
// integration lane read from (ADR-0292 §R11): the WMO checkouts
// wmo-im-GRIB2 and wmo-im-CCT, and the survey corpora as
// survey-<date>/samples with their oracle dumps under survey-<date>/expect.
// Nothing in the default lane reads it. It lives here rather than in the
// reader because the generator's tests need it and the reader imports the
// tables.
//
//	BOXER_GRIB_CORPUS=<a directory holding the corpus>
var CorpusDir = env.NewString(env.Spec{
	Name:        "BOXER_GRIB_CORPUS",
	Category:    env.CategoryTestIntegration,
	Description: "directory of GRIB survey corpora with oracle dumps, and the WMO table checkouts, for the integration lane and the table generator",
})
