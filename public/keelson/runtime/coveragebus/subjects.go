// Package coveragebus is the bus plane of the continuous-coverage stream
// (ADR-0169 §SD4): subjects, the codec seam, producer and consumer. It
// mirrors sysmetricsbus and stays free of runtime/coverage and decoder
// imports — the concrete sampler is wired in covscrape, so a consumer
// importing this package pulls in no acquisition code.
package coveragebus

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// SubjectRoot is the top-level subject family of the coverage plane.
const SubjectRoot = "coverage"

// SubjectWildcard matches every coverage subject; the cap pattern for the
// producer (publish) and consumers (subscribe).
const SubjectWildcard = SubjectRoot + ".>"

// ServiceAppId is the bus identity of the coverage sampler service,
// mirroring "runtime.sysmetrics". It is the only identity that should hold
// the coverage publish capability.
const ServiceAppId app.AppIdT = "runtime.coverage"

// SampleSubject returns the per-host update subject
// ("coverage.{host}.sample"). An empty token falls back to "local". Host
// tokens follow the metric plane's rule — callers pass
// sysmetricsbus.DefaultHostToken() or an equally sanitised token.
func SampleSubject(hostToken string) (subject string) {
	if hostToken == "" {
		hostToken = "local"
	}
	subject = SubjectRoot + "." + hostToken + ".sample"
	return
}

// SampleSubjectWildcard matches every host's update subject
// (coverage.*.sample).
func SampleSubjectWildcard() (subject string) {
	subject = SubjectRoot + ".*.sample"
	return
}
