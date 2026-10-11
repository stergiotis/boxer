package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
)

// queueRig is a coordinator rig with an attention service whose flood limit
// admits perStream items per stream (ADR-0324, proposed).
func queueRig(t *testing.T, perStream int) (*rig, *attention.Service) {
	att := &attention.Service{Levels: &attention.Levels{}, Flood: attention.NewFlood(perStream, 0)}
	att.Queue = attention.NewQueue(att.Levels, 0)
	r := newRigWith(t, func(cfg *Config) {
		cfg.Coordinators = []string{"test.coordinator"}
		cfg.Attention = att
	})
	att.Queue.AddSource(r.svc)
	return r, att
}

func waitingKinds(att *attention.Service) (kinds []string) {
	open, _ := att.Queue.Snapshot(time.Now())
	for _, e := range open {
		kinds = append(kinds, e.Kind)
	}
	return
}

func TestRequestsAreQueueItemsNotModals(t *testing.T) {
	r, att := queueRig(t, 0)
	_, _, err := r.cli.RequestKey(context.Background(), GrantRequest{Plan: "tidy", Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	require.NoError(t, err)
	open, _ := att.Queue.Snapshot(time.Now())
	require.Len(t, open, 1)
	e := open[0]
	assert.Equal(t, attention.KindGrantRequest, e.Kind)
	assert.Equal(t, attention.LevelYourTurn, e.Level)
	assert.Equal(t, "expires", e.BoundWhy)
	assert.NotContains(t, e.Summary, "tidy", "the queue's line is host-written, not the model's plan")
	assert.False(t, r.svc.Chrome().RenderDialogs(nil), "no modal with the queue installed")
}

func TestAConfirmationIsCritical(t *testing.T) {
	r, g := approvedRig(t, ModeAct)
	att := &attention.Service{Levels: &attention.Levels{}}
	att.Queue = attention.NewQueue(att.Levels, 0)
	att.Queue.AddSource(r.svc)
	require.Equal(t, "proposed", r.call(g, "x1", "export", "{}").Phase)
	open, _ := att.Queue.Snapshot(time.Now())
	require.Len(t, open, 1)
	assert.Equal(t, attention.KindConfirmation, open[0].Kind)
	assert.Equal(t, attention.LevelCritical, open[0].Level)
}

func TestTheFloodLimitRefusesARequest(t *testing.T) {
	r, att := queueRig(t, 1)
	ctx := context.Background()
	_, _, err := r.cli.RequestKey(ctx, GrantRequest{Conversation: "c1", Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	require.NoError(t, err)
	_, _, err = r.cli.RequestKey(ctx, GrantRequest{Conversation: "c1", Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "fewer and broader")
	_, _, err = r.cli.RequestKey(ctx, GrantRequest{Conversation: "c2", Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	require.NoError(t, err, "another conversation is its own stream")
	assert.Len(t, waitingKinds(att), 2)
}

func TestTheFloodLimitRefusesAWidening(t *testing.T) {
	r, _ := queueRig(t, 1)
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeObserve}}})
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	out := r.call(g, "w", "set_text", `{"text":"agent"}`)
	assert.Equal(t, "refused", out.Phase, "the stream spent its one item on the grant request")
	assert.Contains(t, out.Reason, "fewer and broader")
}
