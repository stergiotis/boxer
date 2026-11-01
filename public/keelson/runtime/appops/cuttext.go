package appops

// CutText bounds a text an operation returns to n characters (runes),
// marking a cut with "…". The ops catalogs' descriptions say "cut at n
// characters", and a cut by rune never splits a UTF-8 sequence.
func CutText(s string, n int) (out string) {
	if len(s) <= n {
		return s
	}
	runes := 0
	for i := range s {
		if runes == n {
			return s[:i] + "…"
		}
		runes++
	}
	return s
}
