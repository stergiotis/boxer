// Package ladingadhoc publishes an in-memory file tree as a short-lived
// lading mount (ADR-0222 §SD5).
//
// It is the tree-shaped counterpart of ad-hoc datasets (ADR-0134). An app
// that has computed something tree-shaped — a rendered bundle, a generated
// corpus, a set of files it fetched — publishes it here and opens tally on
// it, and everything the browser does then works on it: preview, attributes,
// history, diff, du, the SQL surface and the SFTP head. The transposition is
// exact except for the last row, and that row is the store's nature rather
// than a gap:
//
//	handle    → mount id
//	revision  → snapshot instant
//	alias     → policy name
//	retract   → retention class
//
// Republishing passes the held mount id back, which writes another snapshot
// of the same mount instead of leaving a second one behind — the handle-reuse
// rule of ADR-0134 §SD2. There is no retract: a lading snapshot is written
// once and expires (ADR-0198), and a delete verb would be a second lifetime
// model for one set of tables.
//
// Publishing is a publish, not a hand-off. The rows are in the operator's
// ClickHouse for the retention class whether or not anyone opens a window on
// them, and the publisher writes with its own credentials — the mdedit upload
// posture (ADR-0217 §SD5), which is why this needs no capability subject yet.
package ladingadhoc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"io/fs"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// MountTagValue is the tag every ad-hoc mount carries. ADR-0198 §SD3 leaves
// the tag to the application — the store claims none and never inspects a
// body — so this package claims one for the class, which is what makes an
// ad-hoc mount recognisable without a lookup and what [Visibility] scopes a
// reader to. There is no central registry of mount tag values to record it
// in; the constant is the declaration.
const MountTagValue identifier.TagValue = 2178322

// DefaultTtl is what a published tree is kept for when the caller names no
// class. One day is the shortest the store can express — the tables drop
// whole expiry-day partitions (ADR-0198 §SD4), so retention is in whole days
// and the effective life of a tree is up to two of them.
const DefaultTtl ladingingest.TtlClassE = 1

// StoreNamePrefix begins the policy record's store field for a published
// tree, so the Mounts pane of a browser can say where a mount came from and
// a reader can tell an ad-hoc mount from a recorded one without decoding the
// id.
const StoreNamePrefix = "adhoc:"

// PublishInput is one publish. FS and Publisher are required; everything
// else has a default that is safe for a scratch tree.
type PublishInput struct {
	// FS is the tree to walk. Any io/fs.FS: an fstest.MapFS, a zip, a
	// sub-tree of a granted directory.
	FS fs.FS
	// Name is what the tree is called in a browser's mount list — the
	// alias, in ad-hoc dataset terms.
	Name string
	// Publisher identifies who published it, recorded after StoreNamePrefix
	// on the policy. An app id is the intended value.
	Publisher string
	// Mount republishes into a mount this caller already holds. Zero mints a
	// new one; a mount that is not an ad-hoc id is refused, because
	// republishing into a recorded mount would put scratch rows in it.
	Mount identifier.TaggedId
	// Ttl overrides DefaultTtl. A longer class keeps a tree that is expensive
	// to rebuild; there is no shorter one.
	Ttl ladingingest.TtlClassE
	// Policy overrides the rest of the walk's policy — the inline ceiling,
	// the text rule, the profile, metadata-only. Ttl above still wins over
	// Policy.Ttl, so a caller can set one without restating the other.
	Policy *ladingingest.Policy
}

// PublishResult names the published tree. Mount and Snap together are the
// location a launch config points a browser at.
type PublishResult struct {
	Mount     identifier.TaggedId
	Snap      time.Time
	ExpiresAt time.Time
	Entries   uint64
	Bytes     uint64
	// Errors counts nodes whose walk failed. They are rows carrying the
	// error rather than a failed publish (ADR-0198 §SD6), so a non-zero
	// count is worth reporting and is not a reason to discard the tree.
	Errors uint64
}

