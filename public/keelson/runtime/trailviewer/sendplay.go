package trailviewer

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	playlaunch "github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Export to play (ADR-0350 §SD9, proposed): the table's record — the rows
// the window holds, in the columns it shows — published as an ad-hoc
// dataset (ADR-0134) under the window's own alias, and a play window opened
// on it. Play keeps sort and search, and the publish is itself a trail row.
// There is no file export.

// datasetRows is the dataset's base name, which the play window reads it
// under.
const datasetRows = "trail_rows"

// playSql is the statement the play window opens with.
const playSql = "SELECT *\nFROM keelson('" + datasetRows + "')"

// playSend is the export's publisher and its worker.
type playSend struct {
	pub    *adhocdata.Publisher
	runner bgjob.Runner[string]
	note   string
}

// sendToPlay publishes the table's record and opens play on it, off the
// frame. A send in flight drops the click.
func (inst *App) sendToPlay() {
	ps := &inst.send
	rec := inst.grid.rec
	switch {
	case inst.bus == nil:
		ps.note = "no bus: Send to play needs the app runtime"
		return
	case rec == nil || rec.NumRows() == 0:
		ps.note = "no rows to send"
		return
	case ps.runner.Running():
		return
	}
	if ps.pub == nil {
		ps.pub = adhocdata.NewWindowPublisher(datasetRows)
	}
	rec.Retain()
	bus, pub := inst.bus, ps.pub
	ps.note = ""
	ps.runner.Start(nil, bgjob.Spec{Kind: "trailviewer.send", Title: "Trail viewer: send to play"},
		func(ctx context.Context) (out *string, err error) {
			defer rec.Release()
			note, err := publishToPlay(bus, pub, rec)
			if err != nil {
				return
			}
			return &note, nil
		})
}

// publishToPlay publishes rec under the publisher's alias and asks the
// window host for a play window reading it.
func publishToPlay(bus app.BusI, pub *adhocdata.Publisher, rec arrow.RecordBatch) (note string, err error) {
	if _, err = pub.PublishRecord(bus, rec); err != nil {
		return "", eh.Errorf("publish %s: %w", datasetRows, err)
	}
	cfg, err := buscodec.Encode(playlaunch.PlayLaunch{Sql: playSql, AutoRun: true, Endpoint: playlaunch.EndpointIntrospection,
		Datasets: []string{pub.Alias()}, DatasetNames: []string{datasetRows}})
	if err != nil {
		return "", eh.Errorf("encode the play launch config: %w", err)
	}
	if _, err = windowhost.RequestOpen(bus, playlaunch.AppId, playlaunch.Kind, cfg); err != nil {
		return "", eh.Errorf("open play: %w", err)
	}
	return fmt.Sprintf("sent %d rows to play as %s", rec.NumRows(), datasetRows), nil
}

// drainSend lands the send's outcome.
func (inst *App) drainSend() {
	ps := &inst.send
	if note, _, ok := ps.runner.TakeResult(); ok {
		ps.note = *note
	} else if snap := ps.runner.Snapshot(); snap.State == bgjob.StateFailed {
		ps.runner.Invalidate()
		ps.note = "could not send to play"
		if snap.Err != nil {
			ps.note += ": " + snap.Err.Error()
		}
	}
}
