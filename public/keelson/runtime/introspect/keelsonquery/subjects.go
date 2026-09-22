// Package keelsonquery is the bus side of the introspection tables
// (ADR-0253): the `keelson.query.<table>` request/reply family an app reads
// a `keelson()` table through, and the service that answers it over the
// in-process engine (ADR-0094 §SD4).
//
// One subject per table is the grant's granularity. An app that reads the
// watchbill trail declares that table and no other, the broker can show a
// prompt that says exactly that, and the service holds the statement to it:
// a request on `keelson.query.x` may name x and nothing else — no second
// table, no table function, no mutation. Joins are play's business over the
// HTTP endpoint, which stays for the SQL-console class of consumer.
//
// Reading is a request, so the bus audits it with the app as sender, and
// capslock sees no network capability in the app at all — the loopback
// HTTP read this replaces was invisible to both.
package keelsonquery

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the identity the host's service holds on the bus. It is
// the identity the HTTP `/query` runner already publishes under, since both
// are the same plane answering the same question by two transports.
const ServiceAppId app.AppIdT = "runtime.introspect.query"

// The family (ADR-0253 §SD1): `keelson.query.<table>`, read-only. The
// payload is a keelsonqueryrequest.KeelsonQueryRequest, the reply a
// keelsonqueryreply.KeelsonQueryReply, and the table in the subject and in
// the payload must agree.
const (
	// SubjectPrefix precedes the table name.
	SubjectPrefix = "keelson.query."
	// SubjectAll is the service's subscription pattern.
	SubjectAll = "keelson.query.*"
)

// Subject is the request subject for table.
func Subject(table string) (subject string) { return SubjectPrefix + table }

// DefaultTimeout bounds one read, on either side: the client's wait and the
// service's engine run. It matches the bus's own request default, so a
// reader that leaves Client.Timeout zero waits no longer than the transport
// would have let it.
const DefaultTimeout = 5 * time.Second

// ServiceCaps is what the host's service holds: the requests to serve, the
// inboxes to answer on, and the chlocal pool the engine runs on.
func ServiceCaps(poolName string) (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "keelson.query: serve reads over the introspection tables"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "keelson.query: reply to inboxes"},
		{Pattern: chlocalbroker.SubjectExecPrefix + poolName, Direction: app.CapDirectionPub, Reason: "keelson.query: run the statement on clickhouse-local"},
	}
	return
}

// ClientCaps is what an app declares in Manifest.Caps: one sticky
// publish grant per table it reads. Sticky, because reading an
// introspection table is the low end of what an app can ask for — the
// tables are redacted at the provider (ADR-0094 §SD3) — and one prompt per
// table per install is proportionate. The reply inbox needs no cap of its
// own; the in-proc client bypasses it for its own requests, and a NATS
// server grants it with the request.
func ClientCaps(tables ...string) (caps []app.SubjectFilter) {
	caps = make([]app.SubjectFilter, 0, len(tables))
	for _, t := range tables {
		caps = append(caps, app.SubjectFilter{
			Pattern: Subject(t), Direction: app.CapDirectionPub, Sticky: true,
			Reason: "keelson.query: read keelson('" + t + "')",
		})
	}
	return
}
