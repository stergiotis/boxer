package mdedit

// Upload and send-to-play (ADR-0217): two gestures over one pipeline. Upload
// persists the document in `boxer.facts` through the facts-bound record
// store — the MdDoc row plus its heading, code, link, emphasis, tag and
// frontmatter rows, the same rows the markdown ingestor writes — so "what
// did I upload and when" is a query, and re-uploading identical text is
// visibly the same entity (the natural key is the content hash).
// Send-to-play is the upload plus a view of it: play opened on a launch
// query that selects exactly the document row back, with the content column
// glossed `text/markdown` so the Detail pane renders it as a document
// (ADR-0123/0186).
//
// Each gesture runs whole on one goroutine off the render thread (the
// tally/sysmetricsd shape): connect, verify, ingest, flush — and, for
// send-to-play only, the launch, which is why only that gesture needs the
// bus. Connection failures surface in the status line, and VerifySchema
// never provisions — chstore owns boxer.facts, and a host that has not run
// its DDL should hear that rather than have a table appear on the sly
// (ADR-0184 §SD2).

import (
	"context"
	"time"

	playlaunch "github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	// sendPlayTimeout bounds a whole gesture: connect, verify, ingest, flush
	// and — for send-to-play — the launch. Generous for a first connection,
	// bounded so a wrong endpoint fails as a status line rather than a stuck
	// gesture.
	sendPlayTimeout = 30 * time.Second

	tipUpload = "Persist the document as boxer.facts rows: the mdDoc row plus its extracted heading, code, link, emphasis, tag and frontmatter items — the markdown ingestor's shape, so it queries the same way a vault does. Needs a reachable ClickHouse (CLICKHOUSE_ENDPOINT) with the facts schema provisioned."

	tipSendPlay = "Upload to boxer.facts AND open the SQL playground on a query that reads the document row back — the content renders as markdown in play's Detail pane."
)

var (
	atomsUpload   = c.Atoms().Text("Upload to boxer.facts").Keep()
	atomsSendPlay = c.Atoms().Text("Send to play").Keep()
)

// playLaunchSQL is the query the play window opens with: the one row this
// send wrote, its content glossed as markdown for the Detail pane. The
// LW_COMPONENT read carries the kind's conformance filter itself; the id
// filter narrows it to this send. "id:id" is the friendly handle play's
// column resolver maps to the physical facts column.
func playLaunchSQL(id uint64) (sql string) {
	sql = "SELECT gloss(tupleElement(LW_COMPONENT('MdDoc'), 'Content'), 'text/markdown', 'label', 'doc')\n" +
		"FROM boxer.facts\n" +
		"WHERE \"id:id\" = " + utoa(id) + "\n" +
		"LIMIT 1"
	return
}

// utoa is itoa's uint64 sibling, for ids too large for int.
func utoa(v uint64) (s string) {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// sendInFlight reports whether a send is already running; the button renders
// regardless and the click is dropped (the house rule for in-flight gestures).
func (inst *App) sendInFlight() (busy bool) {
	inst.mu.Lock()
	busy = inst.sending
	inst.mu.Unlock()
	return
}

// sendDoc snapshots the buffer and runs one of the two gestures off-thread:
// the upload alone, or the upload plus the play launch. Only the launch
// half touches the bus, so an upload works in a host with none.
func (inst *App) sendDoc(openPlay bool) {
	if openPlay && inst.bus == nil {
		inst.status = "no bus wired — cannot open play"
		return
	}
	inst.mu.Lock()
	if inst.sending {
		inst.mu.Unlock()
		return
	}
	inst.sending = true
	inst.mu.Unlock()
	gesture := "upload to boxer.facts"
	if openPlay {
		gesture = "send to play"
	}
	inst.status = gesture + "…"

	// Snapshots, taken on the render thread: the goroutine touches no App
	// state until it reports back through the guarded fields. The title and
	// the word count are the extractor's, taken off the same bytes.
	src := inst.src
	fileName := inst.boundName
	if fileName == "" {
		fileName = inst.readName
	}
	bus := inst.bus

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendPlayTimeout)
		defer cancel()
		id, err := uploadDoc(ctx, src, fileName)
		msg := "uploaded to boxer.facts · id " + utoa(id)
		if err == nil && openPlay {
			err = openInPlay(bus, id)
			msg = "sent to play"
		}
		inst.mu.Lock()
		inst.sending = false
		inst.sendDone = true
		inst.sendErr = err
		inst.sendGesture = gesture
		inst.sendMsg = msg
		inst.mu.Unlock()
	}()
}

// uploadDoc is the persistence half: one document's rows in — the MdDoc row
// plus its extracted items, via the same IngestDocument the markdown
// ingestor uses — and the document row's id out, the key a launch query (or
// a hand-written one) selects it by. Pure with respect to App state.
func uploadDoc(ctx context.Context, src, fileName string) (id uint64, err error) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	if err = client.Ping(ctx); err != nil {
		return
	}
	exec, err := storeexec.New(client, nil)
	if err != nil {
		return
	}
	store := mddocfacts.NewMddocStore(exec, nil, mddocfacts.MddocStoreConfig{})
	defer store.Close()
	if err = store.VerifySchema(ctx); err != nil {
		return
	}

	rows, err := store.IngestDocument([]byte(src), fileName, time.Now().UTC())
	if err != nil {
		return
	}
	if _, err = store.Flush(ctx); err != nil {
		return
	}
	id = rows.Doc.Id
	return
}

// openInPlay is the view half: play opened on the row an upload just wrote.
// Sequenced after the Flush by its caller, so the AutoRun query cannot race
// its own row.
func openInPlay(bus app.BusI, id uint64) (err error) {
	cfg := playlaunch.PlayLaunch{
		Sql:     playLaunchSQL(id),
		AutoRun: true,
		Tab:     "detail",
	}
	cfgBytes, err := buscodec.Encode(cfg)
	if err != nil {
		return
	}
	_, err = windowhost.RequestOpen(bus, playlaunch.AppId, playlaunch.Kind, cfgBytes)
	return
}
