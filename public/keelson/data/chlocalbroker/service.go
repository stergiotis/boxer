// Package chlocalbroker is the runtime's broker for ADR-0028's
// `ch.local.exec.<pool>` capability surface. It subscribes to
// `ch.local.exec.>` on the in-proc bus (ADR-0026 §SD5), lazy-creates
// a `chlocalpool.Pool` per pool name, executes the requested SQL on
// a one-shot worker, drains stdout into a tier-pooled buffer
// (`valyala/bytebufferpool`), and publishes the bytes back on the
// caller's reply inbox.
//
// M2 of ADR-0028 (per §SD9): buffered path only. The Streaming
// request flag is accepted on the wire but rejected with a
// structured error — streaming over the bus's bytes payload is
// deferred to a later milestone.
package chlocalbroker

import (
	"context"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// Subject taxonomy this service binds (ADR-0028 §SD1).
const (
	// SubjectExecPrefix is the dotted prefix preceding the pool name.
	SubjectExecPrefix = "ch.local.exec."
	// SubjectExecAll is the NATS wildcard subscription pattern.
	SubjectExecAll = "ch.local.exec.>"
)

// ServiceAppId is the synthetic app identity the broker registers
// under. Audit rows attribute broker-side writes to this id.
const ServiceAppId app.AppIdT = "runtime.chlocal"

// DefaultRequestTimeout caps how long the broker waits for one
// pool.Acquire+execute cycle. Independent of the bus's request
// timeout; the bus may give up sooner.
const DefaultRequestTimeout = 30 * time.Second

// Service is the broker's runtime presence. NewService subscribes;
// Stop unsubscribes and tears down all pools it spawned.
type Service struct {
	bus       *inprocbus.Inst
	log       zerolog.Logger
	poolCfg   chlocalpool.Config
	cacheCfg  CacheConfig
	timeout   time.Duration
	busClient *inprocbus.Client
	unsub     func()

	mu      sync.Mutex
	pools   map[string]*chlocalpool.Pool
	caches  map[string]*poolCache
	stopped bool
}

// NewService registers the broker on the given bus and returns the
// Service handle. The first request to any `ch.local.exec.<pool>`
// subject lazily constructs the corresponding pool using poolCfg as
// the template.
func NewService(bus *inprocbus.Inst, poolCfg chlocalpool.Config, log zerolog.Logger) (svc *Service, err error) {
	if bus == nil {
		err = eh.Errorf("chlocalbroker: bus is nil")
		return
	}
	svc = &Service{
		bus:      bus,
		log:      log,
		poolCfg:  poolCfg,
		cacheCfg: CacheConfig{}.withDefaults(),
		timeout:  DefaultRequestTimeout,
		pools:    make(map[string]*chlocalpool.Pool),
		caches:   make(map[string]*poolCache),
	}
	caps := []app.SubjectFilter{
		{
			Pattern:   SubjectExecAll,
			Direction: app.CapDirectionBoth,
			Reason:    "broker: handle ch.local.exec.<pool> requests",
		},
		{
			Pattern:   inprocbus.InboxPrefix + ">",
			Direction: app.CapDirectionPub,
			Reason:    "broker: publish replies to caller inboxes",
		},
	}
	svc.busClient = bus.NewClient(ServiceAppId, caps)
	unsub, subErr := svc.busClient.Subscribe(SubjectExecAll, svc.handleRequest)
	if subErr != nil {
		err = eh.Errorf("chlocalbroker: subscribe: %w", subErr)
		svc = nil
		return
	}
	svc.unsub = unsub
	return
}

// SetRequestTimeout overrides DefaultRequestTimeout; useful when the
// pool is expected to handle long-running queries (large data
// conversions).
func (inst *Service) SetRequestTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	inst.mu.Lock()
	inst.timeout = d
	inst.mu.Unlock()
}

// SetCacheConfig overrides the cache defaults applied to caches
// constructed by subsequent requests. Has no effect on caches that
// have already been lazy-created for a pool — those keep the config
// they were built with. Tests use this to dial down TTL / size for
// determinism.
func (inst *Service) SetCacheConfig(cfg CacheConfig) {
	inst.mu.Lock()
	inst.cacheCfg = cfg.withDefaults()
	inst.mu.Unlock()
}

