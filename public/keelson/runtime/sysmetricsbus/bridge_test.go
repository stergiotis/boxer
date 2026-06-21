package sysmetricsbus_test

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stretchr/testify/require"
)

// TestBridge_RelaysAcrossBuses proves Bridge relays a subject from a source bus
// to a destination bus — the headless link, where the source stands in for the
// external sysmetricsd's NATS plane and the destination for the carrier's
// in-proc host bus. Two inprocbus Insts model the two transports.
func TestBridge_RelaysAcrossBuses(t *testing.T) {
	src := inprocbus.NewInst(zerolog.Nop())
	dst := inprocbus.NewInst(zerolog.Nop())

	subCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub}}
	pubCap := []app.SubjectFilter{{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub}}

	stop, err := sysmetricsbus.Bridge(
		src.NewClient("bridge", subCap),
		dst.NewClient("bridge", pubCap),
		sysmetricsbus.BundleSubjectWildcard(),
	)
	require.NoError(t, err)
	t.Cleanup(stop)

	got := make(chan []byte, 1)
	unsub, err := dst.NewClient("consumer", subCap).Subscribe(sysmetricsbus.BundleSubjectWildcard(), func(m *app.Msg) {
		select {
		case got <- m.Payload:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(unsub)

	require.NoError(t, src.NewClient("producer", pubCap).Publish(sysmetricsbus.BundleSubject("h1"), []byte("frame")))

	select {
	case payload := <-got:
		require.Equal(t, []byte("frame"), payload)
	case <-time.After(time.Second):
		t.Fatal("Bridge did not relay the message src->dst")
	}
}
