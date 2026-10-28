package trail

// LadingPublish is one publish into the lading store through the publish
// service (ADR-0341 §SD5, proposed): written or refused, and why. The
// calling app and window are the row's [Origin].
type LadingPublish struct {
	_ struct{} `kind:"ladingPublish"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindLadingPublish,symbol"`
	// Tag is the mount tag the request was made under; Mount the mount it
	// named or was minted, zero when refused before one was minted; Snapshot
	// the snapshot instant in Unix nanoseconds, zero unless written.
	Tag      uint64 `lw:"ladingPublishTag,u64Array,unit"`
	Mount    uint64 `lw:"ladingPublishMount,u64Array,unit"`
	Snapshot uint64 `lw:"ladingPublishSnapshot,u64Array,unit"`
	// Name is what the mount is called in a browser's mount list.
	Name    string `lw:"ladingPublishName,stringArray,unit"`
	TtlDays uint32 `lw:"ladingPublishTtlDays,u32Array,unit"`
	// Tier and Confined are the label the caller asserted (ADR-0311 §SD1).
	Tier     string `lw:"ladingPublishTier,symbol"`
	Confined bool   `lw:"ladingPublishConfined,bool"`
	// Decision is "written", "refused" or "failed"; Reason a refusal's or a
	// failure's, one element when there is one.
	Decision string   `lw:"ladingPublishDecision,symbol"`
	Reason   []string `lw:"ladingPublishReason,stringArray"`
	// Paths are the files in the request, sorted; Digests the SHA-256 of
	// each, in the same order.
	Paths   []string `lw:"ladingPublishPaths,stringArray"`
	Digests []string `lw:"ladingPublishDigests,stringArray"`
	Bytes   uint64   `lw:"ladingPublishBytes,u64Array,unit"`
}
