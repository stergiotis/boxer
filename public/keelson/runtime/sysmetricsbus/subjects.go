package sysmetricsbus

import (
	"os"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// SubjectRoot is the top-level subject family for the metric plane
// (ADR-0090 SD1). The full taxonomy is sysmetrics.{host}.{domain}; P2
// uses the {host}.bundle leaf (see BundleSubject) until the per-domain
// split lands.
const SubjectRoot = "sysmetrics"

// SubjectWildcard matches every metric subject. Used as the cap pattern
// for the producer (publish) and consumer (subscribe).
const SubjectWildcard = SubjectRoot + ".>"

// ServiceAppId is the bus identity of the scraper service (ADR-0090 SD2),
// mirroring clipboardbroker's "runtime.clipboard". It is the only identity
// that should hold the sysmetrics publish capability.
const ServiceAppId app.AppIdT = "runtime.sysmetrics"

// BundleSubject returns the P2 whole-bundle subject for a host token:
// "sysmetrics.{host}.bundle". An empty token falls back to "local".
//
// P2 carries the entire BundleSnapshot on this one subject to land the
// producer/consumer bisection first; ADR-0090 SD1's per-domain subjects
// (sysmetrics.{host}.{domain}) are the planned follow-up.
func BundleSubject(hostToken string) (subject string) {
	if hostToken == "" {
		hostToken = "local"
	}
	subject = SubjectRoot + "." + hostToken + ".bundle"
	return
}

// HostToken sanitises s into a single bus subject token: every character
// outside [A-Za-z0-9_-] becomes '_' (the inprocbus/NATS token rule). An
// empty result falls back to "local".
func HostToken(s string) (token string) {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			b = append(b, c)
		} else {
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "local"
	}
	token = string(b)
	return
}

// DefaultHostToken is the local hostname sanitised via HostToken, or
// "local" when the hostname is unavailable. It only disambiguates boxes on
// a shared bus, so the co-located P2 case is insensitive to its value.
func DefaultHostToken() (token string) {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "local"
	}
	token = HostToken(h)
	return
}
