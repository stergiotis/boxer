package play

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_cardgrid_fixture.go is ADR-0245 §SD7: the Cards pane's sample data.
//
// It follows the series fixture lab (ADR-0163 M4): play publishes an ORDINARY
// ad-hoc dataset — `fixture_cards` — and gets out of the way. There is no demo
// mode and no path that knows it is looking at a fixture; the scaffold is a
// query someone can read, paste and change.
//
// The rows are procedural and chosen for §SD4's table rather than for looks:
// every one is a kind of input a card grid has to survive. Deterministic, so
// the tour scene's captures and the fold's golden are stable.

const (
	// cardgridFixtureAlias is the alias a buffer writes as keelson('<alias>').
	cardgridFixtureAlias = "fixture_cards"
)

// cardgridFixtureEpoch is the first row's `recorded`. A fixed instant, for
// the reason the series fixture's is.
var cardgridFixtureEpoch = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

// cardgridFixtureRow is one sample.
type cardgridFixtureRow struct {
	name, kind string
	content    []byte // nil is NULL
	mime       string // "" is NULL
	note       string
	tags       []string
	tone       string // "" is NULL
	// Zero is NULL for the four measures below: a recording has no width and
	// a picture no sample rate, and a NULL fact is simply not on the card.
	lengthMS, width, height, sampleRate, channels int64
}

