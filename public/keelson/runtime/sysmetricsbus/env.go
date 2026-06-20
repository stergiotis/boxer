package sysmetricsbus

import "github.com/stergiotis/boxer/public/config/env"

// NatsURL selects the metric-plane transport for consumers (ADR-0090 P3).
// When set, a consumer (imztop) subscribes over NATS to a standalone
// sysmetricsd scraper; when empty, imztop keeps the co-located in-proc bus
// and samples in-process (the P2 path). The sysmetricsd command reads it as
// the default --url.
var NatsURL = env.NewString(env.Spec{
	Name:        "IMZERO2_SYSMETRICS_NATS_URL",
	Description: "NATS core URL for the system-metrics plane; when set, consumers subscribe to a standalone sysmetricsd scraper instead of sampling in-process (ADR-0090)",
	Category:    env.CategorySystem,
})
