package tabhost

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// hostSumText is the browser host this tree builds, as browserhost.sum
// records it (ADR-0278 SD5, proposed).
//
//go:embed browserhost.sum
var hostSumText string

// hostSumFile is browserhost.sum's path inside boxer's module.
const hostSumFile = "public/thestack/imzero2/browserhost/tabhost/browserhost.sum"

// hostSum is one browserhost.sum: the host's SHA-256 in hex and the IDL
// fingerprint it was generated for.
type hostSum struct {
	sha256 string
	idl    uint64
}

// parseHostSum reads the `sha256 <hex>` and `idl <hex>` lines; comments and
// blank lines are skipped.
func parseHostSum(text string) (hs hostSum, err error) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "sha256":
			hs.sha256 = strings.ToLower(f[1])
		case "idl":
			if hs.idl, err = strconv.ParseUint(f[1], 16, 64); err != nil {
				return hostSum{}, eb.Build().Str("idl", f[1]).Errorf("browserhost.sum: idl: %w", err)
			}
		}
	}
	if len(hs.sha256) != 64 {
		return hostSum{}, eb.Build().Str("sha256", hs.sha256).Errorf("browserhost.sum: no 64-digit sha256 line")
	}
	return
}

// formatHostSum is browserhost.sum's text for hs: the header comment kept
// from the recorded file, the two lines replaced.
func formatHostSum(hs hostSum) (text string) {
	var b strings.Builder
	for _, line := range strings.Split(hostSumText, "\n") {
		if strings.HasPrefix(line, "#") {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("sha256 " + hs.sha256 + "\n")
	b.WriteString(fmt.Sprintf("idl %016x\n", hs.idl))
	return b.String()
}

// recordedHostSum is the embedded browserhost.sum, parsed.
func recordedHostSum() (hs hostSum, err error) {
	return parseHostSum(hostSumText)
}

func hostDigestCommand() (cmd *cli.Command) {
	return &cli.Command{
		Name:  "hostdigest",
		Usage: "print the digest and IDL fingerprint of the browser host this tree builds; --write records them in browserhost.sum (inside boxer)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "host", Usage: "an already built imzero2_browser.wasm; empty builds it from source"},
			&cli.BoolFlag{Name: "write", Usage: "write browserhost.sum in the boxer checkout this runs in"},
		},
		Action: hostDigest,
	}
}

func hostDigest(ctx *cli.Context) (err error) {
	boxerDir, err := moduleDir(boxerModule)
	if err != nil {
		return
	}
	path := ctx.String("host")
	if path == "" {
		if path, err = buildHost(boxerDir, boxerDir); err != nil {
			return
		}
	}
	hs := hostSum{idl: c.IdlFingerprint}
	if hs.sha256, err = fileSha256(path); err != nil {
		return eb.Build().Str("host", path).Errorf("hostdigest: %w", err)
	}
	fmt.Printf("sha256 %s\nidl %016x\n", hs.sha256, hs.idl)
	if !ctx.Bool("write") {
		return
	}
	if inCache, cErr := inModuleCache(boxerDir); cErr != nil || inCache {
		return eh.Errorf("hostdigest: --write needs boxer as a checkout; this module reads it from the module cache, which only ever reads browserhost.sum")
	}
	dst := filepath.Join(boxerDir, filepath.FromSlash(hostSumFile))
	if err = os.WriteFile(dst, []byte(formatHostSum(hs)), 0o644); err != nil {
		return eb.Build().Str("file", dst).Errorf("hostdigest: %w", err)
	}
	log.Info().Str("file", dst).Msg("hostdigest: written")
	return
}

// inModuleCache reports whether dir lies in the Go module cache, where a
// dependency's files are read-only.
func inModuleCache(dir string) (yes bool, err error) {
	o, err := extbin.Go.Output(context.Background(), extbin.Opts{}, "env", "GOMODCACHE")
	if err != nil {
		return false, eh.Errorf("go env GOMODCACHE: %w", err)
	}
	cache := filepath.Clean(strings.TrimSpace(string(o)))
	rel, rErr := filepath.Rel(cache, filepath.Clean(dir))
	return rErr == nil && !strings.HasPrefix(rel, ".."), nil
}
