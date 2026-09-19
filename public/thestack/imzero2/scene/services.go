package scene

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// services.go is the registry of helpers a scene may name (ADR-0248 §SD6).
// A scene document names a service; it cannot say what to run. That is the
// difference between a scene and a shell script, and it is deliberate.

type service struct {
	name   string
	cmd    *exec.Cmd
	export map[string]string
}

func (inst *service) stop() {
	if inst.cmd != nil {
		terminateGroup(inst.cmd)
		done := make(chan struct{})
		go func() { _ = inst.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			killGroup(inst.cmd)
			<-done
		}
		inst.cmd = nil
	}
}

// ServiceTileStub serves generated map tiles from loopback, so a map scene
// needs no network and captures the same pixels every run.
const ServiceTileStub = "tilestub"

func startService(name string, opts Options) (svc *service, err error) {
	switch name {
	case ServiceTileStub:
		return startTileStub(opts)
	default:
		return nil, eb.Build().Str("service", name).Errorf("unknown scene service (known: " + ServiceTileStub + ")")
	}
}

func startTileStub(opts Options) (svc *service, err error) {
	script := filepath.Join(opts.RepoRoot, "scripts", "dev", "tile-stub-server.py")
	cmd, err := extbin.Python3.Command(context.Background(), extbin.Opts{}, script, "0")
	if err != nil {
		return nil, err
	}
	ownGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, eb.Build().Errorf("unable to pipe the tile stub: %w", err)
	}
	if err = cmd.Start(); err != nil {
		return nil, eb.Build().Str("script", script).Errorf("unable to start the tile stub: %w", err)
	}
	svc = &service{name: ServiceTileStub, cmd: cmd}
	ready := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if port, ok := strings.CutPrefix(sc.Text(), "ready "); ok {
				ready <- strings.TrimSpace(port)
				break
			}
		}
		close(ready)
		// Keep draining so the server never blocks on a full pipe.
		for sc.Scan() {
		}
	}()
	select {
	case port := <-ready:
		if port == "" {
			svc.stop()
			return nil, eb.Build().Errorf("the tile stub exited without announcing a port")
		}
		svc.export = map[string]string{"BOXER_MAP_TILE_URL": "http://127.0.0.1:" + port + "/{z}/{x}/{y}.png"}
		return svc, nil
	case <-time.After(15 * time.Second):
		svc.stop()
		return nil, eb.Build().Errorf("the tile stub did not announce a port")
	}
}
