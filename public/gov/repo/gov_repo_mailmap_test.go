package repo

import "testing"

// TestParseMailmapResolve exercises all four gitmailmap forms, the pair-over-
// email precedence, case-insensitive email matching, case-sensitive form-4
// name matching, comment/blank-line tolerance, and the nil-receiver fast path.
// The parser treats the bracketed identity as an opaque key, so non-email
// tokens exercise the same logic without depending on git.
func TestParseMailmapResolve(t *testing.T) {
	cases := []struct {
		name    string
		mailmap string
		inName  string
		inEmail string
		wantN   string
		wantE   string
	}{
		{
			name:    "form1 name fix for canonical id",
			mailmap: "Jane Doe <id_plain>\n",
			inName:  "j", inEmail: "id_plain",
			wantN: "Jane Doe", wantE: "id_plain",
		},
		{
			name:    "form1 any name on the id maps",
			mailmap: "Jane Doe <id_plain>\n",
			inName:  "whatever", inEmail: "ID_PLAIN",
			wantN: "Jane Doe", wantE: "id_plain",
		},
		{
			name:    "form2 id merge keeps commit name",
			mailmap: "<id_new> <id_old>\n",
			inName:  "Jane", inEmail: "id_old",
			wantN: "Jane", wantE: "id_new",
		},
		{
			name:    "form3 id and name merge",
			mailmap: "Jane Doe <id_new> <id_old>\n",
			inName:  "Jane", inEmail: "ID_old",
			wantN: "Jane Doe", wantE: "id_new",
		},
		{
			name:    "form4 pair match folds only the named identity",
			mailmap: "Jane Doe <id_new> Jane Old <id_old>\n",
			inName:  "Jane Old", inEmail: "id_old",
			wantN: "Jane Doe", wantE: "id_new",
		},
		{
			name:    "form4 no name match does not fold",
			mailmap: "Jane Doe <id_new> Jane Old <id_old>\n",
			inName:  "Jane Older", inEmail: "id_old",
			wantN: "Jane Older", wantE: "id_old",
		},
		{
			name:    "form4 name match is case-sensitive",
			mailmap: "Jane Doe <id_new> Jane Old <id_old>\n",
			inName:  "jane old", inEmail: "id_old",
			wantN: "jane old", wantE: "id_old",
		},
		{
			name: "pair beats email for the same commit id",
			mailmap: "Jane Doe <id_new> Jane Old <id_old>\n" +
				"<id_new> <id_old>\n",
			inName: "Jane Old", inEmail: "id_old",
			wantN: "Jane Doe", wantE: "id_new",
		},
		{
			name: "email fallback applies when form4 name misses",
			mailmap: "Jane Doe <id_new> Jane Old <id_old>\n" +
				"Jane Doe <id_new> <id_old>\n",
			inName: "Jane Older", inEmail: "id_old",
			wantN: "Jane Doe", wantE: "id_new",
		},
		{
			name:    "no match leaves identity untouched",
			mailmap: "Jane Doe <id_new> <id_old>\n",
			inName:  "Bob", inEmail: "id_other",
			wantN: "Bob", wantE: "id_other",
		},
		{
			name:    "comments and blank lines ignored",
			mailmap: "# header comment\n\n   \nJane Doe <id_new> <id_old>\n",
			inName:  "Jane", inEmail: "id_old",
			wantN: "Jane Doe", wantE: "id_new",
		},
		{
			name:    "malformed line skipped, next line parsed",
			mailmap: "Jane Doe id_unmatched\nJane Doe <id_new> <id_old>\n",
			inName:  "Jane", inEmail: "id_old",
			wantN: "Jane Doe", wantE: "id_new",
		},
		{
			name:    "nil mailmap lower-cases id only",
			mailmap: "",
			inName:  "Jane", inEmail: "ID_X",
			wantN: "Jane", wantE: "id_x",
		},
		{
			name:    "comment-only mailmap returns nil",
			mailmap: "# only a comment\n",
			inName:  "Jane", inEmail: "ID_X",
			wantN: "Jane", wantE: "id_x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mm := ParseMailmap(tc.mailmap)
			gotN, gotE := mm.Resolve(tc.inName, tc.inEmail)
			if gotN != tc.wantN || gotE != tc.wantE {
				t.Errorf("Resolve(%q, %q) = (%q, %q), want (%q, %q)",
					tc.inName, tc.inEmail, gotN, gotE, tc.wantN, tc.wantE)
			}
		})
	}
}
