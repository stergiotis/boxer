package demo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// listSchema is the Arrow schema for one row per registered app. Stable
// column order so `clickhouse-local` queries by position stay valid.
var listSchema = arrow.NewSchema([]arrow.Field{
	{Name: "id", Type: arrow.BinaryTypes.String},
	{Name: "subject_alias", Type: arrow.BinaryTypes.String},
	{Name: "legacy_code", Type: arrow.PrimitiveTypes.Uint64, Nullable: true},
	{Name: "display", Type: arrow.BinaryTypes.String},
	{Name: "title", Type: arrow.BinaryTypes.String},
	{Name: "category", Type: arrow.BinaryTypes.String},
	{Name: "surface", Type: arrow.BinaryTypes.String},
	{Name: "pref_w", Type: arrow.PrimitiveTypes.Uint16},
	{Name: "pref_h", Type: arrow.PrimitiveTypes.Uint16},
	{Name: "bg_tick_hz", Type: arrow.PrimitiveTypes.Uint8},
	{Name: "caps", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "persisted_keys", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "version", Type: arrow.BinaryTypes.String},
}, nil)

// codeForId is the inverse of legacyCodeToId — returns ok=false when the
// id has no numeric alias. Built once per call (small map), the registry
// is short so the cost is negligible.
func codeForId(id runtimeapp.AppIdT) (code uint64, ok bool) {
	for c, mapped := range legacyCodeToId {
		if mapped == id {
			code = c
			ok = true
			return
		}
	}
	return
}

// manifestsToArrowIPC serialises manifests as a single-record Arrow IPC
// stream. Manifests are sorted by Id — AllManifests() already returns
// sorted order; the explicit sort here keeps the helper self-contained
// so test callers passing arbitrary slices still get deterministic
// output.
func manifestsToArrowIPC(manifests []runtimeapp.Manifest) (buf []byte, err error) {
	sorted := make([]runtimeapp.Manifest, len(manifests))
	copy(sorted, manifests)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Id < sorted[j].Id
	})

	alloc := memory.NewGoAllocator()
	bldr := array.NewRecordBuilder(alloc, listSchema)
	defer bldr.Release()

	id := bldr.Field(0).(*array.StringBuilder)
	alias := bldr.Field(1).(*array.StringBuilder)
	legacy := bldr.Field(2).(*array.Uint64Builder)
	display := bldr.Field(3).(*array.StringBuilder)
	title := bldr.Field(4).(*array.StringBuilder)
	category := bldr.Field(5).(*array.StringBuilder)
	surface := bldr.Field(6).(*array.StringBuilder)
	prefW := bldr.Field(7).(*array.Uint16Builder)
	prefH := bldr.Field(8).(*array.Uint16Builder)
	bgTick := bldr.Field(9).(*array.Uint8Builder)
	caps := bldr.Field(10).(*array.Uint32Builder)
	persisted := bldr.Field(11).(*array.Uint32Builder)
	version := bldr.Field(12).(*array.StringBuilder)

	for _, m := range sorted {
		id.Append(string(m.Id))
		alias.Append(m.Id.SubjectAlias())
		if code, ok := codeForId(m.Id); ok {
			legacy.Append(code)
		} else {
			legacy.AppendNull()
		}
		display.Append(m.Display)
		title.Append(m.WindowTitle())
		category.Append(m.Category)
		surface.Append(m.Surface.String())
		prefW.Append(m.SurfaceHints.PreferredWidth)
		prefH.Append(m.SurfaceHints.PreferredHeight)
		bgTick.Append(m.BackgroundTickHz)
		caps.Append(uint32(len(m.Caps)))
		persisted.Append(uint32(len(m.PersistedKeys)))
		version.Append(m.Version)
	}

	rec := bldr.NewRecord()
	defer rec.Release()

	var out bytes.Buffer
	w := ipc.NewWriter(&out, ipc.WithSchema(listSchema), ipc.WithAllocator(alloc))
	err = w.Write(rec)
	if err != nil {
		err = eh.Errorf("manifest arrow: write record: %w", err)
		return
	}
	err = w.Close()
	if err != nil {
		err = eh.Errorf("manifest arrow: close: %w", err)
		return
	}
	buf = out.Bytes()
	return
}

