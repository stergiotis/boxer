package imagescope

// The regions' operations (ADR-0399 §M2, ADR-0269): what an agent reads of
// the window's regions and how it adds, changes, measures, profiles and
// hands them on. The person's tools, fields and buttons call the same
// handlers as gestures; the origin a region gets is the caller's, set
// here, never an argument.

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/science/imaging/regions"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

const (
	resRegions = "regions"

	opListRegions        = "list_regions"
	opGetRegion          = "get_region"
	opAddRegion          = "add_region"
	opUpdateRegion       = "update_region"
	opRemoveRegion       = "remove_region"
	opSelectRegion       = "select_region"
	opMeasureRegion      = "measure_region"
	opMeasureRegions     = "measure_regions"
	opProfileRegion      = "profile_region"
	opGetRegionsDocument = "get_regions_document"
	opSetRegionsDocument = "set_regions_document"
	opSendRegionsToPlay  = "send_regions_to_play"
	opSendProfileToPlay  = "send_profile_to_play"
)

// RegionIdArgs names a region.
type RegionIdArgs struct {
	Id string `json:",omitzero" desc:"the region's id, from list_regions; for select_region empty selects none"`
}

// UpdateRegionArgs replaces a region, whole.
type UpdateRegionArgs struct {
	Id     string     `desc:"the region's id"`
	Region RegionSpec `desc:"the region as it should be: read get_region, change what should change, and send its Spec back"`
}

// MeasureRegionArgs asks for a region's figures.
type MeasureRegionArgs struct {
	Id   string `desc:"the region's id"`
	Area bool   `json:",omitzero" desc:"add the figures by covered area: every pixel the shape touches weighted by the exact fraction of it inside the shape"`
}

// MeasureRegionsArgs asks for the figures of every region drawn on the
// image shown.
type MeasureRegionsArgs struct {
	Area bool `json:",omitzero" desc:"add the figures by covered area, as measure_region does"`
}

// RegionSummary is one region's figures in a line: per plane the counts,
// the range and the mean.
type RegionSummary struct {
	Region string         `desc:"the region's id"`
	Label  string         `desc:"the region's label"`
	Pixels int64          `desc:"pixels covered"`
	Planes []PlaneSummary `json:",omitzero" desc:"one entry per plane"`
	Error  string         `json:",omitzero" desc:"why the region could not be measured"`
}

// PlaneSummary is one plane's figures over a region, in short.
type PlaneSummary struct {
	Plane   string  `desc:"the plane's name"`
	Valid   uint64  `desc:"valid samples covered"`
	Invalid uint64  `desc:"invalid samples covered"`
	Min     float64 `desc:"smallest value"`
	Max     float64 `desc:"largest value"`
	Mean    float64 `desc:"arithmetic mean"`
}

// RegionsMeasured is what measure_regions settles with.
type RegionsMeasured struct {
	Image     string          `desc:"the id of the image measured"`
	Selection *SelectionInfo  `json:",omitzero" desc:"for a stack's selection: the selection measured"`
	Measured  int             `desc:"regions measured"`
	Failed    int             `json:",omitzero" desc:"regions that could not be measured, each with its Error"`
	NotDrawn  int             `json:",omitzero" desc:"regions left out because they are not drawn on the image shown: another frame, another stack position"`
	Regions   []RegionSummary `desc:"the regions measured, in the order they were made, at most 32; measure_region gives one region's figures whole, the percentiles and the mapping included"`
	Truncated bool            `json:",omitzero" desc:"more regions were measured than are listed"`
	Method    string          `desc:"what the figures are, in words"`
}

// ListRegionsArgs names a page of the regions.
type ListRegionsArgs struct {
	From  int `json:",omitzero" desc:"the first region to list, in the order they were made"`
	Count int `json:",omitzero" desc:"how many to list, at most 64; 0 lists 64"`
}

// ProfileArgs names a page of a line's profile.
type ProfileArgs struct {
	Id    string `desc:"the line region's id"`
	From  int    `json:",omitzero" desc:"the first pixel to list"`
	Count int    `json:",omitzero" desc:"how many pixels to list, at most 1024; 0 lists 1024"`
}

// AddRegionOutcome is what add_region returns.
type AddRegionOutcome struct {
	Id string `desc:"the new region's id; it is also selected"`
}

