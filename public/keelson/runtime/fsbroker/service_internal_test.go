package fsbroker

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// TestService_Pending_ExpiresAfterDialogTimeout: a dialog whose requester
// has given up (DialogTimeout) is neither listed nor resolvable.
func TestService_Pending_ExpiresAfterDialogTimeout(t *testing.T) {
	svc, err := NewService(inprocbus.NewInst(zerolog.Nop()), zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()
	svc.mu.Lock()
	svc.pending["stale"] = &pendingEntry{id: "stale", op: "read", appId: "test.app", created: time.Now().Add(-DialogTimeout - time.Second)}
	svc.pending["fresh"] = &pendingEntry{id: "fresh", op: "read", appId: "test.app", created: time.Now()}
	svc.mu.Unlock()

	all := svc.Pending()
	require.Len(t, all, 1)
	require.Equal(t, "fresh", all[0].Id)
	_, err = svc.Resolve("stale", "/etc/hostname")
	require.Error(t, err)
}
