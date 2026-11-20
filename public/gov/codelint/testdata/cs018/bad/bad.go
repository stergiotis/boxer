package bad

import (
	"context"
	"net"
	"net/http"
)

func listen() (net.Listener, error) {
	return net.Listen("tcp", ":8080") // want a CS018 finding here
}

func listenConfig(ctx context.Context) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp", ":8080") // want a CS018 finding here
}

func serve() error {
	return http.ListenAndServe(":8080", nil) // want a CS018 finding here
}

func serveMethod() error {
	srv := &http.Server{Addr: ":8080"}
	return srv.ListenAndServe() // want a CS018 finding here
}