// cardgridFixtureRows builds the samples. Pure and deterministic.
func cardgridFixtureRows() (rows []cardgridFixtureRow, err error) {
	img := func(name string, w, h, hue int, note string, tags ...string) cardgridFixtureRow {
		return cardgridFixtureRow{name: name, kind: "image", content: cardgridFixturePNG(w, h, hue), mime: "image/png",
			note: note, tags: tags, width: int64(w), height: int64(h)}
	}
	rows = append(rows,
		img("Harbour at dusk", 640, 360, 0, "A hero at the box's own aspect.\n\n- every slot filled\n- nothing too long", "photo", "sample"),
		img("Panorama", 1600, 200, 1, "Eight to one. **Contained** in the box as a band — never cropped, never stretched.", "photo", "wide"),
		img("Strip", 120, 960, 2, "One to eight: contained as a sliver.", "photo", "tall"),
		img("Icon", 16, 16, 3, "Sixteen pixels square, and *not* scaled past its native size.", "icon"),
		img("Large photograph", 2400, 1600, 4, "3.8 megapixels decoded, a thumbnail retained: a page costs thumbnails, not originals.", "photo", "large"),
	)

	jpg, err := cardgridFixtureJPEG(800, 600, 5)
	if err != nil {
		return nil, err
	}
	rows = append(rows, cardgridFixtureRow{name: "A JPEG among the PNGs", kind: "image", content: jpg, mime: "image/jpeg",
		note: "The row says what it is: `mime AS card_hero_gloss`.", tags: []string{"photo", "jpeg"}, width: 800, height: 600})
	anim, err := cardgridFixtureGIF(160, 120)
	if err != nil {
		return nil, err
	}
	rows = append(rows, cardgridFixtureRow{name: "Animated GIF", kind: "image", content: anim, mime: "image/gif",
		note: "Two frames; the card shows the first.", tags: []string{"gif"}, width: 160, height: 120})

	valid := cardgridFixturePNG(320, 180, 6)
	rows = append(rows,
		cardgridFixtureRow{name: "Over the pixel budget", kind: "image", content: cardgridFixtureHeaderOnlyPNG(30000, 30000), mime: "image/png",
			note: "A header claiming 30000 × 30000. It is refused from the header, before anything is allocated.", tags: []string{"rejected"},
			tone: "warning", width: 30000, height: 30000},
		cardgridFixtureRow{name: "Truncated PNG", kind: "image", content: valid[:len(valid)/2], mime: "image/png",
			note: "Half a file. The reason is shown where the hero would have been.", tags: []string{"rejected"}, tone: "error"},
		cardgridFixtureRow{name: "Misspelt media type", kind: "image", content: valid, mime: "image/pgn",
			note: "`image/pgn`: a row value that does not bind is loud on its own card, not silently plain.", tags: []string{"rejected"}, tone: "error",
			width: 320, height: 180},
		cardgridFixtureRow{name: "No hero on this card", kind: "note",
			note: "`content` and `mime` are NULL. The box stays, so the row keeps its line.", tags: []string{"note"}},
		cardgridFixtureRow{
			name: "/srv/archive/2026/03/02/sensors/north-pier/anemometer/raw/0000000000000000000000000000000000000000000000000000000017.parquet",
			kind: "file", content: cardgridFixturePNG(320, 180, 7), mime: "image/png",
			note: "A title with no break opportunity: wrapped anywhere, cut at two lines, whole on hover.\n\n| slot | budget |\n| --- | --- |\n| title | 2 lines |\n| body | 3 lines |\n",
			tags: []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}, tone: "accent", width: 320, height: 180},
	)

	wav := func(name, note string, format pcm.Format, seconds float64, enc wavfile.EncodingE, bits uint16, fn func(pcm.Format, int64) pcm.SampleFunc, tags ...string) error {
		frames := int64(seconds * float64(format.SampleRate))
		data, wErr := cardgridFixtureWAV(format, frames, enc, bits, fn(format, frames))
		if wErr != nil {
			return wErr
		}
		rows = append(rows, cardgridFixtureRow{name: name, kind: "audio", content: data, mime: "audio/wav", note: note, tags: tags,
			lengthMS: int64(seconds * 1000), sampleRate: int64(format.SampleRate), channels: int64(format.Channels)})
		return nil
	}
	mono44 := pcm.Format{SampleRate: 44100, Channels: 1}
	stereo48 := pcm.Format{SampleRate: 48000, Channels: 2}
	for _, w := range []struct {
		name, note string
		format     pcm.Format
		seconds    float64
		enc        wavfile.EncodingE
		bits       uint16
		fn         func(pcm.Format, int64) pcm.SampleFunc
		tags       []string
	}{
		{"Sine sweep", "100 Hz to 4 kHz over three seconds, swelling and fading. The hero is the recording's waveform.", mono44, 3, wavfile.EncodingPCMInt, 16,
			func(f pcm.Format, n int64) pcm.SampleFunc {
				return cardgridFixtureSwell(pcm.Chirp(f, n, 100, 4000, 0.8), n)
			}, []string{"audio", "mono"}},
		{"Gated tone, stereo", "A tone under a gate on the left, a lower one on the right — speech-shaped, two channels.", stereo48, 2, wavfile.EncodingPCMInt, 16,
			func(f pcm.Format, _ int64) pcm.SampleFunc {
				return pcm.PerChannel(pcm.Gate(pcm.Sine(f, 440, 0.7), 9600, 4800), pcm.Gate(pcm.Sine(f, 220, 0.5), 4800, 9600))
			}, []string{"audio", "stereo"}},
		{"Silence", "A second of nothing: a flat line is a waveform too.", mono44, 1, wavfile.EncodingPCMInt, 16,
			func(pcm.Format, int64) pcm.SampleFunc { return pcm.Silence() }, []string{"audio"}},
		{"Eight-bit telephone tone", "8 kHz, 8 bits, in a ringing cadence.", pcm.Format{SampleRate: 8000, Channels: 1}, 1, wavfile.EncodingPCMInt, 8,
			func(f pcm.Format, _ int64) pcm.SampleFunc { return pcm.Gate(pcm.Sine(f, 350, 0.6), 3200, 1600) }, []string{"audio", "8-bit"}},
		{"Float samples", "IEEE float, 32 bits: a falling sweep, struck and left to ring out.", mono44, 1.5, wavfile.EncodingIEEEFloat, 32,
			func(f pcm.Format, n int64) pcm.SampleFunc {
				return cardgridFixtureDecay(pcm.Chirp(f, n, 2000, 200, 0.9), n)
			}, []string{"audio", "float"}},
	} {
		if err = wav(w.name, w.note, w.format, w.seconds, w.enc, w.bits, w.fn, w.tags...); err != nil {
			return nil, err
		}
	}
	// A recording cut short: the header promises more than the file holds.
	whole := rows[len(rows)-5].content
	rows = append(rows, cardgridFixtureRow{name: "Truncated recording", kind: "audio", content: whole[:len(whole)/3], mime: "audio/wav",
		note: "A third of a file whose header promises the whole.", tags: []string{"audio", "truncated"}, tone: "warning",
		lengthMS: 3000, sampleRate: 44100, channels: 1})
	return rows, nil
}

