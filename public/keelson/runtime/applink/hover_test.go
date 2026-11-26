package applink

import (
	"context"
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
