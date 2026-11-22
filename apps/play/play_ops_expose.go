package play

// play_ops_expose.go: exposing an ad-hoc dataset to programs on this
// machine (ADR-0407). expose_dataset asks the dataset service to serve one
// on the host's loopback listener to whoever presents the token its answer
// carries; revoke_exposure ends one; list_datasets says which are exposed.
// The Data sets tab does the same for the person. A task needs
// expose:<prefix> in its grant and may expose only what it published; the
// service decides, and play refuses early with the destination to ask for.

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const (
	opExposeDataset  = "expose_dataset"
	opRevokeExposure = "revoke_exposure"

	opsResExposures = "exposures"
)

// exposeTimeout bounds an exposure request on the bus.
const exposeTimeout = 30 * time.Second

// exposeCallsMax bounds the exposure calls a window remembers.
const exposeCallsMax = 64

// ExposeDatasetArgs is expose_dataset's argument.
type ExposeDatasetArgs struct {
	Name    string `desc:"the ad-hoc dataset to expose: its alias (a bundle's dataset as <bundle>__<name>), or the name list_datasets lists it by in this window"`
	Minutes uint32 `json:",omitzero" desc:"how long the exposure lasts, in minutes: 60 when left out, at most 1440"`
}

// ExposeDatasetResult is expose_dataset's result: what a reader needs.
type ExposeDatasetResult struct {
	Exposure   string `desc:"the exposure's id; revoke_exposure takes it, list_datasets lists it"`
	Alias      string `desc:"the dataset's alias"`
	URL        string `desc:"where a program on this machine fetches the dataset's Arrow stream, presenting the token as the header 'Authorization: Bearer <token>'"`
	CSVURL     string `desc:"the same rows as CSV with a header line, with the same header"`
	Token      string `desc:"the bearer token; this answer is the only place it is given, so hand it on with the URL"`
	ClickHouse string `desc:"the table expression a ClickHouse server on this machine reads the dataset with, token included: SELECT … FROM <this> AS t; its user needs READ ON URL and its remote_url_allow_hosts must admit 127.0.0.1"`
	Curl       string `desc:"a command line that fetches the Arrow stream"`
	Structure  string `desc:"its columns as a ClickHouse structure"`
	Rows       uint64 `desc:"its rows"`
	Bytes      uint64 `desc:"the length of the Arrow stream a reader receives"`
	Revision   uint64 `desc:"the dataset's revision now; a republish under the alias with the same columns is served at its new revision"`
	Expires    string `desc:"when the exposure lapses, RFC 3339 UTC; sooner if the dataset is retracted, its columns change, this window closes or the host stops"`
}

// RevokeExposureArgs is revoke_exposure's argument.
type RevokeExposureArgs struct {
	Exposure string `desc:"the exposure's id, as expose_dataset answered it or list_datasets lists it"`
}

// RevokeExposureResult is revoke_exposure's result.
type RevokeExposureResult struct {
	Revoked string `desc:"the id of the exposure ended; its URL answers nothing from now on"`
}

// exposureView is the window's last reading of the live exposures, and the
// answers to the exposures this window made, which hold what a reader needs.
type exposureView struct {
	mu   sync.Mutex
	list []adhocdata.ExposureInfo
	at   time.Time
	busy bool
	err  string
	// made is every exposure this window made, by id: the answer, which is
	// the only place its token is given. The window shows it while the
	// exposure lives.
	made map[string]adhocdata.ExposeResult
}

func (inst *exposureView) read() (list []adhocdata.ExposureInfo, made map[string]adhocdata.ExposeResult, errText string, at time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.list, maps.Clone(inst.made), inst.err, inst.at
}

// remember keeps an exposure's answer.
func (inst *exposureView) remember(res adhocdata.ExposeResult) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.made == nil {
		inst.made = make(map[string]adhocdata.ExposeResult, 1)
	}
	inst.made[res.Id] = res
	inst.list = append(slices.DeleteFunc(slices.Clone(inst.list), func(e adhocdata.ExposureInfo) bool { return e.Id == res.Id }), res.ExposureInfo)
}