// RegionsDocument is the regions as the versioned regions document.
type RegionsDocument struct {
	Document string `desc:"the regions document (JSON, version 1): {\"version\": 1, \"regions\": [{\"id\", \"label\", \"note\", \"origin\", \"frame\": {\"kind\", \"code\", \"units\"}, \"anchor\": [{\"axis\", \"position\"}], \"shape\": {\"kind\", …}}]}, at most 1 MiB"`
}

// RegionInfo is a region as the catalog reports it.
type RegionInfo struct {
	Id       string           `desc:"the region's id"`
	Label    string           `desc:"its name for a person"`
	Note     string           `json:",omitzero" desc:"free text about it"`
	Origin   string           `desc:"who made it: person, task:<id> or agent for an agent's call, document for a document that named none; set by the window, never by the caller"`
	Spec     RegionSpec       `desc:"the region as update_region takes it back: shape, frame and anchor"`
	Selected bool             `json:",omitzero" desc:"the region is the one selected"`
	Applies  bool             `desc:"the region belongs to the image shown: every position, or its anchor names the stack position shown"`
	Drawn    bool             `desc:"it applies and reached the image's pixels: it is drawn and can be measured"`
	Reason   string           `json:",omitzero" desc:"why it does not apply or is not drawn"`
	Mapping  *MappingInfo     `json:",omitzero" desc:"how it reached the image's pixels, when drawn"`
	Figures  string           `desc:"none (not measured), current, or stale (the region, the image shown or its selection changed since)"`
	Revision uint64           `desc:"moves whenever the region changes"`
	Anchor   []AnchorPosition `json:",omitzero" desc:"the positions it belongs to; empty is every position"`
}

// RegionList is list_regions's result.
type RegionList struct {
	Regions   []RegionInfo `desc:"the regions from From"`
	Total     int          `desc:"regions in the window, at most 256"`
	From      int          `desc:"the first region listed"`
	Truncated bool         `json:",omitzero" desc:"regions follow the ones listed; ask again from From plus the regions listed"`
	Selected  string       `json:",omitzero" desc:"the id of the region selected"`
	Tool      string       `desc:"the tool in the person's hand: select, point, line, rectangle, ellipse or polygon"`
}

// regionInfoOf is a region over the image of a snapshot.
func regionInfoOf(sn opsSnap, e regionEntry) RegionInfo {
	r := e.r
	d := placeRegion(r, sn.pic, sn.stkShown)
	out := RegionInfo{Id: r.Id, Label: r.Label, Note: r.Note, Origin: r.Origin, Spec: specOf(r), Selected: r.Id == sn.selRegion,
		Applies: d.Applies, Drawn: d.Drawn, Reason: d.Reason, Figures: "none", Revision: e.rev, Anchor: anchorPositions(r.Anchor)}
	if d.Drawn {
		mi := mappingInfoOf(d.Mapping, d.Pixels)
		out.Mapping = &mi
	}
	if m := sn.regMeasures[r.Id]; m != nil {
		out.Figures = "current"
		if m.key != regionKey(e, sn.pic, m.area) {
			out.Figures = "stale"
		}
	}
	return out
}

// regionsValue is the regions resource's value: the document and the
// region selected.
type regionsValue struct {
	Document string
	Selected string
}

