// Package deploy implements ADR-0085: on-box pull-build-and-atomic-deploy
// for the imzero2 headless demo. This is the logic layer (SD2); systemd
// supplies supervision and the poll timer.
//
// Phase 1 (this file) is the happy path:
//
//	resolve tag -> checkout -> build -> stage -> ws_probe gate -> atomic
//	symlink swap -> restart
//
// Rollback/retention (SD6) and the env-registry / runinfo / audited-bus
// wiring (SD7-8) are later phases; the seams are left explicit.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config is a deploy run's configuration. Phase 1 takes it from cli flags;
// ADR-0085 SD7 moves these knobs into the imzero2env registry in Phase 3.
type Config struct {
	Remote       string        // git remote name in the workspace clone (read-only)
	Workspace    string        // persistent clone + cargo/go caches; builds run here
	ReleasesDir  string        // immutable release snapshots: <ReleasesDir>/<tag>/
	CurrentLink  string        // `current` symlink the systemd unit ExecStarts through
	ServiceName  string        // systemd unit restarted on swap
	ScratchPort  int           // gate: candidate carrier loopback port
	GateAUs      int           // gate: access units ws_probe must receive
	GateTimeout  time.Duration // gate: overall budget
	LivePort     int           // post-restart health probe: the demo service's listen port
	KeepReleases int           // retain the last K release dirs (rollback history)
	// RequireSignedTags gates building on a valid GPG/SSH tag signature (SD8);
	// false is a loopback/dev escape only.
	RequireSignedTags bool
	EncoderArgs       string // IMZERO2_HEADLESS_ENCODER_ARGS for the gate run
	MainFont     string        // optional; fc-match'd if empty
	PhosphorFont string        // optional; the release's bundled asset if empty
	FallbackFont string        // optional; fc-match'd if empty
	DryRun       bool          // stage + gate but skip the swap + restart
}

// Build-artifact locations relative to the workspace clone root.
const (
	rustDirRel   = "rust/imzero2"
	mainGoRel    = "rust/imzero2/main_go"
	clientBinRel = "rust/imzero2/target/headless/release/imzero2"
	wsProbeRel   = "rust/imzero2/target/headless/release/imzero2_ws_probe" // Cargo bin target name (file is ws_probe.rs)
	assetsRel    = "rust/imzero2/assets"
	phosphorRel  = "assets/fonts/phosphor/Phosphor.ttf" // within a release dir
)

// Run executes the Phase 1 happy path. deployed reports whether a new tag
// was cut over (false when already current, or no release tag was found).
func Run(ctx context.Context, lg zerolog.Logger, cfg Config) (deployed bool, err error) {
	newest, current, err := resolveTags(ctx, lg, cfg)
	if err != nil {
		return false, err
	}
	if newest == "" {
		lg.Info().Msg("deploy: no release tag found; nothing to do")
		return false, nil
	}
	if newest == current {
		lg.Info().Str("tag", current).Msg("deploy: already current; nothing to do")
		return false, nil
	}
	lg.Info().Str("from", orNone(current)).Str("to", newest).Msg("deploy: new release tag")

	if err = verifyTag(ctx, lg, cfg, newest); err != nil {
		return false, err
	}
	if err = checkout(ctx, lg, cfg, newest); err != nil {
		return false, err
	}
	commit, clean, hErr := headInfo(ctx, cfg.Workspace) // SD7: deployed-revision agreement
	if hErr != nil {
		lg.Warn().Err(hErr).Msg("deploy: could not resolve HEAD (deployed-revision agreement unverified)")
	} else {
		lg.Info().Str("tag", newest).Str("commit", short(commit)).Bool("clean", clean).
			Msg("deploy: building revision (the demo's runinfo will report this commit)")
		if !clean {
			lg.Warn().Str("tag", newest).Msg("deploy: workspace not clean after checkout — built vcs_revision will be marked modified")
		}
	}
	if err = build(ctx, lg, cfg); err != nil {
		return false, err
	}
	relDir, err := stage(ctx, lg, cfg, newest)
	if err != nil {
		return false, err
	}
	if err = gate(ctx, lg, cfg, relDir); err != nil {
		// `current` is untouched; the candidate stays on disk for inspection.
		return false, eh.Errorf("deploy: gate failed for %s (current untouched): %w", newest, err)
	}
	if cfg.DryRun {
		lg.Warn().Str("tag", newest).Str("release", relDir).Msg("deploy: dry-run — built + gated OK, skipping swap + restart")
		return true, nil
	}
	prev := currentTarget(cfg) // for rollback; "" on the first deploy
	if err = swap(ctx, lg, cfg, relDir); err != nil {
		return false, err
	}
	if actErr := activate(ctx, lg, cfg, relDir); actErr != nil {
		lg.Error().Err(actErr).Str("tag", newest).Msg("deploy: activation failed — rolling back")
		if prev == "" {
			return false, eh.Errorf("deploy: %s failed to activate and no previous release exists: %w", newest, actErr)
		}
		if rbErr := rollback(ctx, lg, cfg, prev); rbErr != nil {
			return false, eb.Build().Str("rollback_error", rbErr.Error()).
				Errorf("deploy: %s failed to activate AND rollback to %s failed: %w", newest, filepath.Base(prev), actErr)
		}
		prune(lg, cfg)
		return false, eh.Errorf("deploy: %s rolled back to %s after activation failure: %w", newest, filepath.Base(prev), actErr)
	}
	prune(lg, cfg)
	lg.Info().Str("tag", newest).Str("commit", short(commit)).Msg("deploy: live")
	return true, nil
}

