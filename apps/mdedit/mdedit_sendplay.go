package mdedit

// Send-to-play (its own ADR records the flow): the document is persisted as
// one MdDoc row in `boxer.facts` through the facts-bound record store, and
// play is opened on a launch query that selects exactly that row back, with
// the content column glossed `text/markdown` so the Detail pane renders it as
// a document (ADR-0123/0186).
//
// Persisting rather than merely displaying is the point of the gesture: a
// sent document is a fact with a timestamp, a hash and a name, so "what did I
// send and when" is a query, and re-sending identical text is visibly the
// same entity (the natural key is the content hash).
//
// The whole pipeline — connect, verify, ingest, flush, launch — runs on one
// goroutine off the render thread (the tally/sysmetricsd shape): connection
// failures surface in the status line, and VerifySchema never provisions —
// chstore owns boxer.facts, and a host that has not run its DDL should hear
// that rather than have a table appear on the sly (ADR-0184 §SD2).

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"time"

	"lukechampine.com/blake3"

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
	// sendPlayTimeout bounds the whole send: connect, verify, ingest, flush,
	// launch. Generous for a first connection, bounded so a wrong endpoint
	// fails as a status line rather than a stuck gesture.
	sendPlayTimeout = 30 * time.Second

	tipSendPlay = "Persist the document as a boxer.facts row (kind mdDoc) and open the SQL playground on a query that reads it back — the content renders as markdown in play's Detail pane. Needs a reachable ClickHouse (CLICKHOUSE_ENDPOINT) with the facts schema provisioned."
)

var atomsSendPlay = c.Atoms().Text("Send to play").Keep()

// buildMdDocRow is the pure half of the send: the row for this buffer at this
// moment. Id hashes (content, ts) so every send is its own row — the launch
// filter key — while NaturalKey hashes the content alone, so identical text
// is the same entity across sends.
func buildMdDocRow(src, title, fileName string, words int, ts time.Time) (row mddocfacts.MdDoc) {
	contentHash := blake3.Sum256([]byte(src))

	idh := blake3.New(8, nil)
	_, _ = idh.Write([]byte(src))
	var tsb [8]byte
	binary.LittleEndian.PutUint64(tsb[:], uint64(ts.UnixNano()))
	_, _ = idh.Write(tsb[:])

	row = mddocfacts.MdDoc{
		Id:          binary.LittleEndian.Uint64(idh.Sum(nil)),
		NaturalKey:  contentHash[:],
		Ts:          ts,
		Kind:        "mdDoc",
		Title:       title,
		FileName:    fileName,
		Content:     src,
		ContentHash: hex.EncodeToString(contentHash[:]),
		Words:       uint64(words),
	}
	return
}

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

// sendToPlay snapshots the buffer and runs the pipeline off-thread.
func (inst *App) sendToPlay() {
	if inst.bus == nil {
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
	inst.status = "sending to play…"

	// Snapshots, taken on the render thread: the goroutine touches no App
	// state until it reports back through the guarded fields.
	src := inst.src
	title := firstHeadingText(inst.headings())
	fileName := inst.boundName
	if fileName == "" {
		fileName = inst.readName
	}
	words := inst.stats.Words
	bus := inst.bus

	go func() {
		err := sendDocToPlay(bus, src, title, fileName, words)
		inst.mu.Lock()
		inst.sending = false
		inst.sendDone = true
		inst.sendErr = err
		inst.mu.Unlock()
	}()
}

// sendDocToPlay is the pipeline: one row in, one play window out. Pure with
// respect to App state.
func sendDocToPlay(bus app.BusI, src, title, fileName string, words int) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), sendPlayTimeout)
	defer cancel()

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

	row := buildMdDocRow(src, title, fileName, words, time.Now())
	if err = store.IngestMdDoc(row.Ts, []mddocfacts.MdDoc{row}); err != nil {
		return
	}
	if _, err = store.Flush(ctx); err != nil {
		return
	}

	cfg := playlaunch.PlayLaunch{
		Sql:     playLaunchSQL(row.Id),
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
