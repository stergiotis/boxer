package jackstay

import "strings"

// SplitKeyExprs splits a system.tables key column (sorting_key,
// partition_key) into its top-level expressions. The server writes these as a
// comma-separated list with identifiers backquoted where needed, so commas are
// split at paren depth zero, outside quotes. An empty key and `tuple()` both
// yield no expressions.
func SplitKeyExprs(key string) (exprs []string) {
	key = strings.TrimSpace(key)
	if key == "" || key == "tuple()" {
		return
	}
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if quote != 0 {
			switch ch {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				exprs = append(exprs, strings.TrimSpace(key[start:i]))
				start = i + 1
			}
		}
	}
	exprs = append(exprs, strings.TrimSpace(key[start:]))
	return
}

// closingParen returns the index of the parenthesis that closes the one at
// s[open], skipping quoted text, or -1 when it is not closed.
func closingParen(s string, open int) (end int) {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			switch ch {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