// --- steps ---

func resolveTags(ctx context.Context, lg zerolog.Logger, cfg Config) (newest, current string, err error) {
	if err = step(ctx, lg, "fetch", cfg.Workspace, nil, "git", "fetch", "--tags", "--prune", cfg.Remote); err != nil {
		return "", "", err
	}
	out, e := run(ctx, cfg.Workspace, nil, "git", "tag", "--list")
	if e != nil {
		return "", "", eb.Build().Str("output", tail(out, 1000)).Errorf("deploy: git tag list: %w", e)
	}
	current = currentTag(cfg)
	if n, ok := selectNewestTag(strings.Fields(out)); ok {
		newest = n
	}
	return newest, current, nil
}

func checkout(ctx context.Context, lg zerolog.Logger, cfg Config, tag string) error {
	return step(ctx, lg, "checkout", cfg.Workspace, nil, "git", "checkout", "--force", "--detach", tag)
}

// verifyTag enforces SD8: the tag must carry a valid GPG/SSH signature from a
// trusted key (git verify-tag) before the box builds it, so a compromised
// mirror or a forged ref cannot make the box build arbitrary code. Disabling
// the check is a loopback/dev escape only — it is loud about it.
func verifyTag(ctx context.Context, lg zerolog.Logger, cfg Config, tag string) error {
	if !cfg.RequireSignedTags {
		lg.Warn().Str("tag", tag).Msg("deploy: signed-tag verification DISABLED — dev/loopback only, do NOT use on an internet-exposed box")
		return nil
	}
	out, err := run(ctx, cfg.Workspace, nil, "git", "verify-tag", tag)
	if err != nil {
		return eb.Build().Str("tag", tag).Str("output", tail(out, 1500)).
			Errorf("deploy: tag %s has no valid signature from a trusted key — refusing to build: %w", tag, err)
	}
	lg.Info().Str("tag", tag).Msg("deploy: tag signature verified")
	return nil
}

func build(ctx context.Context, lg zerolog.Logger, cfg Config) error {
	rustDir := filepath.Join(cfg.Workspace, rustDirRel)
	// The headless Rust client (+ assets), via the project's own script.
	if err := step(ctx, lg, "build-rust", rustDir, nil, "bash", "build_rust_headless.sh"); err != nil {
		return err
	}
	// ws_probe (the gate client) shares the headless target-dir.
	if err := step(ctx, lg, "build-ws_probe", rustDir, nil,
		"cargo", "build", "--release", "--no-default-features", "--features", "headless",
		"--bin", "imzero2_ws_probe", "--target-dir", "target/headless"); err != nil {
		return err
	}
	// The Go launcher (carries this very `deploy` subcommand for the next run).
	return step(ctx, lg, "build-go", rustDir, nil, "bash", "build_go.sh")
}