// forget drops an exposure that ended.
func (inst *exposureView) forget(id string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	delete(inst.made, id)
	inst.list = slices.DeleteFunc(slices.Clone(inst.list), func(e adhocdata.ExposureInfo) bool { return e.Id == id })
}

// digest is the live exposures as id@alias, compared across frames: the
// exposures resource.
func (inst *exposureView) digest() (d string) {
	list, _, _, _ := inst.read()
	parts := make([]string, 0, len(list))
	for _, e := range list {
		parts = append(parts, e.Id+"@"+e.Alias)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// refreshExposures lists the live exposures off the frame.
func (inst *PlayApp) refreshExposures() {
	c := inst.client
	if c == nil {
		return
	}
	bus := c.sessionBus()
	v := &inst.exposures
	v.mu.Lock()
	if bus == nil || v.busy {
		v.mu.Unlock()
		return
	}
	v.busy = true
	v.mu.Unlock()
	started := c.Go(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, schemaOpTimeout)
		list, err := adhocdata.ExposuresRequest(ctx, bus, nil)
		cancel()
		v.mu.Lock()
		defer v.mu.Unlock()
		v.busy, v.at = false, time.Now()
		if err != nil {
			v.err = err.Error()
			return
		}
		v.list, v.err = list, ""
		for id := range v.made {
			if !slices.ContainsFunc(list, func(e adhocdata.ExposureInfo) bool { return e.Id == id }) {
				delete(v.made, id)
			}
		}
	})
	if !started {
		v.mu.Lock()
		v.busy = false
		v.mu.Unlock()
	}
}

// exposeAlias is the alias name stands for in this window: the alias of a
// dataset bound under it, or name itself.
func (inst *Client) exposeAlias(name string) (alias string) {
	if slices.Contains(inst.DatasetAliases(), name) {
		if n := inst.datasetNameOf(name); n.alias != "" {
			return n.alias
		}
	}
	return name
}

// exposeGrant refuses an agent's exposure of alias whose call's grant lists
// no expose:<prefix> for a prefix of it, naming the alias's own
// destination to ask for. The service's check is the one that decides.
func exposeGrant(obo *app.OnBehalfOf, alias string) (err error) {
	if obo == nil {
		return
	}
	for i := len(alias); i > 0; i-- {
		if slices.Contains(obo.Destinations, adhocdata.DestinationExpose(alias[:i])) {
			return
		}
	}
	d := adhocdata.DestinationExpose(alias)
	return app.RefuseForDestinations("exposing "+alias+" to programs on this machine needs "+d+" in the task's grant, or expose: with a shorter prefix of it", d)
}

// exposeRefusal is a failed exposure call as an operation's refusal.
func exposeRefusal(err error) error {
	if ge, ok := errors.AsType[*adhocdata.GrantError](err); ok && ge.Destination != "" {
		return app.RefuseForDestinations(err.Error(), ge.Destination)
	}
	return app.RefuseOperation(err.Error())
}

// exposeResultOf is the operation's answer for a service answer.
func exposeResultOf(res adhocdata.ExposeResult) (out ExposeDatasetResult) {
	return ExposeDatasetResult{Exposure: res.Id, Alias: res.Alias, URL: res.URL, CSVURL: res.CSVURL, Token: res.Token,
		ClickHouse: res.ClickHouse, Curl: res.Curl, Structure: res.Structure, Rows: res.Rows, Bytes: res.Bytes, Revision: res.Revision,
		Expires: res.ExpiresAt.UTC().Format(time.RFC3339)}
}

// startExpose exposes alias off the frame for obo, nil for the person, and
// remembers the answer in the window.
func (inst *PlayApp) startExpose(obo *app.OnBehalfOf, alias string, lifetime time.Duration) (call *sessionCall[adhocdata.ExposeResult]) {
	c := inst.client
	bus := c.sessionBus()
	return startSessionCall(c, func(ctx context.Context) (res adhocdata.ExposeResult, err error) {
		ctx, cancel := context.WithTimeout(ctx, exposeTimeout)
		defer cancel()
		res, err = adhocdata.ExposeRequest(ctx, bus, adhocdata.ExposeInput{Alias: alias, Lifetime: lifetime, OnBehalfOf: obo})
		switch {
		case err == nil:
			inst.exposures.remember(res)
		case obo == nil:
			inst.setActionNote("expose " + alias + ": " + err.Error())
		}
		return
	})
}

