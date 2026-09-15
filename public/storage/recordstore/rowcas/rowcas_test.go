package rowcas

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The statements are the conditions: a change here is a change of the
// model, so they are pinned verbatim.
func TestStatements(t *testing.T) {
	assert.Equal(t, `UPDATE db.t SET "a" = [1], "b" = ['x'] WHERE "k" = 'j' AND "a"[1] = 0 SETTINGS update_parallel_mode='sync'`,
		UpdateSQL("db.t", []Set{{`"a"`, "[1]"}, {`"b"`, "['x']"}}, `"k" = 'j' AND "a"[1] = 0`))
	assert.Equal(t, "", UpdateSQL("db.t", []Set{{`"a"`, "[1]"}}, " "), "an unguarded update is not composed")
	assert.Equal(t, "", UpdateSQL("db.t", nil, `"k" = 'j'`))
	assert.Equal(t, `DELETE FROM db.t WHERE "a"[1] < 5 SETTINGS lightweight_delete_mode='lightweight_update'`, DeleteSQL("db.t", `"a"[1] < 5`))
	assert.Equal(t, "", DeleteSQL("db.t", ""))
	assert.Equal(t, "ALTER TABLE db.t MODIFY SETTING enable_block_number_column=1, enable_block_offset_column=1", AlterTableSettingsSQL("db.t"))
	assert.Equal(t, ", enable_block_number_column=1, enable_block_offset_column=1", TableSettingsTail)
}
