package notebookview_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// The notebook reader finds cells with a line scan; the markdown widget and
// the block editor find blocks with the parse. A notebook is edited by the
// second and read by the first, so the two have to agree on where a fence
// starts. This walks every markdown document in the repository and reports
// each fence one of them sees at the top level in column 0 and the other
// does not.
func TestScannerAgreesWithTheParseOnTheRepositorysDocuments(t *testing.T) {
	root, err := filepath.Abs("../../../../../..")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "the repository root")

	var files, fences, onlyScan, onlyParse, indentedTop int
	var examples []string
	note := func(kind, path string, src []byte, at int) {
		if len(examples) < 25 {
			line := 1 + strings.Count(string(src[:at]), "\n")
			rel, _ := filepath.Rel(root, path)
			examples = append(examples, kind+" "+rel+":"+itoa(line))
		}
	}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "target", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		src, rErr := os.ReadFile(path)
		if rErr != nil || len(src) > 1<<20 {
			return nil
		}
		nb, rdErr := mdnotebook.Read(src)
		if rdErr != nil {
			return nil
		}
		files++
		scan := make(map[int]bool)
		for _, at := range nb.Fences {
			scan[at] = true
		}
		parse := make(map[int]bool)
		for _, b := range markdown.ParseWith(src, markdown.ParseOptions{}).Blocks() {
			if b.Kind != markdown.BlockCodeBlock || b.Synthetic || b.Start >= len(src) {
				continue
			}
			switch src[b.Start] {
			case '`', '~':
				parse[b.Start] = true
			case ' ':
				if t := strings.TrimLeft(string(src[b.Start:min(b.Start+8, len(src))]), " "); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
					indentedTop++
				}
			}
		}
		for at := range scan {
			fences++
			if !parse[at] {
				onlyScan++
				note("scan-only", path, src, at)
			}
		}
		for at := range parse {
			if !scan[at] {
				onlyParse++
				note("parse-only", path, src, at)
			}
		}
		return nil
	})
	require.NoError(t, walkErr)
	t.Logf("documents %d · column-0 fences %d · seen by the scan alone %d · by the parse alone %d · top-level fences indented 1–3 spaces %d",
		files, fences, onlyScan, onlyParse, indentedTop)
	for _, e := range examples {
		t.Log(e)
	}
	require.Zero(t, onlyScan+onlyParse, "the scan and the parse disagree on where a top-level fence starts")
}

func itoa(n int) string {
	var b []byte
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