// cardgridFixtureSwell shapes a signal with half a sine over its length, so
// its waveform reads as a swell rather than a block.
func cardgridFixtureSwell(inner pcm.SampleFunc, frames int64) pcm.SampleFunc {
	total := float64(max(frames, 1))
	return func(frame int64, ch int) float32 {
		return inner(frame, ch) * float32(math.Sin(math.Pi*float64(frame)/total))
	}
}

// cardgridFixtureDecay shapes a signal with an exponential decay: struck,
// then left to ring out.
func cardgridFixtureDecay(inner pcm.SampleFunc, frames int64) pcm.SampleFunc {
	total := float64(max(frames, 1))
	return func(frame int64, ch int) float32 {
		return inner(frame, ch) * float32(math.Exp(-4*float64(frame)/total))
	}
}

// cardgridFixturePNG is a two-axis gradient with a diagonal grid, so scaling
// and letterboxing are visible at any aspect.
func cardgridFixturePNG(w, h, hue int) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, cardgridFixtureImage(w, h, hue)) // a bytes.Buffer does not fail
	return buf.Bytes()
}

func cardgridFixtureJPEG(w, h, hue int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, cardgridFixtureImage(w, h, hue), &jpeg.Options{Quality: 80}); err != nil {
		return nil, eh.Errorf("play: cards fixture: jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

func cardgridFixtureImage(w, h, hue int) *image.RGBA {
	base := styletokens.QualitativeCycle(hue)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		fy := float32(y) / float32(max(h-1, 1))
		for x := range w {
			fx := float32(x) / float32(max(w-1, 1))
			r := float32(base.R) * (0.35 + 0.65*fx)
			g := float32(base.G) * (0.35 + 0.65*fy)
			b := float32(base.B) * (0.35 + 0.65*(1-fx))
			if (x+y)%32 == 0 {
				r, g, b = min(r+50, 255), min(g+50, 255), min(b+50, 255)
			}
			img.SetRGBA(x, y, color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff})
		}
	}
	return img
}

func cardgridFixtureGIF(w, h int) ([]byte, error) {
	palette := color.Palette{color.RGBA{0x10, 0x12, 0x14, 0xff}, color.RGBA{0xa9, 0xbc, 0xf2, 0xff}, color.RGBA{0xe6, 0xb5, 0x5d, 0xff}}
	anim := &gif.GIF{}
	for frame := range 2 {
		p := image.NewPaletted(image.Rect(0, 0, w, h), palette)
		for y := range h {
			for x := range w {
				if ((x/20)+(y/20))%2 == 0 {
					p.SetColorIndex(x, y, uint8(1+frame))
				}
			}
		}
		anim.Image = append(anim.Image, p)
		anim.Delay = append(anim.Delay, 50)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		return nil, eh.Errorf("play: cards fixture: gif: %w", err)
	}
	return buf.Bytes(), nil
}

// cardgridFixtureHeaderOnlyPNG is a PNG signature and an IHDR claiming w × h,
// and nothing else: enough for a header read to answer, which is all the
// pixel budget looks at — and a few dozen bytes instead of the image the
// header describes.
func cardgridFixtureHeaderOnlyPNG(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 0, 17)
	ihdr = append(ihdr, "IHDR"...)
	ihdr = binary.BigEndian.AppendUint32(ihdr, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 0, 0, 0, 0) // 8-bit greyscale, no interlace
	_ = binary.Write(&buf, binary.BigEndian, uint32(13))
	buf.Write(ihdr)
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(ihdr))
	return buf.Bytes()
}

