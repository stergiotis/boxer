//go:build llm_generated_opus47

package chlocalpool

import (
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Defaults for Config fields per ADR-0028 §SD3.
const (
	DefaultBinaryPath          = "/usr/bin/clickhouse-local"
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
		err = eh.Errorf("chlocalpool: MinIdle (%d) exceeds MaxConcurrent (%d)", inst.MinIdle, inst.MaxConcurrent)
		return
	}
	if inst.SpawnConcurrency > inst.MaxConcurrent {
		err = eh.Errorf("chlocalpool: SpawnConcurrency (%d) exceeds MaxConcurrent (%d)", inst.SpawnConcurrency, inst.MaxConcurrent)
		return
	}
	return
}