// Stop unsubscribes from the bus and stops every pool the broker
// spawned. Idempotent.
func (inst *Service) Stop(ctx context.Context) (err error) {
	inst.mu.Lock()
	if inst.stopped {
		inst.mu.Unlock()
		return
	}
	inst.stopped = true
	if inst.unsub != nil {
		inst.unsub()
	}
	pools := make([]*chlocalpool.Pool, 0, len(inst.pools))
	for _, p := range inst.pools {
		pools = append(pools, p)
	}
	inst.mu.Unlock()

	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for _, p := range pools {
		wg.Add(1)
		go func(p *chlocalpool.Pool) {
			defer wg.Done()
			if e := p.Stop(ctx); e != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = e
				}
				errMu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	if firstErr != nil {
		err = eh.Errorf("chlocalbroker: stop pools: %w", firstErr)
	}
	return
}

// Stats reports cardinality across all pools.
type Stats struct {
	Pools    int
	PerPool  map[string]chlocalpool.Stats
}

func (inst *Service) Stats() (s Stats) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	s.Pools = len(inst.pools)
	s.PerPool = make(map[string]chlocalpool.Stats, len(inst.pools))
	for name, p := range inst.pools {
		s.PerPool[name] = p.Stats()
	}
	return
}

// auditFields collects the per-request observable fields that the
// broker emits via structured zerolog at the end of every request.
// The log bridge (ADR-0026 §SD6 logs-as-facts) routes the event into
// runtime.facts under MembKindLog; each field becomes a typed
// runtime.log.field mixed-membership preserving the columnar query
// surface. Filling these fields is the broker's contribution to
// ADR-0028 §SD7's audit requirement; the bus-level AuditSink covers
// the generic subject/sender/result/sizes dimensions.
type auditFields struct {
	subject    string
	sender     app.AppIdT
	sqlBlake3  string // hex-encoded; empty if request never decoded
	format      string
	cacheable   bool
	streaming   bool
	inputTables int
	cacheHit    bool
	latencyNs  int64
	bytesOut   int
	exitCode   int32
	errMsg     string // empty on success
	stderrTail string
}

// emitAudit logs one event per request. Warn level for failures so
// they're easy to filter; Info level for successes.
func (inst *Service) emitAudit(f auditFields) {
	var ev *zerolog.Event
	if f.errMsg != "" {
		ev = inst.log.Warn()
	} else {
		ev = inst.log.Info()
	}
	ev = ev.
		Str("subject", f.subject).
		Str("sender", string(f.sender)).
		Bool("cacheable", f.cacheable).
		Bool("streaming", f.streaming).
		Bool("cache_hit", f.cacheHit).
		Int64("latency_ns", f.latencyNs).
		Int("bytes_out", f.bytesOut)
	if f.inputTables > 0 {
		ev = ev.Int("input_tables", f.inputTables)
	}
	if f.format != "" {
		ev = ev.Str("format", f.format)
	}
	if f.sqlBlake3 != "" {
		ev = ev.Str("sql_blake3", f.sqlBlake3)
	}
	if f.exitCode != 0 {
		ev = ev.Int32("exit_code", f.exitCode)
	}
	if f.stderrTail != "" {
		ev = ev.Str("stderr_tail", f.stderrTail)
	}
	if f.errMsg != "" {
		ev.Str("error", f.errMsg).Msg("chlocalbroker: exec failed")
	} else {
		ev.Msg("chlocalbroker: exec")
	}
}

