package natsbus_test

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/natsbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// A NATS URL may carry userinfo; a failed connect's error shows neither the
// password in its message nor in its structured fields.
func TestConnectErrorDoesNotCarryThePassword(t *testing.T) {
	_, err := natsbus.Connect(natsbus.Options{URL: "nats://bob:s3cret@127.0.0.1:1", ConnectOptions: []nats.Option{nats.Timeout(200 * time.Millisecond)}})
	require.Error(t, err)
	rendered := eh.FormatErrorPlainS(err)
	require.NotContains(t, rendered, "s3cret")
	require.Contains(t, rendered, "bob:xxxxx@")
}