func stage(ctx context.Context, lg zerolog.Logger, cfg Config, tag string) (string, error) {
	relDir := filepath.Join(cfg.ReleasesDir, tag)
	tmp := relDir + ".staging"
	if err := os.RemoveAll(tmp); err != nil {
		return "", eh.Errorf("deploy: clear staging: %w", err)
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", eh.Errorf("deploy: mkdir staging: %w", err)
	}
	bins := []struct{ src, dst string }{
		{filepath.Join(cfg.Workspace, mainGoRel), "main_go"},
		{filepath.Join(cfg.Workspace, clientBinRel), "imzero2"},
		{filepath.Join(cfg.Workspace, wsProbeRel), "ws_probe"},
	}
	for _, b := range bins {
		if err := copyFile(b.src, filepath.Join(tmp, b.dst), 0o755); err != nil {
			return "", eh.Errorf("deploy: stage %s: %w", b.dst, err)
		}
	}
	if err := copyTree(filepath.Join(cfg.Workspace, assetsRel), filepath.Join(tmp, "assets")); err != nil {
		return "", eh.Errorf("deploy: stage assets: %w", err)
	}
	// Finalize: a release dir is never observed half-populated.
	if err := os.RemoveAll(relDir); err != nil {
		return "", eh.Errorf("deploy: clear release: %w", err)
	}
	if err := os.Rename(tmp, relDir); err != nil {
		return "", eh.Errorf("deploy: finalize release: %w", err)
	}
	relabelSELinux(ctx, lg, relDir) // exec under enforcing SELinux; backstop (see deploy/ansible/README.md)
	lg.Info().Str("release", relDir).Msg("deploy: staged")
	return relDir, nil
}

// relabelSELinux best-effort restores SELinux file contexts on a freshly
// staged release so the gate (and the live demo) can exec its binaries under
// enforcing policy. The durable mechanism is the persistent fcontext rule the
// provisioner installs (the releases tree -> bin_t) plus parent-dir label
// inheritance; this re-applies it per release and catches label drift. It is a
// no-op when SELinux is not enabled or restorecon is absent, and never fatal —
// a relabel hiccup must not strand an otherwise-good release. It works when the
// deploy runs in an unconfined service domain (which may relabel); a confined
// domain can lack the permission, in which case the fcontext rule + inheritance
// carry the label instead.
func relabelSELinux(ctx context.Context, lg zerolog.Logger, dir string) {
	if _, err := os.Stat("/sys/fs/selinux/enforce"); err != nil {
		return // SELinux not enabled on this box
	}
	bin, err := exec.LookPath("restorecon")
	if err != nil {
		lg.Warn().Str("dir", dir).Msg("deploy: SELinux enabled but restorecon not found (policycoreutils); relying on fcontext + inheritance")
		return
	}
	if out, rErr := run(ctx, "", nil, bin, "-RF", dir); rErr != nil {
		lg.Warn().Str("dir", dir).Str("output", tail(out, 500)).Err(rErr).
			Msg("deploy: SELinux relabel failed; if the demo will not exec, label the releases tree bin_t (deploy/ansible/README.md)")
		return
	}
	lg.Debug().Str("dir", dir).Msg("deploy: SELinux contexts restored on release")
}