// handleRequest is the bus subscription callback. Bounded by the
// service's request timeout; SQL execution is gated by chlocalpool's
// own ctx-respecting Acquire. Every code path through this handler
// updates `aud` and the deferred emitAudit produces exactly one
// audit row per request.
func (inst *Service) handleRequest(msg *app.Msg) {
	started := time.Now()
	if msg.Reply == "" {
		inst.log.Warn().Str("subject", msg.Subject).Msg("chlocalbroker: request without reply inbox")
		return
	}

	aud := auditFields{
		subject: msg.Subject,
		sender:  msg.Sender,
	}
	defer func() {
		aud.latencyNs = time.Since(started).Nanoseconds()
		inst.emitAudit(aud)
	}()

	poolName := strings.TrimPrefix(msg.Subject, SubjectExecPrefix)
	if poolName == msg.Subject || poolName == "" {
		aud.errMsg = "subject does not match ch.local.exec.<pool>"
		inst.sendError(msg.Reply, aud.errMsg, "", 0)
		return
	}

	req, err := decodeRequest(msg.Payload)
	if err != nil {
		aud.errMsg = err.Error()
		inst.sendError(msg.Reply, aud.errMsg, "", 0)
		return
	}
	aud.format = req.Format
	aud.cacheable = req.Cacheable
	aud.streaming = req.Streaming

	if req.Streaming {
		aud.errMsg = "streaming not implemented in M2; set Streaming=false"
		inst.sendError(msg.Reply, aud.errMsg, "", 0)
		return
	}

	// Validate input table names up front (ADR-0094 §SD5): a bad name
	// must never reach the cache-key fold or the SQL prelude.
	aud.inputTables = len(req.InputTables)
	for name := range req.InputTables {
		if !validInputTableName(name) {
			aud.errMsg = "invalid input table name: " + name
			inst.sendError(msg.Reply, aud.errMsg, "", 0)
			return
		}
	}

	// Compute sql_blake3 once; reused as cache key when cacheable.
	// InputTables fold into the key so a volatile input never serves a
	// stale hit under unchanged SQL (ADR-0094 §SD5).
	key := foldInputTables(computeCacheKey(req.SQL, req.Format, req.Settings), req.InputTables)
	aud.sqlBlake3 = hex.EncodeToString(key[:])

	// Cache lookup (ADR-0028 §SD5). Eligibility = caller opted in
	// AND SQL prefix is allowlisted (cheap mutation guard).
	cacheable := req.Cacheable && sqlIsCacheable(req.SQL)
	var cache *poolCache
	if cacheable {
		cache = inst.cacheFor(poolName)
		if cache != nil {
			if entry, ok := cache.get(key); ok {
				aud.cacheHit = true
				aud.bytesOut = len(entry.body)
				inst.replyCacheHit(msg.Reply, entry, started)
				return
			}
		}
	}

	pool, err := inst.poolFor(poolName)
	if err != nil {
		aud.errMsg = err.Error()
		inst.sendError(msg.Reply, aud.errMsg, "", 0)
		return
	}

	inst.mu.Lock()
	timeout := inst.timeout
	inst.mu.Unlock()
	brokerDeadline := time.Now().Add(timeout)
	effectiveDeadline := brokerDeadline
	if req.DeadlineUnixNanos > 0 {
		callerDeadline := time.Unix(0, req.DeadlineUnixNanos)
		if callerDeadline.Before(effectiveDeadline) {
			effectiveDeadline = callerDeadline
		}
	}
	ctx, cancel := context.WithDeadline(context.Background(), effectiveDeadline)
	defer cancel()

	w, err := pool.Acquire(ctx)
	if err != nil {
		aud.errMsg = "acquire: " + err.Error()
		inst.sendError(msg.Reply, aud.errMsg, "", 0)
		return
	}
	defer func() { _ = w.Close() }()

	// Bind any InputTables as TEMPORARY tables ahead of the query
	// (ADR-0094 §SD5). The files must outlive the worker's read, so
	// cleanup is deferred to handler return — i.e. after Wait below.
	sql := req.SQL
	if len(req.InputTables) > 0 {
		prelude, cleanup, mErr := materializeInputTables(inst.poolCfg.BaseTmpDir, req.InputTables)
		defer cleanup()
		if mErr != nil {
			aud.errMsg = mErr.Error()
			inst.sendError(msg.Reply, aud.errMsg, "", 0)
			return
		}
		sql = prelude + req.SQL
	}

	if err = w.WriteSQL(sql, req.Format); err != nil {
		aud.errMsg = "write sql: " + err.Error()
		aud.stderrTail = string(w.StderrTail())
		inst.sendError(msg.Reply, aud.errMsg, aud.stderrTail, 0)
		return
	}

	bb := bytebufferpool.Get()
	defer bytebufferpool.Put(bb)

	if _, err = bb.ReadFrom(w.Stdout()); err != nil {
		aud.errMsg = "drain stdout: " + err.Error()
		aud.stderrTail = string(w.StderrTail())
		inst.sendError(msg.Reply, aud.errMsg, aud.stderrTail, 0)
		return
	}
	if err = w.Wait(); err != nil {
		aud.errMsg = err.Error()
		aud.stderrTail = string(w.StderrTail())
		inst.sendError(msg.Reply, aud.errMsg, aud.stderrTail, 0)
		return
	}

	// Copy out of the pool buffer; the bus may retain the payload slice
	// past Publish, and we must return the buffer to the pool safely.
	body := make([]byte, len(bb.B))
	copy(body, bb.B)
	aud.bytesOut = len(body)

	contentType := contentTypeFor(req.Format)

	// Populate cache after the worker has reported clean exit.
	if cacheable && cache != nil {
		cache.put(key, body, contentType)
	}

	rep := wireReply{
		OK:          true,
		Body:        body,
		ContentType: contentType,
		ElapsedNs:   time.Since(started).Nanoseconds(),
	}
	payload, err := encodeReply(rep)
	if err != nil {
		inst.log.Warn().Err(err).Msg("chlocalbroker: encode reply")
		inst.sendError(msg.Reply, "encode reply: "+err.Error(), "", 0)
		return
	}
	if err = inst.busClient.Publish(msg.Reply, payload); err != nil {
		inst.log.Warn().Err(err).Str("reply", msg.Reply).Msg("chlocalbroker: publish reply")
	}
}

