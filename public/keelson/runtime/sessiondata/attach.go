package sessiondata

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// attachment is one ad-hoc dataset read for a statement.
type attachment struct {
	alias    string
	external queryengine.ExternalTable
	handle   string
	revision uint64
	digest   string
	rows     uint64
	// ownClear is the dataset service's answer for the read: the revision
	// attached is the reading task's own publication at TLP:CLEAR.
	ownClear bool
}

// attachAll reads each alias for c, as the dataset service lets c read it,
// and makes it an external table; ext names the i-th. An ad-hoc dataset is
// sealed, so a statement that attaches one is confined (ADR-0145 §SD3):
// it goes only to an endpoint the caller declared may see sealed
// plaintext, and the attached bytes together stay under the attach bound.
// That holds whoever published the dataset. Whether the revision attached
// is the reading task's own publication at TLP:CLEAR is the dataset
// service's answer, taken with the bytes; it decides only the label of
// what is made of them.
func (inst *Service) attachAll(ctx context.Context, c Caller, e Endpoint, aliases []string) (out []attachment, err error) {
	if len(aliases) == 0 {
		return
	}
	if !e.ServesConfined {
		return nil, eb.Build().Str("endpoint", EndpointHost(e.URL)).Strs("datasets", aliases).
			Errorf("the statement would send an ad-hoc dataset, which is sealed data, to an endpoint not declared as allowed to see sealed data")
	}
	if inst.adhoc == nil {
		return nil, eh.Errorf("no ad-hoc dataset service to read keelson('…') from")
	}
	var total uint64
	for i, alias := range aliases {
		if adhocdata.IsHandle(alias) {
			return nil, eb.Build().Str("name", alias).Errorf("name the ad-hoc dataset by its alias, not its handle")
		}
		res, rErr := inst.adhoc.Read(ctx, alias, adhocdata.Identity{App: c.By.App, Instance: c.By.Instance}, c.OnBehalfOf)
		if rErr != nil {
			var ge *adhocdata.GrantError
			if errors.As(rErr, &ge) {
				return nil, &GrantError{Destination: ge.Destination, Reason: ge.Reason}
			}
			return nil, eb.Build().Str("alias", alias).Errorf("keelson('…') attaches only an ad-hoc dataset the caller may read: %w", rErr)
		}
		total += uint64(len(res.ArrowIPCStream))
		if total > inst.limits.MaxAttachBytes {
			return nil, eb.Build().Str("alias", alias).Uint64("bound", inst.limits.MaxAttachBytes).
				Errorf("the ad-hoc datasets the statement attaches are larger together than the attach bound")
		}
		structure, sErr := streamStructure(res.ArrowIPCStream)
		if sErr != nil {
			return nil, eb.Build().Str("alias", alias).Errorf("read the dataset's schema: %w", sErr)
		}
		out = append(out, attachment{
			alias:    alias,
			external: queryengine.ExternalTable{Name: externalName(i), Format: "ArrowStream", Structure: structure, Data: res.ArrowIPCStream},
			handle:   res.Handle, revision: res.Revision, digest: res.StreamDigest, rows: res.Rows,
			ownClear: res.OwnClear,
		})
	}
	return
}

// streamStructure is the ClickHouse structure of an Arrow IPC stream's
// schema, by the publish gate's mapping, so the server reads the columns
// as a query through keelson('…') would.
func streamStructure(stream []byte) (structure string, err error) {
	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	if err != nil {
		return "", eh.Errorf("decode the stream: %w", err)
	}
	defer rdr.Release()
	return adhocdata.StructureFor(rdr.Schema())
}

// ownClearTask is the task a request's sealed reads may be the own clear
// publications of: the task of the attested call, empty for a person's
// request, which owns none.
func ownClearTask(cc option.Option[app.CallContext]) (task string) {
	if cc.Has {
		task = cc.Val.Task
	}
	return
}

// ownClearRefs are the attachments the dataset service answered as the
// reading task's own publication at TLP:CLEAR, as the trail names them.
func ownClearRefs(atts []attachment) (refs []string) {
	for _, a := range atts {
		if a.ownClear {
			refs = append(refs, a.ref())
		}
	}
	return
}

// allOwnClear reports whether every attachment is the reading task's own
// publication at TLP:CLEAR.
func allOwnClear(atts []attachment) (yes bool) {
	for _, a := range atts {
		if !a.ownClear {
			return false
		}
	}
	return true
}

// ref names what was attached for the trail: alias, handle, revision and
// the digest of the stream sent.
func (inst attachment) ref() (s string) {
	return inst.alias + "@" + inst.handle + "#" + strconv.FormatUint(inst.revision, 10) + ":" + inst.digest
}