// gate starts the candidate on a scratch loopback port (interactive
// carousel: no --launch, so no clickhouse-local is needed) and requires
// ws_probe to decode real access units before the swap is allowed.
func gate(ctx context.Context, lg zerolog.Logger, cfg Config, relDir string) error {
	gctx, cancel := context.WithTimeout(ctx, cfg.GateTimeout)
	defer cancel()

	// The candidate carrier binds the scratch port (ws) plus scratch+1 (its
	// viewer page); ws_probe then connects to the scratch port. If either is
	// already held — a carrier leaked by a crashed prior deploy, or a port
	// collision with the live service — the candidate can't own the port and
	// ws_probe could decode a STRANGER's stream, waving a broken release
	// through the gate. Fail fast rather than gate against an unknown listener.
	for _, p := range []int{cfg.ScratchPort, cfg.ScratchPort + 1} {
		if !portFree(p) {
			return eb.Build().Int("port", p).Errorf(
				"deploy: gate — scratch port %d already in use (stale carrier or collision with the live port); refusing to probe an unknown listener", p)
		}
	}

	args := []string{
		"--logLevel=warn", "imzero2", "demo",
		"--clientBinary", filepath.Join(relDir, "imzero2"),
		"--clientInitialMainWindowWidth", "1280",
		"--clientInitialMainWindowHeight", "800",
	}
	if f := resolveFont(cfg.MainFont, "Noto Sans"); f != "" {
		args = append(args, "--mainFontTTF", f)
	}
	phosphor := cfg.PhosphorFont
	if phosphor == "" {
		phosphor = filepath.Join(relDir, phosphorRel)
	}
	args = append(args, "--phosphorFontTTF", phosphor)
	if f := resolveFont(cfg.FallbackFont, "Noto Sans CJK JP"); f != "" {
		args = append(args, "--fallbackFontTTF", f)
	}

	cmd := exec.CommandContext(gctx, filepath.Join(relDir, "main_go"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own group → kill the whole tree
	cmd.Env = envWith(
		"IMZERO2_HEADLESS_LISTEN=127.0.0.1:"+strconv.Itoa(cfg.ScratchPort),
		"IMZERO2_HEADLESS_FPS=30",
		"IMZERO2_HEADLESS_ENCODER_ARGS="+cfg.EncoderArgs,
		"LIBGL_ALWAYS_SOFTWARE=1",
	)
	var buf lockedBuffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return eh.Errorf("deploy: gate start: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // the group: main_go + rust client + ffmpeg
		}
		_ = cmd.Wait()
	}()

	aus, kf, pout, perr := probeStream(gctx, filepath.Join(relDir, "ws_probe"), cfg.ScratchPort, cfg.GateAUs)
	if perr != nil {
		return eb.Build().Str("candidate", tail(buf.String(), 1500)).Str("probe", tail(pout, 600)).Errorf("deploy: gate — %w", perr)
	}
	if aus < cfg.GateAUs || kf < 1 {
		return eb.Build().Int("aus", aus).Int("want", cfg.GateAUs).Int("keyframes", kf).
			Errorf("deploy: gate — stream did not deliver the required frames")
	}
	lg.Info().Int("aus", aus).Int("keyframes", kf).Msg("deploy: gate passed")
	return nil
}

func swap(_ context.Context, lg zerolog.Logger, cfg Config, relDir string) error {
	if err := atomicSymlink(cfg.CurrentLink, relDir); err != nil {
		return err
	}
	lg.Info().Str("current", cfg.CurrentLink).Str("target", relDir).Msg("deploy: swapped current")
	return nil
}

// atomicSymlink points link at target via a temp symlink + rename, which is
// atomic over an existing link.
func atomicSymlink(link, target string) error {
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return eh.Errorf("deploy: prepare symlink: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		return eh.Errorf("deploy: atomic swap: %w", err)
	}
	return nil
}

func restart(ctx context.Context, lg zerolog.Logger, cfg Config) error {
	return step(ctx, lg, "restart", "", nil, "systemctl", "restart", cfg.ServiceName)
}

// --- Phase 2: activate / health re-probe / rollback / retention (SD6) ---

// activate restarts the service onto `current` and verifies it streams.
func activate(ctx context.Context, lg zerolog.Logger, cfg Config, relDir string) error {
	if err := restart(ctx, lg, cfg); err != nil {
		return err
	}
	return healthCheck(ctx, lg, cfg, relDir)
}

// healthCheck probes the live demo service after a restart (SD6): the
// release just made current must actually serve and stream.
func healthCheck(ctx context.Context, lg zerolog.Logger, cfg Config, relDir string) error {
	hctx, cancel := context.WithTimeout(ctx, cfg.GateTimeout)
	defer cancel()
	aus, kf, pout, err := probeStream(hctx, filepath.Join(relDir, "ws_probe"), cfg.LivePort, cfg.GateAUs)
	if err != nil {
		return eb.Build().Str("probe", tail(pout, 600)).Errorf("deploy: health — %w", err)
	}
	if aus < cfg.GateAUs || kf < 1 {
		return eb.Build().Int("aus", aus).Int("keyframes", kf).Errorf("deploy: health — live service did not stream after restart")
	}
	lg.Info().Int("aus", aus).Int("keyframes", kf).Msg("deploy: post-restart health probe passed")
	return nil
}

// rollback repoints `current` at a previous release and restarts (SD6) —
// instant, no rebuild.
func rollback(ctx context.Context, lg zerolog.Logger, cfg Config, prevRelDir string) error {
	if err := atomicSymlink(cfg.CurrentLink, prevRelDir); err != nil {
		return err
	}
	lg.Warn().Str("current", cfg.CurrentLink).Str("target", prevRelDir).Msg("deploy: rolled back current")
	return restart(ctx, lg, cfg)
}

// probeStream waits for the carrier page on port, then requires ws_probe to
// decode wantAUs access units. Returns the AU/keyframe counts and the raw
// probe output for diagnostics. Shared by the pre-swap gate and the
// post-restart health check.
func probeStream(ctx context.Context, wsProbeBin string, port, wantAUs int) (aus, keyframes int, out string, err error) {
	if err = waitForPage(ctx, fmt.Sprintf("http://127.0.0.1:%d/", port)); err != nil {
		return 0, 0, "", eh.Errorf("carrier did not come up: %w", err)
	}
	outFile := filepath.Join(os.TempDir(), fmt.Sprintf("imzero2-probe-%d.h264", port))
	out, perr := run(ctx, "", nil, wsProbeBin, fmt.Sprintf("ws://127.0.0.1:%d/", port), outFile, strconv.Itoa(wantAUs))
	_ = os.Remove(outFile)
	aus, keyframes = parseProbe(out)
	if perr != nil {
		return aus, keyframes, out, eh.Errorf("ws_probe: %w", perr)
	}
	return aus, keyframes, out, nil
}

// currentTarget resolves the absolute, cleaned path `current` points at, or "".
func currentTarget(cfg Config) string {
	t, err := os.Readlink(cfg.CurrentLink)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(filepath.Dir(cfg.CurrentLink), t)
	}
	return filepath.Clean(t)
}

