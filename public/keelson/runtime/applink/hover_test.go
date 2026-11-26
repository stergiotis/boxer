package applink

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

func TestHovers_AskOnceAndAnswerFromTheCache(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	host := bus.NewClient("test.host", []app.SubjectFilter{
		{Pattern: DescribeSubject, Direction: app.CapDirectionSub},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub},
	})
	var mu sync.Mutex
	asked := 0
	_, err := host.Subscribe(DescribeSubject, func(msg *app.Msg) {
		mu.Lock()
		asked++
		mu.Unlock()
		_ = buscodec.Reply(host.Publish, msg.Reply, DescribeReply{V: 1, Display: "Sheet", Summary: "set the statement"})
	})
	require.NoError(t, err)
	surface := bus.NewClient("test.surface", []app.SubjectFilter{DescribeCap("test")})
	var wg sync.WaitGroup
	spawn := func(f func(ctx context.Context)) bool {
		wg.Add(1)
		go func() { defer wg.Done(); f(context.Background()) }()
		return true
	}
	h := NewHovers(surface, UnknownAuthor, spawn)
	assert.Equal(t, hoverAsking, h.Text("boxer:open/app/sheet"))
	wg.Wait()
	assert.Equal(t, "Sheet — set the statement", h.Text("boxer:open/app/sheet"))
	assert.Equal(t, "Sheet — set the statement", h.Text("boxer:open/app/sheet"))
	mu.Lock()
	assert.Equal(t, 1, asked, "asked once while the answer is fresh")
	mu.Unlock()

	var nilHovers *Hovers
	assert.Equal(t, "", nilHovers.Text("boxer:open/app/sheet"))
}

// A page with more links than the cache remembers asks about each once:
// links drawn every frame are not forgotten, and asks go a few at a time.
func TestHovers_ManyLinksAreAskedAboutOnce(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	host := bus.NewClient("test.host", []app.SubjectFilter{
		{Pattern: DescribeSubject, Direction: app.CapDirectionSub},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub},
	})
	var mu sync.Mutex
	asked := map[string]int{}
	_, err := host.Subscribe(DescribeSubject, func(msg *app.Msg) {
		req, _ := buscodec.Decode[DescribeRequest](msg.Payload)
		mu.Lock()
		asked[req.Link]++
		mu.Unlock()
		_ = buscodec.Reply(host.Publish, msg.Reply, DescribeReply{V: 1, Display: "Sheet"})
	})
	require.NoError(t, err)
	surface := bus.NewClient("test.surface", []app.SubjectFilter{DescribeCap("test")})
	var wg sync.WaitGroup
	spawn := func(f func(ctx context.Context)) bool {
		wg.Add(1)
		go func() { defer wg.Done(); f(context.Background()) }()
		return true
	}
	h := NewHovers(surface, UnknownAuthor, spawn)
	const links = maxHovers + 44
	link := func(i int) string { return "boxer:open/app/sheet/set_sql@1?sql=" + strconv.Itoa(i) }
	for frame := 0; frame < 200; frame++ {
		for i := 0; i < links; i++ {
			h.Text(link(i))
		}
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, asked, links, "every link was asked about")
	for l, n := range asked {
		assert.Equal(t, 1, n, "asked about %s more than once", l)
	}
}