// regionOperations declares the regions' resource and operations.
func regionOperations(s *appops.Set[*App, opsSnap]) {
	s.Resource(resRegions, "the window's regions: shapes in a stated frame with an anchor, a label, a note and an origin, and the one selected", func(inst *App) any {
		doc, _ := regionsDocument(inst.regs)
		return regionsValue{Document: string(doc), Selected: inst.selRegion}
	})
	s.Restorable(resRegions, func(inst *App, v any) bool {
		rv, ok := v.(regionsValue)
		if !ok || inst.setRegionsDocument([]byte(rv.Document), originPerson) != nil {
			return false
		}
		return inst.selectRegion(rv.Selected) == nil
	})

	appops.Query(s, app.OperationSpec{Name: opListRegions, Version: 1,
		Summary: "list the window's regions, at most 64 at a time: their shapes, frames, anchors, origins, whether each is drawn on the image shown and why not, and whether its figures are current",
		Reads:   []string{resRegions, resImage}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListRegionsArgs) (out RegionList, err error) {
			count := in.Count
			if count <= 0 {
				count = regionsPageMax
			}
			count = min(count, regionsPageMax)
			from := min(max(in.From, 0), len(sn.regs))
			to := min(len(sn.regs), from+count)
			out = RegionList{Total: len(sn.regs), From: from, Truncated: to < len(sn.regs), Selected: sn.selRegion, Tool: sn.tool.String()}
			for _, e := range sn.regs[from:to] {
				out.Regions = append(out.Regions, regionInfoOf(sn, e))
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetRegion, Version: 1,
		Summary: "read one region: its shape, frame and anchor as update_region takes them back, its origin, whether it is drawn on the image shown and how it reached its pixels",
		Reads:   []string{resRegions, resImage}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in RegionIdArgs) (out RegionInfo, err error) {
			i := regionIndex(sn.regs, in.Id)
			if i < 0 {
				return out, errNoRegion(in.Id)
			}
			return regionInfoOf(sn, sn.regs[i]), nil
		})

	appops.Command(s, app.OperationSpec{Name: opAddRegion, Version: 1,
		Summary: "add a region — a point, a line, a rectangle, an ellipse or a polygon by its parameters, in the pixel frame or the image's own, for every stack position or the one shown — and select it; returns its id",
		Effect:  app.OperationEffectDocument, Writes: []string{resRegions}, Agents: true,
		Gesture: "a click or a drag on the image with a tool of the Tool row; Keep as region for the marked rectangle", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the origin is set by the window: person for the person's gesture, the caller's task otherwise",
			"a window holds at most 256 regions; a polygon made here at most 4096 vertices"}},
		func(inst *App, call app.OperationCall, in RegionSpec) (out AddRegionOutcome, err error) {
			out.Id, err = inst.addRegion(in, originOf(call))
			return
		})

	appops.Command(s, app.OperationSpec{Name: opUpdateRegion, Version: 1,
		Summary: "replace a region's label, note, shape, frame and anchor, whole, keeping its id: read get_region, change what should change, send its Spec back",
		Effect:  app.OperationEffectDocument, Writes: []string{resRegions}, Agents: true,
		Gesture: "the fields of the region selected and Apply; a drag of the selected region with the select tool; the Frame and Anchor buttons", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the origin becomes the caller's", "the numbers are taken in the frame the Spec names; nothing is converted"}},
		func(inst *App, call app.OperationCall, in UpdateRegionArgs) (out appops.None, err error) {
			err = inst.updateRegion(in.Id, in.Region, originOf(call))
			return
		})

	appops.Command(s, app.OperationSpec{Name: opRemoveRegion, Version: 1,
		Summary: "remove a region and its figures",
		Effect:  app.OperationEffectDocument, Writes: []string{resRegions}, Agents: true,
		Gesture: "Remove in the fields of the region selected, or Delete with the image focused", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, in RegionIdArgs) (out appops.None, err error) {
			err = inst.removeRegion(in.Id)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectRegion, Version: 1,
		Summary: "select a region, whose fields, figures and profile the window shows; the empty id selects none",
		Effect:  app.OperationEffectView, Writes: []string{resRegions}, Agents: true,
		Gesture: "a click on a region with the select tool, or on its row of the table", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, in RegionIdArgs) (out appops.None, err error) {
			err = inst.selectRegion(in.Id)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opMeasureRegion, Version: 1,
		Summary: "measure a region over the image shown from its samples at full resolution — per plane the valid and invalid counts, range, mean, standard deviation and percentiles, and by covered area when asked — with the method, the coverage rule of each figure and how the region reached the pixels; the call settles with the figures",
		Effect:  app.OperationEffectRun, Reads: []string{resRegions, resImage}, Agents: true, Async: true,
		Gesture: "Measure in the fields of the region selected", Fork: app.OperationForkPerInstance,
		Follows: []string{"a region not drawn on the image shown is refused with the reason: another frame, another stack position",
			"the figures are of the image shown, which they name; they are marked stale when the region, the image or its selection changes"}},
		func(inst *App, _ app.OperationCall, in MeasureRegionArgs) (out appops.None, err error) {
			err = inst.startRegionMeasure([]string{in.Id}, in.Area)
			return
		})
	appops.SettlesRaw(s, opMeasureRegion, func(inst *App, _ app.OperationCall, args []byte) (settled bool, result []byte, err error) {
		if inst.regionJob.Running() || inst.regionLanded != inst.regionCallGen {
			return false, nil, nil
		}
		in, derr := buscodec.Decode[MeasureRegionArgs](args)
		if derr != nil {
			return true, nil, derr
		}
		if inst.regionJobErr != "" {
			return true, nil, app.RefuseOperation(inst.regionJobErr)
		}
		i := regionIndex(inst.regs, in.Id)
		if i < 0 {
			return true, nil, app.RefuseOperation("the region was removed while it was measured")
		}
		m, stale := inst.measureState(inst.regs[i])
		switch {
		case m == nil:
			return true, nil, app.RefuseOperation("the region was not measured; measure_region again")
		case m.err != nil:
			return true, nil, app.RefuseOperation("the region could not be measured: " + refusalText(m.err))
		}
		result, err = buscodec.Encode(regionMeasurementOf(m, inst.regs[i].r.Label, stale))
		return true, result, err
	})
	appops.Command(s, app.OperationSpec{Name: opMeasureRegions, Version: 1,
		Summary: "measure every region drawn on the image shown, as measure_region does each, in one task; the call settles with a line per region, at most 32 of them",
		Effect:  app.OperationEffectRun, Reads: []string{resRegions, resImage}, Agents: true, Async: true,
		Gesture: "Measure all in the regions pane", Fork: app.OperationForkPerInstance,
		Follows: []string{"regions not drawn on the image shown are left out and counted in NotDrawn; with none drawn the call is refused",
			"the window holds at most 256 regions, so one call measures at most that many; the figures are computed at full resolution, so its time is linear in the pixels the regions cover",
			"each region's figures, whole, are then measure_region's without measuring again, until the region or the image changes"}},
		func(inst *App, _ app.OperationCall, in MeasureRegionsArgs) (out appops.None, err error) {
			err = inst.startRegionMeasure(nil, in.Area)
			return
		})
	appops.Settles(s, opMeasureRegions, func(inst *App, _ app.OperationCall) (out RegionsMeasured, settled bool, err error) {
		if inst.regionJob.Running() || inst.regionLanded != inst.regionCallGen {
			return out, false, nil
		}
		if inst.regionJobErr != "" {
			return out, true, app.RefuseOperation(inst.regionJobErr)
		}
		return inst.regionsMeasured(), true, nil
	})
	s.Available(opMeasureRegions, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.pic == nil:
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		case sn.regionMeasuring:
			return false, "a measurement of regions is under way"
		case len(sn.regs) == 0:
			return false, "there are no regions; add_region adds one"
		}
		return true, ""
	})

	s.Available(opMeasureRegion, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.pic == nil:
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		case sn.regionMeasuring:
			return false, "a measurement of regions is under way"
		}
		return true, ""
	})

	appops.Query(s, app.OperationSpec{Name: opProfileRegion, Version: 1,
		Summary: "the profile of a line region over the image shown: the pixels it passes through in order, each once, with the length of the line in each, the distance to its middle and every plane's value, at most 1024 at a time",
		Reads:   []string{resRegions, resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ProfileArgs) (out ProfileRows, err error) {
			if sn.pic == nil {
				return out, errNoImage
			}
			i := regionIndex(sn.regs, in.Id)
			if i < 0 {
				return out, errNoRegion(in.Id)
			}
			e := sn.regs[i]
			if e.r.Shape.Kind() != regions.ShapeKindLine {
				return out, app.RefuseOperation("region " + strconv.Quote(e.r.Label) + " is a " + e.r.Shape.Kind().String() + "; a profile is of a line")
			}
			d := placeRegion(e.r, sn.pic, sn.stkShown)
			if !d.Drawn {
				return out, app.RefuseOperation("region " + strconv.Quote(e.r.Label) + " is not profiled on the image shown: it is " + d.Reason)
			}
			count := in.Count
			if count <= 0 {
				count = profilePageMax
			}
			count = min(count, profilePageMax)
			key := regionKey(e, sn.pic, false)
			rp := sn.profile
			if rp == nil || rp.key != key {
				if n := profileBound(d.Pixels, sn.pic); n > maxHandoverPixels {
					return out, app.RefuseOperation(fmt.Sprintf("the line crosses up to %d pixels; at most %d are profiled here", n, maxHandoverPixels))
				}
				p, perr := regions.Profile(context.Background(), regions.StoreSource(sn.pic), e.r, regions.ProfileOptions{})
				if perr != nil {
					return out, refuse(perr)
				}
				rp = newRegionProfile(e.r.Id, key, p, sn.shownId, shownSelection(sn.stkShown))
			}
			return profileRowsOf(rp, in.From, count), nil
		})

	appops.Query(s, app.OperationSpec{Name: opGetRegionsDocument, Version: 1,
		Summary: "the window's regions as the versioned regions document (JSON), at most 1 MiB: what set_regions_document takes, and what Save regions… writes",
		Reads:   []string{resRegions}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out RegionsDocument, err error) {
			doc, err := regionsDocument(sn.regs)
			if err != nil {
				return out, refuse(err)
			}
			if len(doc) > maxRegionsDocumentBytes {
				return out, app.RefuseOperation(fmt.Sprintf("the document is %d bytes and at most %d are returned; remove regions, or the person saves it with Save regions…", len(doc), maxRegionsDocumentBytes))
			}
			return RegionsDocument{Document: string(doc)}, nil
		})

	appops.Command(s, app.OperationSpec{Name: opSetRegionsDocument, Version: 1,
		Summary: "replace the window's regions with those of a regions document (JSON, version 1, at most 1 MiB and 256 regions); figures and the profile are dropped",
		Effect:  app.OperationEffectDocument, Writes: []string{resRegions}, Agents: true,
		Gesture: "Load regions…", Rebuild: app.OperationRebuildRefold, Fork: app.OperationForkPerInstance,
		Follows: []string{"a document an agent sets takes the agent's origin for every region; one the person loads keeps the origins it records",
			"a document that does not read is refused naming the member and the region at fault, and the regions stay as they were"}},
		func(inst *App, call app.OperationCall, in RegionsDocument) (out appops.None, err error) {
			err = inst.setRegionsDocument([]byte(in.Document), originOf(call))
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSendRegionsToPlay, Version: 1,
		Summary: "measure every region drawn on the image shown and publish the figures as an ad-hoc dataset, one row per region and plane, then open the playground on it",
		Effect:  app.OperationEffectConsequential, Reads: []string{resRegions, resImage}, Agents: true,
		Gesture: "Send figures to play", Fork: app.OperationForkPerInstance,
		Follows: []string{"the request runs off the frame; get_view's RegionsHandover reports what opened or why it failed"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			err = inst.handoverRegions()
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSendProfileToPlay, Version: 1,
		Summary: "publish the profile of a line region over the image shown as an ad-hoc dataset, one row per pixel along the line, then open the playground on it",
		Effect:  app.OperationEffectConsequential, Reads: []string{resRegions, resImage}, Agents: true,
		Gesture: "Send profile to play", Fork: app.OperationForkPerInstance,
		Follows: []string{"the request runs off the frame; get_view's ProfileHandover reports what opened or why it failed",
			"a line crossing more than about four million pixels is refused"}},
		func(inst *App, _ app.OperationCall, in RegionIdArgs) (out appops.None, err error) {
			err = inst.handoverProfile(in.Id)
			return
		})
	for _, op := range []string{opSendRegionsToPlay, opSendProfileToPlay} {
		s.Available(op, func(sn opsSnap) (ok bool, reason string) {
			if sn.pic == nil {
				return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
			}
			return true, ""
		})
	}
}

// regionsMeasured is the last measurement of regions in short, as
// measure_regions settles with it.
func (inst *App) regionsMeasured() (out RegionsMeasured) {
	out.NotDrawn, out.Method = inst.regionNotDrawn, methodRegions
	for _, id := range inst.regionBatch {
		i := regionIndex(inst.regs, id)
		if i < 0 {
			continue
		}
		m, _ := inst.measureState(inst.regs[i])
		if m == nil {
			continue
		}
		out.Measured++
		out.Image, out.Selection = m.imageId, m.selection
		line := RegionSummary{Region: id, Label: inst.regs[i].r.Label}
		if m.err != nil {
			out.Failed++
			line.Error = refusalText(m.err)
		} else {
			line.Pixels = m.m.Pixels
			for _, p := range m.m.Planes {
				line.Planes = append(line.Planes, PlaneSummary{Plane: p.Label, Valid: p.Valid, Invalid: p.Invalid, Min: p.Min, Max: p.Max, Mean: p.Mean})
			}
		}
		if len(out.Regions) < regionsSummaryMax {
			out.Regions = append(out.Regions, line)
		} else {
			out.Truncated = true
		}
	}
	return out
}