type relEntry struct {
	path  string
	mtime time.Time
}

// selectPrune returns the release dirs to delete: everything beyond the
// `keep` newest by mtime, never including `current`. Pure, for testability.
func selectPrune(rels []relEntry, current string, keep int) []string {
	if keep < 1 {
		keep = 1
	}
	sorted := append([]relEntry(nil), rels...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].mtime.After(sorted[j].mtime) })
	var del []string
	kept := 0
	for _, r := range sorted {
		if r.path == current {
			continue // never prune the live release
		}
		if kept < keep {
			kept++
			continue
		}
		del = append(del, r.path)
	}
	return del
}

// prune trims releases/ to the retention window (SD6), protecting `current`.
func prune(lg zerolog.Logger, cfg Config) {
	entries, err := os.ReadDir(cfg.ReleasesDir)
	if err != nil {
		lg.Warn().Err(err).Msg("deploy: prune skipped")
		return
	}
	cur := currentTarget(cfg)
	var rels []relEntry
	for _, e := range entries {
		if !e.IsDir() || strings.HasSuffix(e.Name(), ".staging") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		rels = append(rels, relEntry{filepath.Join(cfg.ReleasesDir, e.Name()), info.ModTime()})
	}
	for _, p := range selectPrune(rels, cur, cfg.KeepReleases) {
		if rerr := os.RemoveAll(p); rerr != nil {
			lg.Warn().Str("release", p).Err(rerr).Msg("deploy: prune failed")
		} else {
			lg.Info().Str("release", p).Msg("deploy: pruned old release")
		}
	}
}

// --- tag selection (pure; unit-tested) ---

var releaseTagRe = regexp.MustCompile(`^v?\d+(\.\d+)*$`)