// startRevoke revokes the exposure id off the frame for obo.
func (inst *PlayApp) startRevoke(obo *app.OnBehalfOf, id string) (call *sessionCall[string]) {
	c := inst.client
	bus := c.sessionBus()
	return startSessionCall(c, func(ctx context.Context) (out string, err error) {
		ctx, cancel := context.WithTimeout(ctx, exposeTimeout)
		defer cancel()
		err = adhocdata.RevokeExposureRequest(ctx, bus, id, obo)
		switch {
		case err == nil:
			inst.exposures.forget(id)
			out = id
		case obo == nil:
			inst.setActionNote("revoke " + id + ": " + err.Error())
		}
		return
	})
}

// exposeReady refuses an exposure call the window cannot make.
func (inst *PlayLauncher) exposeReady() (p *PlayApp, err error) {
	p = inst.inner
	switch {
	case p == nil:
		return nil, app.RefuseOperation("the window has not mounted")
	case p.client == nil || p.client.sessionBus() == nil:
		return nil, app.RefuseOperation("the window has no bus to the dataset service")
	}
	return
}

func addExposeOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	s.Resource(opsResExposures, "the live exposures of ad-hoc datasets, as id@alias, as the window last listed them", func(inst *PlayLauncher) any {
		if inst.inner == nil {
			return ""
		}
		return inst.inner.exposuresSeen
	})
	appops.Command(s, app.OperationSpec{Name: opExposeDataset, Version: 1,
		Summary: "expose an ad-hoc dataset to programs on this machine: the host serves it on its loopback port to whoever presents the token the answer carries",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResExposures}, AnyRevision: true, Agents: true, Async: true,
		Follows: []string{"the answer's clickhouse is a url() expression a ClickHouse server on this machine reads it with, and curl a command line that fetches it; hand them on in your reply, with the URL and the token, since nothing gives the token again",
			"a task needs expose:<alias> in its grant (ask request_access; expose:<prefix> covers every alias starting with it) and exposes only a dataset its own call published; the person exposes any from the Data sets tab",
			"it is readable only from this machine and only while the host runs; it lapses at its lifetime (60 minutes unless asked), when the dataset is retracted or its columns change, or when this window closes",
			"revoke_exposure ends it; list_datasets lists the live exposures"}},
		func(inst *PlayLauncher, call app.OperationCall, in ExposeDatasetArgs) (out ExposeDatasetResult, err error) {
			p, err := inst.exposeReady()
			if err != nil {
				return
			}
			switch {
			case !validDatasetIdentifier(in.Name):
				return out, app.RefuseOperation("name a dataset by its alias: letters, digits and _, at most 64 bytes")
			case adhocdata.IsHandle(in.Name):
				return out, app.RefuseOperation("that is a dataset handle; name the alias its publisher gave it")
			case in.Minutes > uint32(adhocdata.MaxExposureLifetime/time.Minute):
				return out, app.RefuseOperation("an exposure lasts at most " + strconv.Itoa(int(adhocdata.MaxExposureLifetime/time.Minute)) + " minutes")
			}
			alias := p.client.exposeAlias(in.Name)
			if err = exposeGrant(call.OnBehalfOf, alias); err != nil {
				return
			}
			if p.exposeCalls == nil || len(p.exposeCalls) >= exposeCallsMax {
				p.exposeCalls = make(map[string]*sessionCall[adhocdata.ExposeResult], 1)
			}
			p.exposeCalls[call.Key] = p.startExpose(call.OnBehalfOf, alias, time.Duration(in.Minutes)*time.Minute)
			return
		})
	appops.Settles(s, opExposeDataset, func(inst *PlayLauncher, call app.OperationCall) (out ExposeDatasetResult, settled bool, err error) {
		p := inst.inner
		if p == nil {
			return out, true, app.RefuseOperation("the window has not mounted")
		}
		ec := p.exposeCalls[call.Key]
		if ec == nil {
			return out, true, app.RefuseOperation("this window no longer knows the exposure the call started; list_datasets lists the live exposures")
		}
		res, xErr, done := ec.result()
		if !done {
			return out, false, nil
		}
		delete(p.exposeCalls, call.Key)
		if xErr != nil {
			return out, true, exposeRefusal(xErr)
		}
		return exposeResultOf(res), true, nil
	})
	appops.Command(s, app.OperationSpec{Name: opRevokeExposure, Version: 1,
		Summary: "end an exposure of an ad-hoc dataset: its URL answers nothing from then on",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResExposures}, AnyRevision: true, Agents: true, Async: true,
		Follows: []string{"a task revokes the exposures its calls made; the person revokes any",
			"expose_dataset exposes it again, with a new token"}},
		func(inst *PlayLauncher, call app.OperationCall, in RevokeExposureArgs) (out RevokeExposureResult, err error) {
			p, err := inst.exposeReady()
			if err != nil {
				return
			}
			if !strings.HasPrefix(in.Exposure, "exp_") {
				return out, app.RefuseOperation("name the exposure by its id, exp_…, as expose_dataset answered it or list_datasets lists it")
			}
			if p.revokeCalls == nil || len(p.revokeCalls) >= exposeCallsMax {
				p.revokeCalls = make(map[string]*sessionCall[string], 1)
			}
			p.revokeCalls[call.Key] = p.startRevoke(call.OnBehalfOf, in.Exposure)
			return
		})
	appops.Settles(s, opRevokeExposure, func(inst *PlayLauncher, call app.OperationCall) (out RevokeExposureResult, settled bool, err error) {
		p := inst.inner
		if p == nil {
			return out, true, app.RefuseOperation("the window has not mounted")
		}
		rc := p.revokeCalls[call.Key]
		if rc == nil {
			return out, true, app.RefuseOperation("this window no longer knows the revoke the call started; list_datasets lists the live exposures")
		}
		id, rErr, done := rc.result()
		if !done {
			return out, false, nil
		}
		delete(p.revokeCalls, call.Key)
		if rErr != nil {
			return out, true, exposeRefusal(rErr)
		}
		return RevokeExposureResult{Revoked: id}, true, nil
	})
}

