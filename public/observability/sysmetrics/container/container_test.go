package container_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/container"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
)

func TestDetect_Podman_ContainerEnv(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run", ".containerenv"), nil, 0o644))

	d, _ := container.New(container.Options{MarkerRoot: root, Proc: procfs.New(t.TempDir())})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EnginePodman, info.Engine)
}

func TestDetect_Docker_DotDockerenv(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".dockerenv"), nil, 0o644))

	d, _ := container.New(container.Options{MarkerRoot: root, Proc: procfs.New(t.TempDir())})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineDocker, info.Engine)
}

func TestDetect_SystemdNspawn_FromContainerFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run/systemd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run/systemd/container"), []byte("systemd-nspawn\n"), 0o644))

	d, _ := container.New(container.Options{MarkerRoot: root, Proc: procfs.New(t.TempDir())})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineSystemdNspawn, info.Engine)
	assert.Equal(t, "systemd-nspawn", info.Detail)
}

func TestDetect_VendorContainerFile_Lxc(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run/systemd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run/systemd/container"), []byte("lxc\n"), 0o644))

	d, _ := container.New(container.Options{MarkerRoot: root, Proc: procfs.New(t.TempDir())})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineLXC, info.Engine)
}

func TestDetect_VendorContainerFile_Unknown(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run/systemd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run/systemd/container"), []byte("custom-runtime-xyz\n"), 0o644))

	d, _ := container.New(container.Options{MarkerRoot: root, Proc: procfs.New(t.TempDir())})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineUnknown, info.Engine)
	assert.Equal(t, "custom-runtime-xyz", info.Detail)
}

func TestDetect_Cgroup_Kubernetes(t *testing.T) {
	procRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "1"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(procRoot, "1", "cgroup"),
		[]byte("0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod1234abcd.slice/cri-containerd-abcdef.scope\n"),
		0o644,
	))
	d, _ := container.New(container.Options{MarkerRoot: t.TempDir(), Proc: procfs.New(procRoot)})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineKubernetes, info.Engine)
	assert.Contains(t, info.Detail, "kubepods")
}

func TestDetect_Cgroup_DockerWithoutMarker(t *testing.T) {
	procRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "1"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(procRoot, "1", "cgroup"),
		[]byte("0::/docker/abcdef0123456789\n"),
		0o644,
	))
	d, _ := container.New(container.Options{MarkerRoot: t.TempDir(), Proc: procfs.New(procRoot)})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineDocker, info.Engine)
}

func TestDetect_Cgroup_LXC(t *testing.T) {
	procRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "1"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(procRoot, "1", "cgroup"),
		[]byte("0::/lxc/mycontainer/init.scope\n"),
		0o644,
	))
	d, _ := container.New(container.Options{MarkerRoot: t.TempDir(), Proc: procfs.New(procRoot)})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineLXC, info.Engine)
}

func TestDetect_NotInContainer(t *testing.T) {
	procRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "1"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(procRoot, "1", "cgroup"),
		[]byte("0::/init.scope\n"),
		0o644,
	))
	d, _ := container.New(container.Options{MarkerRoot: t.TempDir(), Proc: procfs.New(procRoot)})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineNone, info.Engine)
}

func TestDetect_Precedence_PodmanBeatsDockerEnv(t *testing.T) {
	// If both /run/.containerenv and /.dockerenv are present, podman wins
	// (matches btop ordering).
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run/.containerenv"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".dockerenv"), nil, 0o644))

	d, _ := container.New(container.Options{MarkerRoot: root, Proc: procfs.New(t.TempDir())})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EnginePodman, info.Engine)
}

func TestDetect_NoProc_NoMarker_ReturnsNone(t *testing.T) {
	d, _ := container.New(container.Options{MarkerRoot: t.TempDir(), Proc: procfs.New(t.TempDir())})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, container.EngineNone, info.Engine)
}

func TestDetect_LiveSystem_Smoke(t *testing.T) {
	d, _ := container.New(container.Options{})
	info, err := d.Detect(context.Background())
	require.NoError(t, err)
	// Either we're in a container or we're not — test runner should not
	// be a docker container, but tolerate either outcome.
	t.Logf("live container detection: engine=%s detail=%q", info.Engine, info.Detail)
}

func TestDetect_ContextCancelled(t *testing.T) {
	d, _ := container.New(container.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.Detect(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestEngineE_String(t *testing.T) {
	cases := map[container.EngineE]string{
		container.EngineNone:          "none",
		container.EngineUnknown:       "unknown",
		container.EngineDocker:        "docker",
		container.EnginePodman:        "podman",
		container.EngineLXC:           "lxc",
		container.EngineKubernetes:    "kubernetes",
		container.EngineSystemdNspawn: "systemd-nspawn",
	}
	for e, want := range cases {
		assert.Equal(t, want, e.String(), "engine=%d", e)
	}
}
