package cli

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Source of truth: https: //github.com/ClickHouse/ClickHouse/blob/master/src/Parsers/IAST.cpp#L27
const (
	hiliteKeyword      = "\033[1m"
	hiliteIdentifier   = "\033[0;36m"
	hiliteFunction     = "\033[0;33m"
	hiliteOperator     = "\033[1;33m"
	hiliteAlias        = "\033[0;32m"
	hiliteSubstitution = "\033[1;36m"
	hiliteNone         = "\033[0m"
)

func debugMarkupHiliteOutput(hilitedOutput string) (marked string) {
	// LEFT-POINTING ANGLE BRACKET (U+2329, Ps): 〈
	// RIGHT-POINTING ANGLE BRACKET (U+232A, Pe): 〉
	marked = hilitedOutput
	marked = strings.ReplaceAll(marked, hiliteKeyword, "〈keyword ")
	marked = strings.ReplaceAll(marked, hiliteIdentifier, "〈identifier ")
	marked = strings.ReplaceAll(marked, hiliteFunction, "〈function ")
	marked = strings.ReplaceAll(marked, hiliteOperator, "〈operator ")
	marked = strings.ReplaceAll(marked, hiliteAlias, "〈alias ")
	marked = strings.ReplaceAll(marked, hiliteSubstitution, "〈substitution ")
	marked = strings.ReplaceAll(marked, hiliteNone, "〉")
	return
}

type Formater struct {
	opts FormaterOptions
	args []string
	buf       *bytes.Buffer
	stderrBuf *bytes.Buffer
}
type FormaterOptions struct {
	BinaryPath      string
	Hilite          bool
	KeepComments    bool
	MaxLineLength   uint8
	AllowMultiQuery bool
	Seed            string
	MaxParserDepth  uint32
	MaxQuerySize    uint32
	Obfuscate       bool
	Oneline         bool
	Timeout         time.Duration
}

func (inst FormaterOptions) ToArgs() []string {
	args := make([]string, 0, 9)
	if inst.Hilite {
		args = append(args, "--hilite")
	}
	if inst.KeepComments {
		args = append(args, "--comments")
	}
	args = append(args, "--max_line_length", strconv.FormatUint(uint64(inst.MaxLineLength), 10))
	if inst.AllowMultiQuery {
		args = append(args, "--multiquery")
	}
	if inst.Seed != "" {
		args = append(args, "--seed", inst.Seed)
	}
	args = append(args, "--max_parser_depth", strconv.FormatUint(uint64(inst.MaxParserDepth), 10))
	args = append(args, "--max_query_size", strconv.FormatUint(uint64(inst.MaxQuerySize), 10))
	if inst.Obfuscate {
		args = append(args, "--obfuscate")
	}
	if inst.Oneline {
		args = append(args, "--oneline")
	}
	return args
}

func NewFormater(opts FormaterOptions) (inst *Formater, err error) {
	inst = &Formater{
		opts:      opts,
		args:      opts.ToArgs(),
		buf:       bytes.NewBuffer(make([]byte, 0, 4096*2)),
		stderrBuf: bytes.NewBuffer(make([]byte, 0, 4096)),
	}
	return
}
func (inst *Formater) Format(sql io.Reader) (sqlOut string, err error) {
	buf := inst.buf
	buf.Reset()
	err = inst.FormatToWriter(sql, buf)
	if err != nil {
		return
	}
	sqlOut = buf.String()
	return
}
func (inst *Formater) FormatFromString(sql string) (sqlOut string, err error) {
	return inst.Format(strings.NewReader(sql))
}
func (inst *Formater) FormatToWriter(sql io.Reader, out io.Writer) (err error) {
	var ctx context.Context
	if inst.opts.Timeout == 0 {
		ctx = context.Background()
	} else {
		var cancel func()
		ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(inst.opts.Timeout))
		defer cancel()
	}
	stderrBuf := inst.stderrBuf
	cmd := exec.CommandContext(ctx, inst.opts.BinaryPath, inst.args...)
	cmd.Stdin = sql
	cmd.Stdout = out
	cmd.Stderr = stderrBuf
	err = cmd.Run()
	if err != nil {
		err = eb.Build().Str("stderr", stderrBuf.String()).Str("binary", inst.opts.BinaryPath).Strs("args", inst.args).Errorf("unable to format sql: %w", err)
		return
	}
	return
}