func cardgridFixtureWAV(format pcm.Format, frames int64, enc wavfile.EncodingE, bits uint16, fn pcm.SampleFunc) ([]byte, error) {
	src, err := pcm.NewSynthSourceE(format, frames, fn)
	if err != nil {
		return nil, eh.Errorf("play: cards fixture: %w", err)
	}
	var buf bytes.Buffer
	if err = wavfile.WriteE(context.Background(), &buf, format, enc, bits, src); err != nil {
		return nil, eh.Errorf("play: cards fixture: wav: %w", err)
	}
	return buf.Bytes(), nil
}

// encodeCardgridFixture renders the rows as the Arrow IPC stream a publish
// takes.
func encodeCardgridFixture(rows []cardgridFixtureRow, alloc memory.Allocator) (out []byte, err error) {
	tsType := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "kind", Type: arrow.BinaryTypes.String},
		{Name: "content", Type: arrow.BinaryTypes.Binary, Nullable: true},
		{Name: "mime", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "note", Type: arrow.BinaryTypes.String},
		{Name: "tags", Type: arrow.ListOf(arrow.BinaryTypes.String)},
		{Name: "tone", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "recorded", Type: tsType},
		{Name: "bytes", Type: arrow.PrimitiveTypes.Int64},
		{Name: "length_ms", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "width", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "height", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "sample_rate", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "channels", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(alloc, schema)
	defer b.Release()
	str := func(i int, s string, nullIfEmpty bool) {
		sb := b.Field(i).(*array.StringBuilder)
		if nullIfEmpty && s == "" {
			sb.AppendNull()
			return
		}
		sb.Append(s)
	}
	num := func(i int, v int64) {
		nb := b.Field(i).(*array.Int64Builder)
		if v == 0 {
			nb.AppendNull()
			return
		}
		nb.Append(v)
	}
	for ri, r := range rows {
		str(0, r.name, false)
		str(1, r.kind, false)
		if r.content == nil {
			b.Field(2).(*array.BinaryBuilder).AppendNull()
		} else {
			b.Field(2).(*array.BinaryBuilder).Append(r.content)
		}
		str(3, r.mime, true)
		str(4, r.note, false)
		lb := b.Field(5).(*array.ListBuilder)
		lb.Append(true)
		for _, tag := range r.tags {
			lb.ValueBuilder().(*array.StringBuilder).Append(tag)
		}
		str(6, r.tone, true)
		b.Field(7).(*array.TimestampBuilder).Append(arrow.Timestamp(cardgridFixtureEpoch.Add(time.Duration(ri) * time.Minute).UnixMicro()))
		b.Field(8).(*array.Int64Builder).Append(int64(len(r.content)))
		num(9, r.lengthMS)
		num(10, r.width)
		num(11, r.height)
		num(12, r.sampleRate)
		num(13, r.channels)
	}
	rec := b.NewRecordBatch()
	defer rec.Release()
	return adhocdata.EncodeRecord(rec)
}

// cardgridFixtureScaffold is the query the affordance writes into the buffer
// after a publish: the ADR's own example, over the fixture.
func cardgridFixtureScaffold() string {
	return "\n-- ADR-0245: cards from a result. `card_*` names the slots, every other column is a fact,\n" +
		"-- and `mime AS card_hero_gloss` lets each row say what its hero is.\n" +
		"SELECT name    AS card_title,\n" +
		"       kind    AS card_overline,\n" +
		"       content AS card_hero,\n" +
		"       mime    AS card_hero_gloss,\n" +
		"       note    AS `card_body@text/markdown`,\n" +
		"       tags    AS card_tags,\n" +
		"       tone    AS card_tone,\n" +
		"       recorded AS card_footer,\n" +
		"       length_ms AS `length@gloss/duration;unit=ms`,\n" +
		"       bytes     AS `size@gloss/bytes`,\n" +
		"       width, height, sample_rate, channels\n" +
		"FROM keelson('" + cardgridFixtureAlias + "')\n"
}

// cardgridFixtureState is the affordance's state: the publisher — one alias,
// one handle across re-publishes — and the last round's outcome.
type cardgridFixtureState struct {
	publisher *adhocdata.Publisher

	mu         sync.Mutex
	publishing bool
	err        error
	summary    string
	generation uint64
}

func newCardgridFixtureState() *cardgridFixtureState {
	return &cardgridFixtureState{publisher: adhocdata.NewPublisher(cardgridFixtureAlias, false)}
}

