//go:build integration

package mdnotebook_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

// The buffer of a buffer's notebook answers as the buffer does: taking a
// buffer apart into records and composing them again changes its text and
// not its rows (ADR-0371 §SD6).
func TestFromBufferKeepsWhatTheBufferAnswers(t *testing.T) {
	if _, err := exec.LookPath("clickhouse-local"); err != nil {
		t.Skip("clickhouse-local is not installed")
	}
	run := func(sql string) string {
		out, err := exec.Command("clickhouse-local", "-q", sql).CombinedOutput()
		require.NoError(t, err, "%s\n%s", sql, out)
		return string(out)
	}
	for _, sql := range bufferCases {
		text, err := mdnotebook.FromBuffer(sql, "")
		require.NoError(t, err, sql)
		nb, err := mdnotebook.Read(text)
		require.NoError(t, err, sql)
		buffer, err := mdnotebook.Buffer(nb)
		require.NoError(t, err, sql)
		assert.Equal(t, run(sql), run(buffer), "%s\nas\n%s", sql, buffer)
	}
}