// latchExposures moves the exposures resource to the window's last listing.
func (inst *PlayApp) latchExposures() {
	inst.exposuresSeen = inst.exposures.digest()
}

// DatasetExposure is one live exposure as list_datasets lists it.
type DatasetExposure struct {
	Exposure string `desc:"the exposure's id; revoke_exposure takes it"`
	Alias    string `desc:"the dataset's alias"`
	URL      string `desc:"where a reader on this machine fetches it, with the token expose_dataset answered"`
	Revision uint64 `desc:"the dataset's revision a read serves now"`
	Expires  string `desc:"when it lapses, RFC 3339 UTC"`
	Reads    uint64 `desc:"the reads that received data so far"`
	Yours    bool   `json:",omitzero" desc:"true when this task's call, or the person in this window, made it"`
}

// datasetExposures lists the live exposures for obo's list_datasets.
func datasetExposures(ctx context.Context, client *Client, obo *app.OnBehalfOf) (out []DatasetExposure, err error) {
	bus := client.sessionBus()
	if bus == nil {
		return
	}
	list, err := adhocdata.ExposuresRequest(ctx, bus, obo)
	if err != nil {
		return
	}
	for _, e := range list {
		out = append(out, DatasetExposure{Exposure: e.Id, Alias: e.Alias, URL: e.URL, Revision: e.Revision,
			Expires: e.ExpiresAt.UTC().Format(time.RFC3339), Reads: e.Reads, Yours: e.Yours})
	}
	return
}