// selectNewestTag returns the highest semver-ish release tag (`v?N(.N)*`),
// ignoring non-release tags; comparison is numeric and component-wise.
func selectNewestTag(tags []string) (string, bool) {
	type cand struct {
		tag string
		ver []int
	}
	var cands []cand
	for _, t := range tags {
		if ver, ok := parseReleaseTag(t); ok {
			cands = append(cands, cand{t, ver})
		}
	}
	if len(cands) == 0 {
		return "", false
	}
	sort.SliceStable(cands, func(i, j int) bool { return compareVer(cands[i].ver, cands[j].ver) > 0 })
	return cands[0].tag, true
}

func parseReleaseTag(t string) ([]int, bool) {
	if !releaseTagRe.MatchString(t) {
		return nil, false
	}
	parts := strings.Split(strings.TrimPrefix(t, "v"), ".")
	ver := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		ver = append(ver, n)
	}
	return ver, true
}

func compareVer(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// --- helpers ---

func currentTag(cfg Config) string {
	if t := currentTarget(cfg); t != "" {
		return filepath.Base(t)
	}
	return ""
}

func step(ctx context.Context, lg zerolog.Logger, what, dir string, env []string, name string, args ...string) error {
	lg.Info().Str("step", what).Str("cmd", name+" "+strings.Join(args, " ")).Msg("deploy: step")
	out, err := run(ctx, dir, env, name, args...)
	if err != nil {
		return eb.Build().Str("step", what).Str("output", tail(out, 2000)).Errorf("deploy: step %q failed: %w", what, err)
	}
	lg.Debug().Str("step", what).Msg("deploy: step ok")
	return nil
}

// portFree reports whether a loopback TCP port can be bound right now. The gate
// uses it to refuse probing a scratch port something else already holds (see the
// caller) — a held port means the candidate isn't the listener ws_probe reaches.
func portFree(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// envWith returns os.Environ() with the given KEY=VALUE entries, replacing any
// inherited entry for those keys rather than appending duplicates. getenv
// returns the FIRST match for a duplicate key, so a plain append would let an
// inherited IMZERO2_HEADLESS_ENCODER_ARGS shadow the encoder the gate intends.
func envWith(kv ...string) []string {
	override := make(map[string]bool, len(kv))
	for _, e := range kv {
		if i := strings.IndexByte(e, '='); i > 0 {
			override[e[:i]] = true
		}
	}
	out := make([]string, 0, len(kv)+8)
	for _, e := range os.Environ() {
		if i := strings.IndexByte(e, '='); i > 0 && override[e[:i]] {
			continue
		}
		out = append(out, e)
	}
	return append(out, kv...)
}

func run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func waitForPage(ctx context.Context, url string) error {
	cl := &http.Client{Timeout: 2 * time.Second}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if resp, err := cl.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		t := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

var probeRe = regexp.MustCompile(`probe done:\s*(\d+)\s+AUs,\s*\d+\s+bytes,\s*(\d+)\s+keyframes`)

func parseProbe(s string) (aus, keyframes int) {
	if m := probeRe.FindStringSubmatch(s); m != nil {
		aus, _ = strconv.Atoi(m[1])
		keyframes, _ = strconv.Atoi(m[2])
	}
	return aus, keyframes
}

func resolveFont(override, family string) string {
	if override != "" {
		return override
	}
	out, err := exec.Command("fc-match", "-f", "%{file}", family).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// headInfo returns the workspace's resolved HEAD commit and whether the tree
// is clean. The Go build stamps this commit as the binary's vcs revision, so
// the running demo's runinfo will report it — ADR-0085 SD7's deployed-revision
// agreement, by construction.
func headInfo(ctx context.Context, workspace string) (commit string, clean bool, err error) {
	rev, e := run(ctx, workspace, nil, "git", "rev-parse", "HEAD")
	if e != nil {
		return "", false, eb.Build().Str("output", tail(rev, 400)).Errorf("deploy: rev-parse HEAD: %w", e)
	}
	commit = strings.TrimSpace(rev)
	st, e := run(ctx, workspace, nil, "git", "status", "--porcelain")
	if e != nil {
		return commit, false, eh.Errorf("deploy: git status: %w", e)
	}
	return commit, strings.TrimSpace(st) == "", nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// lockedBuffer is a concurrency-safe sink for a child's merged stdout+stderr.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
