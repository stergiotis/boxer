package chlocalpool

import (
	"os"
	"time"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Defaults for Config fields per ADR-0028 §SD3.
const (
	// DefaultBinaryPath names the multi-call `clickhouse` binary; extbin adds
	// the `local` subcommand. The packaged install puts it here.
	DefaultBinaryPath                        = "/usr/bin/clickhouse"
	DefaultMinIdle             uint8         = 2
	DefaultMaxConcurrent       uint8         = 8
	DefaultSpawnConcurrency    uint8         = 2
	DefaultMaxMemoryPerWorker  uint64        = 1 << 30
	DefaultSpawnTimeout        time.Duration = 2 * time.Second
	DefaultWatchdogMaxLifetime time.Duration = 60 * time.Second
	DefaultKillGrace           time.Duration = 250 * time.Millisecond
	DefaultStderrCapBytes      uint32        = 4 << 10
)

// Config parameterises a Pool. Zero-valued fields are filled from
// the Default* constants by withDefaults; a caller can pass Config{}
// to accept all defaults.
type Config struct {
	// BinaryPath names the multi-call `clickhouse` binary. Empty resolves at
	// New: DefaultBinaryPath when the packaged install put it there, else what
	// the extbin declaration resolves — the BOXER_CLICKHOUSE_LOCAL override,
	// then a `clickhouse` on PATH. A path set here is taken as written, and
	// a missing one is a startup error rather than a fallback, since an
	// operator who names a binary means that binary.
	BinaryPath          string
	BaseTmpDir          string
	MinIdle             uint8
	MaxConcurrent       uint8
	SpawnConcurrency    uint8
	MaxMemoryPerWorker  uint64
	SpawnTimeout        time.Duration
	WatchdogMaxLifetime time.Duration
	KillGrace           time.Duration
	StderrCapBytes      uint32
}

func (inst Config) withDefaults() (cfg Config) {
	cfg = inst
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = DefaultBinaryPath
	}
	if cfg.MinIdle == 0 {
		cfg.MinIdle = DefaultMinIdle
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = DefaultMaxConcurrent
	}
	if cfg.SpawnConcurrency == 0 {
		cfg.SpawnConcurrency = DefaultSpawnConcurrency
	}
	if cfg.MaxMemoryPerWorker == 0 {
		cfg.MaxMemoryPerWorker = DefaultMaxMemoryPerWorker
	}
	if cfg.SpawnTimeout <= 0 {
		cfg.SpawnTimeout = DefaultSpawnTimeout
	}
	if cfg.WatchdogMaxLifetime <= 0 {
		cfg.WatchdogMaxLifetime = DefaultWatchdogMaxLifetime
	}
	if cfg.KillGrace <= 0 {
		cfg.KillGrace = DefaultKillGrace
	}
	if cfg.StderrCapBytes == 0 {
		cfg.StderrCapBytes = DefaultStderrCapBytes
	}
	return
}

func (inst Config) validate() (err error) {
	if inst.MinIdle > inst.MaxConcurrent {
		err = eb.Build().Uint8("minIdle", inst.MinIdle).Uint8("maxConcurrent", inst.MaxConcurrent).Errorf("chlocalpool: MinIdle exceeds MaxConcurrent")
		return
	}
	if inst.SpawnConcurrency > inst.MaxConcurrent {
		err = eb.Build().Uint8("spawnConcurrency", inst.SpawnConcurrency).Uint8("maxConcurrent", inst.MaxConcurrent).Errorf("chlocalpool: SpawnConcurrency exceeds MaxConcurrent")
		return
	}
	return
}

// resolveBinaryPath is what an unset Config.BinaryPath means: the packaged
// install's DefaultBinaryPath when it exists, else whatever the extbin
// declaration resolves on this host — the BOXER_CLICKHOUSE_LOCAL override,
// then a `clickhouse` on PATH, which is where a single-binary install lands
// (ADR-0028's 2026-09-13 update). With neither it returns the default path,
// so the startup error names where the packaged install would have put it.
func resolveBinaryPath() string {
	if _, err := os.Stat(DefaultBinaryPath); err == nil {
		return DefaultBinaryPath
	}
	if bin, ok := extbin.ClickHouseLocal.Resolve(); ok && bin != "" {
		return bin
	}
	return DefaultBinaryPath
}

// LookupBinary resolves the `clickhouse` binary the way New does and
// reports whether it exists, so a test that needs a live engine skips on
// the same answer the pool would fail on — not on the packaged path alone.
func LookupBinary() (path string, err error) {
	path = resolveBinaryPath()
	if _, err = os.Stat(path); err != nil {
		return "", eb.Build().Str("binaryPath", path).Errorf("chlocalpool: binary: %w", err)
	}
	return path, nil
}
