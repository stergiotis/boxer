package trailviewer

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
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

// SendToPlayResult is send_to_play's result: what was handed to the
// worker; the publish and the window open land later, and the line beside
// the button says how they went.
type SendToPlayResult struct {
	Rows    int64  `desc:"the rows published: the rows the window holds"`
	Dataset string `desc:"the name the play window reads the dataset under"`
}

// sendToPlay publishes the table's record and opens play on it, off the
// frame; the button and send_to_play both land here. A send in flight
// refuses the next.
func (inst *App) sendToPlay() (out SendToPlayResult, err error) {
	ps := &inst.send
	rec := inst.grid.rec
	switch {
	case inst.bus == nil:
		err = app.RefuseOperation("no bus: Send to play needs the app runtime")
		return
	case rec == nil || rec.NumRows() == 0:
		err = app.RefuseOperation("no rows to send: the table is empty or not read yet")
		return
	case ps.runner.Running():
		err = app.RefuseOperation("a send to play is in flight; ask again shortly")
		return
	}
	out = SendToPlayResult{Rows: rec.NumRows(), Dataset: datasetRows}
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
	return
}

// sendToPlayGesture routes the button through the catalog (ADR-0345
// §SD3); a refusal from either path is shown beside the button.
func (inst *App) sendToPlayGesture() {
	gestureOr(inst, opSendToPlay, appops.None{}, func() {
		if _, err := inst.sendToPlay(); err != nil {
			inst.send.note = err.Error()
		}
	})
}

// publishToPlay publishes rec under the publisher's alias and asks the
// window host for a play window reading it.
func publishToPlay(bus app.BusI, pub *adhocdata.Publisher, rec arrow.RecordBatch) (note string, err error) {
	if _, err = pub.PublishRecord(bus, rec); err != nil {
		return "", eh.Errorf("publish %s: %w", datasetRows, err)
	}
	// The default endpoint, not the introspection one: play routes a statement
	// naming only keelson tables to the host and one naming plain tables to
	// the server, so the same window reads the rows and the marts.
	if _, err = playground.Open(bus, playground.Launch{Sql: playSql, AutoRun: true,
		Datasets: []string{pub.Alias()}, DatasetNames: []string{datasetRows}}); err != nil {
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
