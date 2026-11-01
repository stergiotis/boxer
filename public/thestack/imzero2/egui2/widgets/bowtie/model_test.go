package bowtie

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prototypeRows is the reference bow-tie: three threats, three consequences,
// nine barriers, in an order that is not the barriers' step order.
func prototypeRows() []Row {
	const top = "Ransomware encrypts core ERP"
	r := func(side SideE, path string, step int64, barrier string, st StatusE, ev string) Row {
		return Row{TopEvent: top, Side: side, Path: path, Step: step, HasStep: true, Barrier: barrier, Status: st, Evidence: ev}
	}
	return []Row{
		r(SidePrevention, "Phishing steals credentials", 2, "Mail filtering and sandbox", StatusDegraded, "Tested 3 Aug · 6 of 50 test payloads delivered"),
		r(SidePrevention, "Phishing steals credentials", 1, "MFA on all remote access", StatusEffective, "Tested 12 Sep · 0 bypasses in 40 attempts"),
		r(SidePrevention, "Unpatched VPN appliance", 1, "Known-exploited patches within 14 days", StatusDegraded, "6 critical patches overdue"),
		r(SidePrevention, "Supplier remote access abused", 1, "Supplier access through PAM only", StatusUntested, "No test recorded since Jul last year"),
		r(SidePrevention, "Supplier remote access abused", 2, "Session recording and review", StatusFailed, "Tested 20 Sep · 3 of 10 sessions unrecorded"),
		r(SideMitigation, "Production halt over 48 h", 1, "Offline backups, restore drill", StatusDegraded, "Restore tests 92% passed, appetite 95%"),
		r(SideMitigation, "Production halt over 48 h", 2, "Manual order fallback", StatusEffective, "Exercised 14 Jun · 6 h to switch"),
		r(SideMitigation, "Notification deadline missed", 1, "Incident playbook with 24 h report step", StatusEffective, "Tabletop 2 Oct · report drafted in 9 h"),
		r(SideMitigation, "Data leaked for extortion", 1, "Egress monitoring and EDR containment", StatusFailed, "Red team 18 Sep · exfiltration undetected for 11 h"),
	}
}

func TestBuildModelFoldsThePrototype(t *testing.T) {
	m, err := BuildModel(prototypeRows())
	require.NoError(t, err)
	assert.Equal(t, "Ransomware encrypts core ERP", m.TopEvent)
	require.Len(t, m.Threats, 3)
	require.Len(t, m.Consequences, 3)
	// Paths keep first-seen order; barriers are sorted by step.
	assert.Equal(t, "Phishing steals credentials", m.Threats[0].Name)
	require.Len(t, m.Threats[0].Barriers, 2)
	assert.Equal(t, "MFA on all remote access", m.Threats[0].Barriers[0].Name)
	assert.Equal(t, 1, m.Threats[0].Barriers[0].Source, "Source is the row the barrier came from")
	assert.Equal(t, 0, m.Threats[0].Source, "a path's Source is the first row naming it")
	assert.Equal(t, StatusFailed, m.Threats[2].Barriers[1].Status)
	n := 0
	for _, p := range append(m.Threats, m.Consequences...) {
		n += len(p.Barriers)
	}
	assert.Equal(t, 9, n)
}

func TestBuildModelAcceptsAPathWithNoBarrier(t *testing.T) {
	m, err := BuildModel([]Row{
		{TopEvent: "loss", Side: SidePrevention, Path: "cause"},
		{Side: SideMitigation, Path: "effect", Step: 7, HasStep: true, Barrier: "b"},
	})
	require.NoError(t, err)
	require.Len(t, m.Threats, 1)
	assert.Empty(t, m.Threats[0].Barriers)
	require.Len(t, m.Consequences, 1)
	assert.Equal(t, "b", m.Consequences[0].Barriers[0].Name, "steps need not start at 1")
	assert.Equal(t, "loss", m.TopEvent, "one row naming the top event is enough")
}

func TestBuildModelRefusesWhatIsNotOneBowTie(t *testing.T) {
	cases := map[string][]Row{
		"two top events": {
			{TopEvent: "a", Path: "p"},
			{TopEvent: "b", Path: "q"},
		},
		"no top event":  {{Path: "p"}},
		"no path":       {{TopEvent: "a"}},
		"step, no name": {{TopEvent: "a", Path: "p", Step: 1, HasStep: true}},
		"name, no step": {{TopEvent: "a", Path: "p", Barrier: "b"}},
		"parallel barriers": {
			{TopEvent: "a", Path: "p", Step: 1, HasStep: true, Barrier: "b1"},
			{TopEvent: "a", Path: "p", Step: 1, HasStep: true, Barrier: "b2"},
		},
	}
	for name, rows := range cases {
		_, err := BuildModel(rows)
		assert.Error(t, err, name)
	}
	m, err := BuildModel(nil)
	require.NoError(t, err, "no rows is an empty model, not an error")
	assert.Empty(t, m.TopEvent)
}

func TestSamePathNameOnTwoSidesIsTwoPaths(t *testing.T) {
	m, err := BuildModel([]Row{
		{TopEvent: "a", Side: SidePrevention, Path: "x"},
		{TopEvent: "a", Side: SideMitigation, Path: "x"},
	})
	require.NoError(t, err)
	assert.Len(t, m.Threats, 1)
	assert.Len(t, m.Consequences, 1)
}

func TestStatusVocabulary(t *testing.T) {
	for _, tok := range StatusTokens() {
		st, ok := ParseStatus(tok)
		require.True(t, ok, tok)
		assert.Equal(t, tok, st.Token())
		assert.NotEmpty(t, st.Label())
		assert.NotEmpty(t, st.Icon())
	}
	st, ok := ParseStatus("  Failed ")
	require.True(t, ok)
	assert.Equal(t, StatusFailed, st)
	st, ok = ParseStatus("")
	require.True(t, ok, "an empty status is an untested barrier")
	assert.Equal(t, StatusUntested, st)
	_, ok = ParseStatus("green")
	assert.False(t, ok)

	// Every status differs in icon and label, not in colour alone.
	icons, labels := map[string]bool{}, map[string]bool{}
	for _, tok := range StatusTokens() {
		st, _ := ParseStatus(tok)
		icons[st.Icon()], labels[st.Label()] = true, true
	}
	assert.Len(t, icons, len(StatusTokens()))
	assert.Len(t, labels, len(StatusTokens()))
}

func TestSideVocabulary(t *testing.T) {
	s, ok := ParseSide(" Mitigation")
	require.True(t, ok)
	assert.Equal(t, SideMitigation, s)
	assert.Equal(t, "mitigation", s.String())
	_, ok = ParseSide("left")
	assert.False(t, ok)
}
