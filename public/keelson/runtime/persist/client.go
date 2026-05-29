//go:build llm_generated_opus47

package persist

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Client adapts an app.BusI into the app.StorageI surface by translating
// Get/Set/Delete calls into `runtime.persist.{alias}.{key}.{op}` requests
// on the bus. One Client per app per process; the alias is baked in at
// construction so the request subject matches the app's declared
// `runtime.persist.{ownAlias}.>` cap.
//
// The host (windowhost) mints one per-app Client at Open and threads it
// through MountCtx.Storage(). Apps that have no
// `runtime.persist.{ownAlias}.>` cap will see every operation error with
// a permission-denied message — which is the intended lockdown until the
// manifest declares the cap.
type Client struct {
	bus   app.BusI
	alias string
}

var _ app.StorageI = (*Client)(nil)

// NewClient binds the bus client (whose AppId matches appId) to the
// runtime.persist.{alias} subject family. A nil bus is rejected at
// construction so the host fails fast rather than handing the app a
// Storage that errors on every call.
func NewClient(bus app.BusI, appId app.AppIdT) (inst *Client, err error) {
	if bus == nil {
		err = eh.Errorf("persist: NewClient: nil bus")
		return
	}
	inst = &Client{
		bus:   bus,
		alias: appId.SubjectAlias(),
	}
	return
}

// Get issues runtime.persist.{alias}.{key}.get and parses the reply. The
// found flag distinguishes "key absent" from "value was an empty slice".
// Errors propagate from the bus (timeout, permission) and from the
// service (backend failure).
//
// Key constraints: `key` must be a single NATS subject token — no dots,
// no wildcards. A dotted key produces a "malformed subject" error from
// the service. Use camelCase or snake_case names like "editorFont" or
// "selected_app" instead.
func (inst *Client) Get(key string) (value []byte, found bool, err error) {
	subject := SubjectFor(inst.alias, key, OpGet)
	raw, rerr := inst.bus.Request(subject, nil)
	if rerr != nil {
		err = eh.Errorf("persist: get %s: %w", subject, rerr)
		return
	}
	r, perr := UnmarshalReply(raw)
	if perr != nil {
		err = eh.Errorf("persist: get %s: %w", subject, perr)
		return
	}
	if r.Error != "" {
		err = eh.Errorf("persist: get %s: %s", subject, r.Error)
		return
	}
	value = r.Value
	found = r.Found
	return
}

// Set issues runtime.persist.{alias}.{key}.set with value as the payload.
// Empty values are valid and round-trip; the service treats them as
// distinct from "key absent" (Get returns found=true, value=[]byte{}).
func (inst *Client) Set(key string, value []byte) (err error) {
	subject := SubjectFor(inst.alias, key, OpSet)
	raw, rerr := inst.bus.Request(subject, value)
	if rerr != nil {
		err = eh.Errorf("persist: set %s: %w", subject, rerr)
		return
	}
	r, perr := UnmarshalReply(raw)
	if perr != nil {
		err = eh.Errorf("persist: set %s: %w", subject, perr)
		return
	}
	if r.Error != "" {
		err = eh.Errorf("persist: set %s: %s", subject, r.Error)
		return
	}
	return
}

// Delete issues runtime.persist.{alias}.{key}.delete. The subsequent Get
// for the same key returns found=false. Deleting a never-set key is not
// an error in the in-memory backend; CH-backed backends are at liberty
// to surface a "not found" error if they choose.
func (inst *Client) Delete(key string) (err error) {
	subject := SubjectFor(inst.alias, key, OpDelete)
	raw, rerr := inst.bus.Request(subject, nil)
	if rerr != nil {
		err = eh.Errorf("persist: delete %s: %w", subject, rerr)
		return
	}
	r, perr := UnmarshalReply(raw)
	if perr != nil {
		err = eh.Errorf("persist: delete %s: %w", subject, perr)
		return
	}
	if r.Error != "" {
		err = eh.Errorf("persist: delete %s: %s", subject, r.Error)
		return
	}
	return
}