// poolFor returns the pool for poolName, lazy-constructing it on
// first request. Safe under concurrent use; a race between two
// constructors throws one away.
func (inst *Service) poolFor(name string) (p *chlocalpool.Pool, err error) {
	inst.mu.Lock()
	if inst.stopped {
		inst.mu.Unlock()
		err = eh.Errorf("chlocalbroker: service stopped")
		return
	}
	if existing, ok := inst.pools[name]; ok {
		inst.mu.Unlock()
		p = existing
		return
	}
	inst.mu.Unlock()

	poolCfg := inst.poolCfg
	poolLog := inst.log.With().Str("pool", name).Logger()
	candidate, createErr := chlocalpool.New(poolCfg, poolLog)
	if createErr != nil {
		err = eh.Errorf("chlocalbroker: create pool %q: %w", name, createErr)
		return
	}

	inst.mu.Lock()
	if inst.stopped {
		inst.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = candidate.Stop(ctx)
		cancel()
		err = eh.Errorf("chlocalbroker: service stopped during pool init")
		return
	}
	if existing, ok := inst.pools[name]; ok {
		inst.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = candidate.Stop(ctx)
		cancel()
		p = existing
		return
	}
	inst.pools[name] = candidate
	inst.mu.Unlock()
	p = candidate
	return
}

// cacheFor returns the per-pool LRU cache, lazy-creating it on first
// request. Returns nil if construction fails (logged at warn); the
// handler treats nil as "not cacheable" and falls through to the
// worker.
func (inst *Service) cacheFor(name string) (c *poolCache) {
	inst.mu.Lock()
	if existing, ok := inst.caches[name]; ok {
		inst.mu.Unlock()
		c = existing
		return
	}
	cfg := inst.cacheCfg
	inst.mu.Unlock()

	candidate, err := newPoolCache(cfg)
	if err != nil {
		inst.log.Warn().Err(err).Str("pool", name).Msg("chlocalbroker: cache create failed; bypass")
		return
	}

	inst.mu.Lock()
	if existing, ok := inst.caches[name]; ok {
		inst.mu.Unlock()
		c = existing
		return
	}
	inst.caches[name] = candidate
	inst.mu.Unlock()
	c = candidate
	return
}

// replyCacheHit publishes a success reply built from a cached entry.
// Sets CacheHit=true and an ElapsedNs measured from the start of
// this request so callers can observe hit latency.
func (inst *Service) replyCacheHit(replySubject string, entry *cacheEntry, started time.Time) {
	rep := wireReply{
		OK:          true,
		Body:        entry.body,
		ContentType: entry.contentType,
		ElapsedNs:   time.Since(started).Nanoseconds(),
		CacheHit:    true,
	}
	payload, err := encodeReply(rep)
	if err != nil {
		inst.log.Warn().Err(err).Msg("chlocalbroker: encode cache-hit reply")
		inst.sendError(replySubject, "encode cache hit: "+err.Error(), "", 0)
		return
	}
	if err = inst.busClient.Publish(replySubject, payload); err != nil {
		inst.log.Warn().Err(err).Str("reply", replySubject).Msg("chlocalbroker: publish cache-hit reply")
	}
}

func (inst *Service) sendError(replySubject, errMsg, stderrTail string, exitCode int32) {
	rep := wireReply{
		OK:       false,
		Error:    errMsg,
		Stderr:   stderrTail,
		ExitCode: exitCode,
	}
	payload, err := encodeReply(rep)
	if err != nil {
		inst.log.Warn().Err(err).Msg("chlocalbroker: encode error reply")
		return
	}
	if err = inst.busClient.Publish(replySubject, payload); err != nil {
		inst.log.Warn().Err(err).Str("reply", replySubject).Msg("chlocalbroker: publish error reply")
	}
}

