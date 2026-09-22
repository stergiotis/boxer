package keelsonquery

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Service answers `keelson.query.<table>` over the in-process engine: the
// statement is gated to the subject's table, the referenced table is
// snapshotted and projected in-process (ADR-0094 §SD4), and the result
// comes back in the request's FORMAT. No loopback socket is involved, so
// the reads work wherever the chlocal pool does — the HTTP table source
// may be off (KEELSON_INTROSPECT_ENABLE) and a window still reads.
type Service struct {
	reg       *introspect.Registry
	engine    *introspectengine.Engine
	busClient *inprocbus.Client
	unsub     func()
	log       zerolog.Logger
	// Timeout bounds one engine run; zero is [DefaultTimeout].
	Timeout time.Duration
}

// NewService constructs and subscribes a Service over reg, running on the
// chlocal pool poolName. The caller MUST invoke Close.
func NewService(bus *inprocbus.Inst, log zerolog.Logger, reg *introspect.Registry, poolName string) (s *Service, err error) {
	if bus == nil {
		err = eh.Errorf("keelson.query: nil bus")
		return
	}
	if reg == nil {
		err = eh.Errorf("keelson.query: nil registry")
		return
	}
	if poolName == "" {
		poolName = introspectengine.DefaultPoolName
	}
	s = &Service{reg: reg, log: log.With().Str("app", string(ServiceAppId)).Logger()}
	s.busClient = bus.NewClient(ServiceAppId, ServiceCaps(poolName))
	s.engine, err = introspectengine.New(introspectengine.Config{Registry: reg, Bus: s.busClient, PoolName: poolName}, s.log)
	if err != nil {
		s.busClient.Close()
		return nil, err
	}
	s.unsub, err = s.busClient.Subscribe(SubjectAll, s.handleRequest)
	if err != nil {
		s.busClient.Close()
		err = eb.Build().Str("subject", SubjectAll).Errorf("keelson.query: subscribe: %w", err)
		return nil, err
	}
	return
}

// Close releases the subscription and the bus client. Safe to call more
// than once.
func (inst *Service) Close() {
	if inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
	if inst.busClient != nil {
		inst.busClient.Close()
		inst.busClient = nil
	}
}

func (inst *Service) handleRequest(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("keelson.query: request without reply, dropping")
		return
	}
	table := strings.TrimPrefix(msg.Subject, SubjectPrefix)
	req, err := buscodec.Decode[keelsonqueryrequest.KeelsonQueryRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, table, "malformed request: "+err.Error())
		return
	}
	if req.Table != "" && req.Table != table {
		inst.refuse(msg, table, "table in subject ("+table+") and payload ("+req.Table+") disagree")
		return
	}
	if !introspect.ValidTableName(table) {
		inst.refuse(msg, table, "not a table name")
		return
	}
	if _, ok := inst.reg.Lookup(table); !ok {
		inst.refuse(msg, table, "no introspection table named "+table+" in this process")
		return
	}
	bare, reason := Gate(inst.reg, req.Sql, table)
	if reason != "" {
		inst.refuse(msg, table, reason)
		return
	}
	timeout := inst.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	started := time.Now()
	body, contentType, err := inst.engine.Query(ctx, bare, req.Format)
	if err != nil {
		inst.log.Warn().Str("sender", string(msg.Sender)).Str("table", table).Err(err).Msg("keelson.query: engine failed")
		inst.reply(msg.Reply, keelsonqueryreply.KeelsonQueryReply{At: time.Now().UTC(), Reason: err.Error()})
		return
	}
	inst.log.Debug().Str("sender", string(msg.Sender)).Str("table", table).Int("bytes", len(body)).
		Dur("elapsed", time.Since(started)).Msg("keelson.query: read")
	inst.reply(msg.Reply, keelsonqueryreply.KeelsonQueryReply{At: time.Now().UTC(), Ok: true, ContentType: contentType, Body: body})
}

// refuse answers with the reason. Warn level, because a refusal is either a
// mis-addressed reader or a statement reaching past its grant, and both are
// worth a line.
func (inst *Service) refuse(msg *app.Msg, table string, reason string) {
	inst.log.Warn().Str("sender", string(msg.Sender)).Str("table", table).Str("reason", reason).Msg("keelson.query: refused")
	inst.reply(msg.Reply, keelsonqueryreply.KeelsonQueryReply{At: time.Now().UTC(), Reason: reason})
}

func (inst *Service) reply(inbox string, r keelsonqueryreply.KeelsonQueryReply) {
	payload, err := buscodec.Encode(r)
	if err != nil {
		inst.log.Error().Err(err).Msg("keelson.query: encode reply")
		return
	}
	if err = inst.busClient.Publish(inbox, payload); err != nil {
		inst.log.Warn().Err(err).Str("inbox", inbox).Msg("keelson.query: publish reply")
	}
}
