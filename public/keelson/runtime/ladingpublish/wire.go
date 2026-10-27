// Package ladingpublish is the host's write path into the lading store
// (ADR-0338, proposed): an app sends a tree on a bus request under its own
// mount tag, and the service checks the tree's label against what the store
// may hold before it writes a snapshot. Reading the store is not this
// package's: readers keep their own connection.
//
// The tag is in the subject. An app declares the capability
// `lading.publish.<tag>` on its manifest, so the bus's own subject check
// keeps it to its tag, and the service refuses a tag two apps declare or a
// sender whose manifest does not name it.
package ladingpublish

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SubjectPrefix opens every publish subject; the tag value follows it.
const SubjectPrefix = "lading.publish."

// subjectWildcard is what the service subscribes to.
const subjectWildcard = SubjectPrefix + "*"

// ServiceAppId is the service's own bus identity.
const ServiceAppId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"

// publishTimeout bounds a client's wait: a publish connects, verifies and
// writes a snapshot, which is not a five-second operation. The service gives
// up at writeDeadline from receipt, a margin earlier, so a publish the
// client stopped waiting for has not been written.
const (
	publishTimeout = 2 * time.Minute
	writeDeadline  = publishTimeout - 15*time.Second
)

// checkTimeout bounds Check: the service answers a dry run at once, so a
// host without the service is known in seconds. A variable for the tests.
var checkTimeout = 10 * time.Second

// Subject is the publish subject for a mount tag, the capability an app
// declares on its manifest.
func Subject(tag identifier.TagValue) string {
	return SubjectPrefix + strconv.FormatUint(uint64(tag), 10)
}

// tagOfSubject is the tag a publish subject names.
func tagOfSubject(subject string) (tag identifier.TagValue, ok bool) {
	rest, found := strings.CutPrefix(subject, SubjectPrefix)
	if !found {
		return
	}
	v, err := strconv.ParseUint(rest, 10, 32)
	if err != nil || v == 0 {
		return
	}
	tag = identifier.TagValue(v)
	return tag, tag.IsValid()
}

// File is one file of a published tree, by its slash-separated path from the
// tree's root.
type File struct {
	Path string
	Data []byte
}

// Request is one publish.
type Request struct {
	// Mount is a mount under the subject's tag to write another snapshot
	// of; zero mints one.
	Mount uint64
	// Name is what the mount is called in a browser's mount list.
	Name string
	// TtlDays is the retention class in whole days; zero is DefaultTtlDays.
	TtlDays uint16
	// Tier and Confined are the tree's label as the caller states it
	// (ADR-0311 §SD1): the TLP tier, and whether it holds sealed data.
	Tier     uint8
	Confined bool
	Files    []File
	// DryRun asks for the check alone: nothing is written and no trail row
	// is left. Check sets it.
	DryRun bool
}

// Result is what the service did with a request. A refused publish wrote
// nothing; a failed one may have left the mount's policy row and rows no
// snapshot commits, which readers do not see.
type Result struct {
	Mount     uint64
	SnapNanos int64
	// ExpiresNanos is when the snapshot's partition is dropped.
	ExpiresNanos int64
	Files        uint64
	Bytes        uint64
	Refused      bool
	Failed       bool
	Reason       string
}

// Snap is the snapshot instant.
func (inst Result) Snap() time.Time { return time.Unix(0, inst.SnapNanos) }

// Expires is when the snapshot expires.
func (inst Result) Expires() time.Time { return time.Unix(0, inst.ExpiresNanos) }

// RefusedError is a publish the service refused or failed.
type RefusedError struct{ Result Result }

func (inst *RefusedError) Error() string { return "lading publish: " + inst.Result.Reason }

// Check asks the service whether it would write req, without writing it: a
// refusal is a RefusedError, and a host without the service an error within
// checkTimeout. A caller with something to do before the publish that should
// not happen for a publish that cannot (an endorsement) checks first.
func Check(bus app.BusI, tag identifier.TagValue, req Request) (err error) {
	req.DryRun = true
	_, err = request(bus, tag, req, checkTimeout)
	return
}

// Publish sends req under tag and waits for the service's answer. The
// caller's bus client holds the capability Subject(tag). It blocks for as
// long as the write takes, so call it off the frame goroutine.
func Publish(bus app.BusI, tag identifier.TagValue, req Request) (res Result, err error) {
	req.DryRun = false
	return request(bus, tag, req, publishTimeout)
}

func request(bus app.BusI, tag identifier.TagValue, req Request, d time.Duration) (res Result, err error) {
	b, err := buscodec.Encode(req)
	if err != nil {
		err = eh.Errorf("lading publish: encode: %w", err)
		return
	}
	reply, err := bus.RequestWithTimeout(Subject(tag), b, d)
	if errors.Is(err, inprocbus.ErrTimeout) {
		err = eb.Build().Uint64("tag", uint64(tag)).Errorf("lading publish: no answer from the publish service; does this host run it? %w", err)
		return
	}
	if err != nil {
		err = eb.Build().Uint64("tag", uint64(tag)).Errorf("lading publish: %w", err)
		return
	}
	if res, err = buscodec.Decode[Result](reply); err != nil {
		err = eh.Errorf("lading publish: decode the reply: %w", err)
		return
	}
	if res.Refused || res.Failed {
		err = &RefusedError{Result: res}
	}
	return
}
