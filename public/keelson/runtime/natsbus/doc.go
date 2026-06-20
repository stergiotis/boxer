// Package natsbus is an app.BusI backed by a NATS core connection — the
// cross-process transport for the metric plane (ADR-0090 P3) and, more
// broadly, the post-M4 transport of ADR-0026 §SD4: an external NATS server,
// with boxer as a client (the server is not embedded or supervised here).
//
// It mirrors inprocbus.Client behind the same app.BusI interface, so the
// sysmetricsbus Producer/Consumer — and any other BusI user — run over
// either transport unchanged. Co-located deployments use inprocbus; a real
// deployment points at a NATS server. Pure pub/sub: no JetStream, no KV
// (ADR-0090 SD4).
//
// Authorization is the one deliberate asymmetry vs inprocbus. Subject
// permissions belong on the NATS server (per-app NKey/JWT, ADR-0026 §SD4),
// so this client does not re-check the manifest's SubjectFilter caps
// in-process — unlike inprocbus.Client, which must, having no server to
// defer to. Until JWT provisioning lands the server is open; the AppId
// rides as the connection name so server-side ACLs and monitoring can
// attribute traffic later.
package natsbus
