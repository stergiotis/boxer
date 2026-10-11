package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// keelson('harness_profiles') is one row per constant, ordered by app, id
// and field; a profile published again replaces its rows; the table is
// live, since an owner publishes when it first builds its registry, and
// its rows never alias the owner's map.
func TestPublishedProfilesAreALiveLongTable(t *testing.T) {
	fields := map[string]string{"rounds": "24", "keep_turns": "2"}
	PublishProfile(ProfileRow{App: "test/app", Id: "default", Fingerprint: "fp1", Fields: fields})
	PublishProfile(ProfileRow{App: "test/app", Id: "alt", Fingerprint: "fp2", Fields: map[string]string{"rounds": "8"}})
	fields["rounds"] = "99"
	rows := PublishedProfiles()
	var mine []ProfileRow
	for _, r := range rows {
		if r.App == "test/app" {
			mine = append(mine, r)
		}
	}
	require.Len(t, mine, 2)
	assert.Equal(t, "alt", mine[0].Id, "by id within an app")
	assert.Equal(t, "24", mine[1].Fields["rounds"], "the owner's later write did not reach the table")
	mine[1].Fields["rounds"] = "7"
	assert.Equal(t, "24", PublishedProfiles()[len(rows)-1].Fields["rounds"], "a reader's write did not reach it either")

	PublishProfile(ProfileRow{App: "test/app", Id: "default", Fingerprint: "fp3", Fields: map[string]string{"rounds": "12"}})
	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg))
	p, ok := reg.Lookup(TableProfiles)
	require.True(t, ok)
	assert.Equal(t, introspect.FreshnessLive, p.Freshness())
	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer rec.Release()
	var got []string
	for i := range int(rec.NumRows()) {
		if rec.Column(0).ValueStr(i) != "test/app" {
			continue
		}
		got = append(got, rec.Column(1).ValueStr(i)+"@"+rec.Column(2).ValueStr(i)+" "+rec.Column(3).ValueStr(i)+"="+rec.Column(4).ValueStr(i))
	}
	assert.Equal(t, []string{"alt@fp2 rounds=8", "default@fp3 rounds=12"}, got, "replaced, one row per field, fields sorted")
}