func (inst *cardgridFixtureState) status() (publishing bool, summary string, gen uint64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.publishing, inst.summary, inst.generation, inst.err
}

// publishCardgridFixture generates and publishes, off the render thread.
// Single-flight, like the other write paths here.
func (inst *PlayApp) publishCardgridFixture() {
	if inst.bus == nil || inst.cardFixtures == nil {
		return
	}
	st := inst.cardFixtures
	st.mu.Lock()
	if st.publishing {
		st.mu.Unlock()
		return
	}
	st.publishing, st.err = true, nil
	st.mu.Unlock()

	bus := inst.bus
	go func() {
		summary, err := doPublishCardgridFixture(bus, st)
		st.mu.Lock()
		st.publishing, st.err = false, err
		if err == nil {
			st.summary = summary
			st.generation++
		}
		st.mu.Unlock()
	}()
}

func doPublishCardgridFixture(bus busPublisherI, st *cardgridFixtureState) (summary string, err error) {
	rows, err := cardgridFixtureRows()
	if err != nil {
		return
	}
	ipc, err := encodeCardgridFixture(rows, memory.NewGoAllocator())
	if err != nil {
		return
	}
	res, err := st.publisher.Publish(bus, ipc)
	if err != nil {
		return "", eb.Build().Str("alias", cardgridFixtureAlias).Errorf("play: cards fixture: publish: %w", err)
	}
	return fmt.Sprintf("%s: %d samples", cardgridFixtureAlias, res.Rows), nil
}

// syncCardgridFixture binds the alias and offers the scaffold once per
// publish. Called from the tab body, on the render thread, where BindDataset
// and the delivery ops both belong.
func (inst *PlayApp) syncCardgridFixture() {
	if inst.cardFixtures == nil {
		return
	}
	_, _, gen, _ := inst.cardFixtures.status()
	if gen == inst.cardFixturesSeen {
		return
	}
	inst.cardFixturesSeen = gen
	// The alias is the readable name; the HANDLE is the dataset the publish
	// minted (the series fixture's finding).
	if handle := inst.cardFixtures.publisher.Handle(); handle != "" {
		if err := inst.BindDataset(cardgridFixtureAlias, handle); err != nil {
			return
		}
	}
	// A buffer that already queries the fixture — restored from a previous
	// session, or published a second time — needs the binding, not a second
	// copy of the query.
	if !strings.Contains(inst.sql, "keelson('"+cardgridFixtureAlias+"')") {
		inst.InsertSqlAtCaret(cardgridFixtureScaffold())
	}
}

// renderCardgridFixtureOffer is the affordance, shown where the pane has
// nothing to draw: one button, and what the last round did.
func (inst *PlayApp) renderCardgridFixtureOffer() {
	if inst.cardFixtures == nil || inst.bus == nil {
		return
	}
	inst.syncCardgridFixture()
	publishing, summary, _, err := inst.cardFixtures.status()
	for range c.Horizontal().KeepIter() {
		label := icons.PhCards + " publish sample cards"
		if publishing {
			label = "publishing…"
		}
		if c.Button(inst.ids.PrepareStr("cards-fixture-publish"), c.Atoms().Text(label).Keep()).
			Small().SendResp().HasPrimaryClicked() && !publishing {
			inst.publishCardgridFixture()
		}
		switch {
		case err != nil:
			for rt := range c.RichTextLabel("The samples did not publish: " + err.Error()) {
				rt.Small().Weak()
			}
		case summary != "":
			for rt := range c.RichTextLabel("published " + summary + " — the query is in the editor; run it") {
				rt.Small().Weak()
			}
		default:
			for rt := range c.RichTextLabel("images and recordings as an ordinary ad-hoc dataset, keelson('" + cardgridFixtureAlias + "')") {
				rt.Small().Weak()
			}
		}
	}
}

// cardgridFixtureNames lists the fixture's titles — what a test or a scene
// asserts against without re-deriving the rows.
func cardgridFixtureNames(rows []cardgridFixtureRow) string {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.name
	}
	return strings.Join(names, "\n")
}
