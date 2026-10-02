package jackstay

import (
	"context"
	"io"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SourceI is the source side of every step (ADR-0271 §SD3): a ClickHouse
// server ([ServerSource]) or a pack written by [Export] ([OpenPack]). The
// interface is sealed; a step reads the source only through it, so a pack
// stands in for a server wherever one is read.
type SourceI interface {
	discover(ctx context.Context) (inv Inventory, err error)
	// digests is the one leaf-digest scan of a spec (§SD4 of ADR-0259).
	digests(ctx context.Context, spec *DigestSpec) (chunks map[string]*chunkDigests, err error)
	pairs(ctx context.Context, spec *DigestSpec, leaves []ChunkLeaf) (pairs map[pairKey]*pairSide, err error)
	chunkList(ctx context.Context, spec *DigestSpec) (chunks []chunkListRow, err error)
	// stream returns the spec's rows as Native, in the content encoding it
	// names; compression is the encoding asked for.
	stream(ctx context.Context, spec *DigestSpec, compression string) (body io.ReadCloser, encoding string, err error)
	deriveChunking(ctx context.Context, pt *PlanTable, opts ChunkingOptions) (c Chunking, err error)
	// precheck says whether the spec's rows can be streamed, before a sync
	// clears anything on the target to make room for them.
	precheck(ctx context.Context, spec *DigestSpec) (err error)
	limits() (l sourceLimits)
}

// sourceLimits are what a source cannot serve.
type sourceLimits struct {
	// noPairs: no per-row (key hash, row hash) pairs, so differing leaves
	// stay unresolved.
	noPairs bool
	// noFinal: no FINAL reads.
	noFinal bool
	// noSubsets: no subset of a chunk (leaves, a sample), so no repair and
	// no sample sync.
	noSubsets bool
	// what names the source in a refusal.
	what string
}

// streamerI is the part of [ClientI] a server source streams rows through.
type streamerI interface {
	QueryStream(ctx context.Context, sql string, opts chclient.StreamOptions) (body io.ReadCloser, contentEncoding string, err error)
}

type serverSource struct {
	q QueryI
}

// ServerSource is a ClickHouse server as a source. q answers the metadata and
// digest queries; streaming rows needs a q that is also a [ClientI], as
// [chclient.Client] is.
func ServerSource(q QueryI) (src SourceI) {
	return serverSource{q: q}
}

func (inst serverSource) discover(ctx context.Context) (inv Inventory, err error) {
	return Discover(ctx, inst.q)
}

func (inst serverSource) digests(ctx context.Context, spec *DigestSpec) (chunks map[string]*chunkDigests, err error) {
	return readDigests(ctx, inst.q, spec)
}

func (inst serverSource) pairs(ctx context.Context, spec *DigestSpec, leaves []ChunkLeaf) (pairs map[pairKey]*pairSide, err error) {
	return readPairs(ctx, inst.q, spec, leaves)
}

func (inst serverSource) chunkList(ctx context.Context, spec *DigestSpec) (chunks []chunkListRow, err error) {
	return queryRows[chunkListRow](ctx, inst.q, spec.ChunkListQuery())
}

func (inst serverSource) stream(ctx context.Context, spec *DigestSpec, compression string) (body io.ReadCloser, encoding string, err error) {
	s, ok := inst.q.(streamerI)
	if !ok {
		err = eh.Errorf("the source client cannot stream rows")
		return
	}
	return s.QueryStream(ctx, spec.SelectNative(), chclient.StreamOptions{AcceptEncoding: compression})
}

func (inst serverSource) deriveChunking(ctx context.Context, pt *PlanTable, opts ChunkingOptions) (c Chunking, err error) {
	return DeriveChunking(ctx, inst.q, pt.Source, pt.SortingKey, pt.PartitionKey, pt.Rows, opts)
}

func (inst serverSource) precheck(ctx context.Context, spec *DigestSpec) (err error) {
	return
}

func (inst serverSource) limits() (l sourceLimits) {
	return sourceLimits{what: "the source server"}
}

// sourceLeafSet sums a spec's source digests over its chunks.
func sourceLeafSet(ctx context.Context, src SourceI, spec *DigestSpec) (ls leafSet, err error) {
	var chunks map[string]*chunkDigests
	chunks, err = src.digests(ctx, spec)
	if err != nil {
		return
	}
	ls = leafSetOf(chunks)
	return
}
