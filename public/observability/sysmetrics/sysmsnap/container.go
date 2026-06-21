package sysmsnap

// EngineE classifies the detected container runtime.
type EngineE uint8

const (
	// EngineNone — not running inside a container (default zero value).
	EngineNone EngineE = iota
	// EngineUnknown — a marker indicating containerization was found,
	// but the runtime could not be classified.
	EngineUnknown
	EngineDocker
	EnginePodman
	EngineLXC
	EngineKubernetes
	EngineSystemdNspawn
)

func (e EngineE) String() (out string) {
	switch e {
	case EngineDocker:
		return "docker"
	case EnginePodman:
		return "podman"
	case EngineLXC:
		return "lxc"
	case EngineKubernetes:
		return "kubernetes"
	case EngineSystemdNspawn:
		return "systemd-nspawn"
	case EngineUnknown:
		return "unknown"
	default:
		return "none"
	}
}

// AllEngines lists every defined [EngineE] value.
var AllEngines = []EngineE{
	EngineNone, EngineUnknown, EngineDocker, EnginePodman,
	EngineLXC, EngineKubernetes, EngineSystemdNspawn,
}

// ContainerInfo is the result of a container detection.
type ContainerInfo struct {
	Engine EngineE

	// Detail holds runtime-specific metadata when available — the
	// content of /run/systemd/container for nspawn-class, or the
	// matched cgroup path substring for cgroup-based detection. Empty
	// otherwise.
	Detail string
}
