package jackstay

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SyncModeE is what a sync copies of a table (ADR-0259 §SD5).
type SyncModeE uint8

const (
	// SyncModeFull copies every chunk.
	SyncModeFull SyncModeE = iota
	// SyncModeRepair makes the target's differing leaves equal to the
	// source's, and only those the plan's diff showed.
	SyncModeRepair
	// SyncModeSample copies SampleNum of every SampleDen keys.
	SyncModeSample
)

var AllSyncModes = []SyncModeE{SyncModeFull, SyncModeRepair, SyncModeSample}

func (inst SyncModeE) String() (s string) {
	switch inst {
	case SyncModeFull:
		return "full"
	case SyncModeRepair:
		return "repair"
	case SyncModeSample:
		return "sample"
	}
	return "invalid"
}

func (inst SyncModeE) MarshalText() (text []byte, err error) {
	s := inst.String()
	if s == "invalid" {
		err = eb.Build().Uint8("mode", uint8(inst)).Errorf("invalid sync mode")
		return
	}
	return []byte(s), nil
}

func (inst *SyncModeE) UnmarshalText(text []byte) (err error) {
	for _, m := range AllSyncModes {
		if m.String() == string(text) {
			*inst = m
			return
		}
	}
	return eb.Build().Str("mode", string(text)).Errorf("unknown sync mode")
}

// ExistingPolicyE is what a full or sample sync does with a target table that
// already holds rows when the run begins.
type ExistingPolicyE uint8

const (
	// ExistingPolicyRefuse stops before copying anything.
	ExistingPolicyRefuse ExistingPolicyE = iota
	// ExistingPolicyAppend inserts beside the existing rows. The run does not
	// own them, so a chunk that fails verification cannot be retried.
	ExistingPolicyAppend
	// ExistingPolicyReplace clears each target chunk before copying it.
	ExistingPolicyReplace
)

var AllExistingPolicies = []ExistingPolicyE{ExistingPolicyRefuse, ExistingPolicyAppend, ExistingPolicyReplace}

func (inst ExistingPolicyE) String() (s string) {
	switch inst {
	case ExistingPolicyRefuse:
		return "refuse"
	case ExistingPolicyAppend:
		return "append"
	case ExistingPolicyReplace:
		return "replace"
	}
	return "invalid"
}

func (inst ExistingPolicyE) MarshalText() (text []byte, err error) {
	s := inst.String()
	if s == "invalid" {
		err = eb.Build().Uint8("policy", uint8(inst)).Errorf("invalid existing-rows policy")
		return
	}
	return []byte(s), nil
}

func (inst *ExistingPolicyE) UnmarshalText(text []byte) (err error) {
	for _, p := range AllExistingPolicies {
		if p.String() == string(text) {
			*inst = p
			return
		}
	}
	return eb.Build().Str("policy", string(text)).Errorf("unknown existing-rows policy")
}

// ChunkStatusE is the outcome of one chunk.
type ChunkStatusE uint8

const (
	// ChunkStatusCopied: copied and verified in this call.
	ChunkStatusCopied ChunkStatusE = iota
	// ChunkStatusDone: verified by an earlier call of the same run, and the
	// source has not moved since.
	ChunkStatusDone
	// ChunkStatusIdentical: repair found nothing to do.
	ChunkStatusIdentical
	// ChunkStatusStale: repair found the target no longer as the plan's diff
	// showed it, and touched nothing.
	ChunkStatusStale
	// ChunkStatusFailed: the chunk could not be copied and verified.
	ChunkStatusFailed
)

var AllChunkStatuses = []ChunkStatusE{ChunkStatusCopied, ChunkStatusDone, ChunkStatusIdentical, ChunkStatusStale, ChunkStatusFailed}

func (inst ChunkStatusE) MarshalText() (text []byte, err error) {
	s := inst.String()
	if s == "invalid" {
		err = eb.Build().Uint8("status", uint8(inst)).Errorf("invalid chunk status")
		return
	}
	return []byte(s), nil
}

func (inst *ChunkStatusE) UnmarshalText(text []byte) (err error) {
	for _, st := range AllChunkStatuses {
		if st.String() == string(text) {
			*inst = st
			return
		}
	}
	return eb.Build().Str("status", string(text)).Errorf("unknown chunk status")
}

func (inst ChunkStatusE) String() (s string) {
	switch inst {
	case ChunkStatusCopied:
		return "copied"
	case ChunkStatusDone:
		return "done"
	case ChunkStatusIdentical:
		return "identical"
	case ChunkStatusStale:
		return "stale"
	case ChunkStatusFailed:
		return "failed"
	}
	return "invalid"
}

// ParseCompression reads an operator's compression choice ("zstd", "gzip",
// "none" or empty) as the HTTP content encoding a sync relays and an export
// stores: empty for none.
func ParseCompression(s string) (encoding string, err error) {
	switch s {
	case "zstd", "gzip":
		return s, nil
	case "none", "":
		return "", nil
	}
	err = eb.Build().Str("compression", s).Errorf("expected zstd, gzip or none")
	return
}

// ParseFraction reads a sample fraction "num/den", with 0 < num <= den;
// space around either number is allowed.
func ParseFraction(s string) (num uint32, den uint32, err error) {
	a, b, found := strings.Cut(strings.TrimSpace(s), "/")
	var n, d uint64
	if found {
		n, err = strconv.ParseUint(strings.TrimSpace(a), 10, 32)
		if err == nil {
			d, err = strconv.ParseUint(strings.TrimSpace(b), 10, 32)
		}
	}
	if !found || err != nil || n == 0 || d == 0 || n > d {
		err = eb.Build().Str("sample", s).Errorf("the sample must be a fraction num/den with 0 < num <= den")
		return
	}
	return uint32(n), uint32(d), nil
}