// Publish walks in.FS into the store as one snapshot and returns where it
// landed. It verifies the store's shape and never provisions it: a host that
// has not run the lading DDL should hear that rather than have tables appear
// on the sly (the ADR-0184 §SD2 posture).
//
// Off the render thread — it walks a tree and writes rows.
func Publish(ctx context.Context, exec recordstore.ExecutorI, in PublishInput) (res PublishResult, err error) {
	if in.FS == nil {
		err = eh.Errorf("ladingadhoc: no tree to publish")
		return
	}
	if in.Publisher == "" {
		err = eh.Errorf("ladingadhoc: publish needs a publisher")
		return
	}
	mount := in.Mount
	if mount == 0 {
		if mount, err = MintMount(); err != nil {
			return
		}
	} else if !IsAdhoc(mount) {
		err = eb.Build().Uint64("mount", mount.Value()).
			Errorf("ladingadhoc: republish target is not an ad-hoc mount")
		return
	}
	if exec == nil {
		err = eh.Errorf("ladingadhoc: no executor")
		return
	}
	policy := ladingingest.DefaultPolicy()
	if in.Policy != nil {
		policy = *in.Policy
	}
	policy.Ttl = DefaultTtl
	if in.Ttl != 0 {
		policy.Ttl = in.Ttl
	}
	if err = lading.Verify(ctx, exec); err != nil {
		err = eh.Errorf("ladingadhoc: lading store: %w", err)
		return
	}

	name := in.Name
	if name == "" {
		name = "ad-hoc tree"
	}
	policies := ladingpolicy.NewPolicyStore(exec, nil, ladingpolicy.PolicyStoreConfig{})
	defer policies.Close()
	// Declared once per publish. The policy log is append-only, so a
	// republish under a changed name or class is a new declaration and the
	// previous one stays readable — which is the behaviour a scratch mount
	// wants: the mount list shows what it is called now.
	if err = ladingingest.RecordPolicy(ctx, policies, mount, policy, name, StoreNamePrefix+in.Publisher); err != nil {
		err = eh.Errorf("ladingadhoc: record policy: %w", err)
		return
	}

	meta := ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{})
	defer meta.Close()
	stores := lading.Stores{Meta: meta}
	if !policy.MetaOnly {
		data := ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{})
		defer data.Close()
		stores.Data = data
	}
	out, err := ladingingest.Snapshot(ctx, in.FS, mount, policy, stores)
	if err != nil {
		err = eh.Errorf("ladingadhoc: snapshot: %w", err)
		return
	}
	res = PublishResult{
		Mount:     mount,
		Snap:      out.Snap,
		ExpiresAt: out.ExpiresAt,
		Entries:   out.Entries,
		Bytes:     out.Bytes,
		Errors:    out.Errors,
	}
	return
}

// MintMount returns a fresh ad-hoc mount id: the class tag over a random
// body. Random rather than derived from the tree, because two publishes of
// identical content are two trees with two lifetimes — content identity is
// what the entries' hashes already record.
func MintMount() (mount identifier.TaggedId, err error) {
	tag := MountTagValue.GetTag()
	if !tag.IsValid() {
		err = eh.Errorf("ladingadhoc: the ad-hoc mount tag does not encode")
		return
	}
	maxBody := tag.GetMaxPossibleIdIncl()
	var buf [8]byte
	if _, err = rand.Read(buf[:]); err != nil {
		err = eh.Errorf("ladingadhoc: mint mount id: %w", err)
		return
	}
	body := binary.LittleEndian.Uint64(buf[:]) % (uint64(maxBody) + 1)
	if body == 0 {
		// Zero is the reserved invalid body; one is as good a substitute as
		// any and the space is large enough that this never matters.
		body = 1
	}
	mount = tag.ComposeId(identifier.UntaggedId(body))
	return
}

// IsAdhoc reports whether a mount id carries the ad-hoc class tag.
func IsAdhoc(mount identifier.TaggedId) (yes bool) {
	if !mount.IsValid() {
		return false
	}
	return mount.GetTag() == MountTagValue.GetTag()
}

// Visibility is the mount scope that sees ad-hoc mounts and nothing else. A
// reader built for published trees — a viewer an app opens on its own
// output — hands it to `ladingsql.Config` so a query it runs cannot reach a
// recorded mount.
//
// A tag names a space of ids rather than a list of them, so this scope cannot
// enumerate: a query under it must name its mount, and `fs('*')` is refused
// at expansion (ADR-0200 §SD6). That is the shape's cost, and it is the right
// one here — a published tree is opened on the location its publisher was
// handed, not searched for.
func Visibility() (v ladingsql.MountVisibilityI) {
	return ladingsql.VisibleUnderTag{MountTagValue.GetTag()}
}

// DefaultDatabase is the store name a policy record's `store` field would
// carry for a recorded mount. It is here so a caller writing a mount list
// can tell the two apart in one place.
const DefaultDatabase = ladingschema.DatabaseName
