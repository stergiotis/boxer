package sysmetricsbus

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Bridge relays messages on subject from src to dst — it subscribes on one bus
// and republishes (same subject and payload) on the other — and returns an
// unsubscribe func.
//
// It is the headless-deployment link (ADR-0090). The original problem is that a
// sandboxed carrier cannot read /proc, so the scraper must live elsewhere. The
// answer here keeps the carrier's apps on the in-proc host bus (where the fs
// picker, clipboard, and persistence brokers are co-located by nature) and
// bridges only the system-metrics plane in from an external sysmetricsd over
// NATS: the carrier connects to NATS, subscribes, and republishes onto its
// in-proc bus, so imztop consumes via MountCtx.Bus() while the carrier itself
// never touches /proc.
//
// This is deliberately narrower than ADR-0026 §SD4's "all capabilities over
// NATS" (which would require migrating every broker off inprocbus.Inst); only
// the read-only, separately-sandboxed metric plane crosses the NATS boundary.
//
// src needs subscribe and dst publish permission for subject.
func Bridge(src, dst app.BusI, subject string) (stop func(), err error) {
	if src == nil || dst == nil {
		err = eh.Errorf("sysmetricsbus: bridge needs both src and dst buses")
		return
	}
	stop, err = src.Subscribe(subject, func(m *app.Msg) {
		_ = dst.Publish(m.Subject, m.Payload)
	})
	if err != nil {
		err = eh.Errorf("sysmetricsbus: bridge subscribe %q: %w", subject, err)
		return
	}
	return
}
