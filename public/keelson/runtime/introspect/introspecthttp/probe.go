package introspecthttp

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// probe.go — the E6 reachability probe primitive of
// doc/explanation/query-system-requirements.md.
//
// The problem it answers is R3: an endpoint string does not establish
// machine-locality. `127.0.0.1:8123` looks local and can be an SSH tunnel to
// a server on another continent — and that server's `url()` fetches would
// then dereference *its* loopback, not this one. Anything derived from the
// address alone gets that wrong in the direction that matters, because the
// data on this loopback plane exists only in this process.
//
// So locality is proven by demonstration rather than inferred: mint a
// single-use nonce URL, hand it to the engine as part of a query, and ask
// afterwards whether it was actually fetched. A tunnelled engine cannot
// fetch it, so the proof simply fails — the safe direction.
//
// What a successful check establishes is exactly this and nothing more:
// **that engine could reach this process's loopback plane at that moment.**
// It is not a claim about the future, about other engines, or about any
// other address the engine might use. Proof caching, re-probe cadence, and
// what to do with a proof are the caller's policy; boxer ships the
// primitive and deliberately does not consume it.

const (
	// probeTTL bounds how long a minted nonce stays answerable. The probe
	// flow is mint → embed in a statement → engine fetches → check, which is
	// a round trip of seconds; anything longer is a stale proof pretending
	// to be a fresh one.
	probeTTL = 30 * time.Second
	// probeNonceBytes is the entropy behind a nonce. It is the whole secret:
	// anything that can guess it can forge a reachability proof.
	probeNonceBytes = 32
)

// probeEntry is one outstanding nonce.
type probeEntry struct {
	nonce   string
	fetched bool
	expires time.Time
}

// probeStore holds the outstanding nonces. Small by construction — entries
// live for probeTTL and are consumed by a check — which is what makes the
// linear constant-time scan in lookup affordable.
type probeStore struct {
	mu      sync.Mutex
	entries []probeEntry
}

// MintProbe issues a single-use reachability nonce and the URL that
// demonstrates it. Hand the URL to the engine under test (typically inside
// a statement it will evaluate), then ask CheckProbe.
//
// The returned URL is on this server's loopback base, so an engine that
// cannot reach this process cannot fetch it.
func (s *Server) MintProbe() (nonce string, url string, err error) {
	raw := make([]byte, probeNonceBytes)
	_, err = rand.Read(raw)
	if err != nil {
		err = eh.Errorf("introspecthttp: mint probe nonce: %w", err)
		return
	}
	nonce = hex.EncodeToString(raw)
	s.probes.add(probeEntry{nonce: nonce, expires: time.Now().Add(probeTTL)})
	url = s.BaseURL() + "/probe/" + nonce
	return
}

// CheckProbe reports whether the nonce's URL was fetched, and consumes it.
//
// Single-use in both directions: a nonce can be fetched once and checked
// once. A second check of the same nonce is false, as is an unknown nonce,
// an expired one, and one that was never fetched. False therefore means
// only "not proven", never "proven not to be reachable" — a check that
// arrives after the TTL cannot distinguish the two.
func (s *Server) CheckProbe(nonce string) (fetched bool) {
	fetched = s.probes.consume(nonce, time.Now())
	return
}

// handleProbe answers a nonce fetch. A valid, unexpired, not-yet-fetched
// nonce marks itself fetched and answers 200; everything else is a 404 that
// says nothing about why, so a caller cannot use the response to tell an
// expired nonce from one that never existed.
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		// A HEAD is not a fetch: ClickHouse's url() engine sizes a resource
		// before reading it, and consuming the nonce there would 404 the GET
		// that follows and fail the probing statement. Answering without
		// marking keeps "fetchable once" about the content.
		if !s.probes.isOutstanding(r.PathValue("nonce"), time.Now()) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		return
	}
	if !s.probes.markFetched(r.PathValue("nonce"), time.Now()) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (inst *probeStore) add(e probeEntry) {
	inst.mu.Lock()
	inst.pruneLocked(time.Now())
	inst.entries = append(inst.entries, e)
	inst.mu.Unlock()
}

// isOutstanding reports whether nonce is known, unexpired and not yet
// fetched, WITHOUT consuming it. It exists for HEAD, and deliberately
// answers the same question markFetched would without changing anything.
func (inst *probeStore) isOutstanding(nonce string, now time.Time) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.pruneLocked(now)
	i := inst.indexOfLocked(nonce)
	ok = i >= 0 && !inst.entries[i].fetched
	return
}

// markFetched records that nonce's URL was fetched. Returns false when the
// nonce is unknown, expired, or already fetched — a nonce is fetchable once.
func (inst *probeStore) markFetched(nonce string, now time.Time) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.pruneLocked(now)
	i := inst.indexOfLocked(nonce)
	if i < 0 || inst.entries[i].fetched {
		return
	}
	inst.entries[i].fetched = true
	ok = true
	return
}

// consume reports whether nonce was fetched and removes it either way.
func (inst *probeStore) consume(nonce string, now time.Time) (fetched bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.pruneLocked(now)
	i := inst.indexOfLocked(nonce)
	if i < 0 {
		return
	}
	fetched = inst.entries[i].fetched
	inst.entries = append(inst.entries[:i], inst.entries[i+1:]...)
	return
}

// indexOfLocked finds a nonce without leaking it through timing: every
// entry is compared, with no early exit, using a constant-time comparison.
// The nonce is the entire secret behind a reachability proof, and an
// attacker who can measure how long a near-miss takes can walk it out one
// byte at a time.
func (inst *probeStore) indexOfLocked(nonce string) (idx int) {
	idx = -1
	for i := range inst.entries {
		if subtle.ConstantTimeCompare([]byte(inst.entries[i].nonce), []byte(nonce)) == 1 {
			idx = i
		}
	}
	return
}

// pruneLocked drops expired entries. Called on every operation, so an
// abandoned probe costs at most probeTTL of memory and nothing else.
func (inst *probeStore) pruneLocked(now time.Time) {
	kept := inst.entries[:0]
	for _, e := range inst.entries {
		if e.expires.After(now) {
			kept = append(kept, e)
		}
	}
	inst.entries = kept
}