// renderManifestList writes the manifest set to stdout, pretty-printed via
// clickhouse-local when present, otherwise a plain ASCII table fallback. If
// outputPath is non-empty the Arrow IPC bytes are also written there so
// downstream tooling can re-query the file. format is the
// clickhouse-local --output-format name (default "PrettyCompact").
func renderManifestList(manifests []runtimeapp.Manifest, outputPath, format string, stdout io.Writer) (err error) {
	arrowBytes, marshalErr := manifestsToArrowIPC(manifests)
	if marshalErr != nil {
		err = eh.Errorf("manifest list: %w", marshalErr)
		return
	}
	if outputPath != "" {
		err = os.WriteFile(outputPath, arrowBytes, 0o644)
		if err != nil {
			err = eb.Build().Str("path", outputPath).Errorf("manifest list: write arrow file: %w", err)
			return
		}
	}
	ok, runErr := runChLocalQuery(arrowBytes, "SELECT * FROM table", format, stdout)
	if runErr != nil {
		err = eh.Errorf("manifest list: %w", runErr)
		return
	}
	if !ok {
		// clickhouse-local unreachable: drop to the ASCII fallback so the
		// command still produces useful output in minimal environments.
		err = renderManifestsAscii(manifests, stdout)
	}
	return
}

// runChLocalQuery pipes Arrow IPC bytes to clickhouse-local on stdin and
// streams its result to stdout. ok=false signals the binary is unreachable
// (neither at chlocalpool.DefaultBinaryPath nor on $PATH); callers decide
// whether to error or fall back. format is the --output-format name (e.g.
// "TabSeparated", "PrettyCompact"); empty string defaults to PrettyCompact.
// On a non-zero exit the clickhouse-local stderr and the executed query
// are folded into the returned error for diagnostic visibility.
func runChLocalQuery(arrowBytes []byte, query, format string, stdout io.Writer) (ok bool, err error) {
	if format == "" {
		format = "PrettyCompact"
	}
	// Prefer the bundled path; leave it empty to let extbin resolve on PATH.
	chPath := ""
	if _, statErr := os.Stat(chlocalpool.DefaultBinaryPath); statErr == nil {
		chPath = chlocalpool.DefaultBinaryPath
	}
	cmd, err := extbin.ClickHouseLocal.Command(context.Background(),
		extbin.Opts{Path: chPath},
		"--input-format", "ArrowStream",
		"--output-format", format,
		"--query", query,
	)
	if err != nil {
		// Unreachable (not at the bundled path, not on PATH): signal ok=false
		// with no error so callers fall back to the ASCII renderer.
		return false, nil
	}
	ok = true
	cmd.Stdin = bytes.NewReader(arrowBytes)
	cmd.Stdout = stdout
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	runErr := cmd.Run()
	if runErr != nil {
		err = eb.Build().Str("stderr", stderrBuf.String()).Str("bin", cmd.Path).
			Str("query", query).Errorf("clickhouse-local: %w", runErr)
		return
	}
	return
}

// renderManifestsAscii is the no-CH fallback. Plain text, one row per
// manifest, columns separated by two spaces with per-column padding.
// Mirrors the columns most useful for a human eye: legacy code, alias,
// surface, category, display, and id.
func renderManifestsAscii(manifests []runtimeapp.Manifest, w io.Writer) (err error) {
	sorted := make([]runtimeapp.Manifest, len(manifests))
	copy(sorted, manifests)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Id < sorted[j].Id
	})
	headers := []string{"code", "alias", "surface", "category", "display", "id"}
	rows := make([][]string, 0, len(sorted))
	for _, m := range sorted {
		code := "-"
		if c, ok := codeForId(m.Id); ok {
			code = "a" + strconv.FormatUint(c, 10)
		}
		rows = append(rows, []string{
			code,
			m.Id.SubjectAlias(),
			m.Surface.String(),
			m.Category,
			m.Display,
			string(m.Id),
		})
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i, v := range r {
			if len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
	}
	writeRow := func(cells []string) {
		for i, v := range cells {
			pad := widths[i] - len(v)
			if i > 0 {
				_, _ = io.WriteString(w, "  ")
			}
			_, _ = io.WriteString(w, v)
			if pad > 0 && i != len(cells)-1 {
				_, _ = io.WriteString(w, strings.Repeat(" ", pad))
			}
		}
		_, _ = io.WriteString(w, "\n")
	}
	writeRow(headers)
	sepCells := make([]string, len(headers))
	for i := range sepCells {
		sepCells[i] = strings.Repeat("-", widths[i])
	}
	writeRow(sepCells)
	for _, r := range rows {
		writeRow(r)
	}
	_, _ = fmt.Fprintf(w, "\n%d application(s) registered\n", len(sorted))
	return
}
