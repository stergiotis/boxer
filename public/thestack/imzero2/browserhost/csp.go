package browserhost

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// The headers Serve sends on every response it answers itself (ADR-0391).
// The page has inline script and style, so the policy names each inline
// block by its digest rather than allowing inline code; a worker's context
// takes the policy of the worker script's own response.
//
//   - script-src: this origin, the inline blocks of the page served, and
//     'wasm-unsafe-eval' to compile the two modules;
//   - worker-src: this origin only, so a link cannot start a worker from a
//     data: URL;
//   - connect-src: this origin, and https for an app that fetches map tiles;
//   - frame-ancestors: this origin only, so the page cannot be framed by
//     another site;
//   - X-Content-Type-Options: nosniff, so a file is never run as a type it
//     was not served as.
const cspFormat = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'%s; style-src 'self'%s; " +
	"img-src 'self' data: blob: https:; font-src 'self'; connect-src 'self' https:; worker-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

var (
	inlineScript = regexp.MustCompile(`(?is)<script(\s[^>]*)?>(.*?)</script>`)
	inlineStyle  = regexp.MustCompile(`(?is)<style(\s[^>]*)?>(.*?)</style>`)
	srcAttr      = regexp.MustCompile(`(?i)\ssrc\s*=`)
)

// contentSecurityPolicy is the policy for a response whose body is page (an
// HTML document, nil for any other response).
func contentSecurityPolicy(page []byte) (csp string) {
	var scripts, styles strings.Builder
	for _, m := range inlineScript.FindAllSubmatch(page, -1) {
		if srcAttr.Match(m[1]) {
			continue
		}
		scripts.WriteString(" " + cspHash(m[2]))
	}
	for _, m := range inlineStyle.FindAllSubmatch(page, -1) {
		styles.WriteString(" " + cspHash(m[2]))
	}
	return fmt.Sprintf(cspFormat, scripts.String(), styles.String())
}

func cspHash(b []byte) (src string) {
	sum := sha256.Sum256(b)
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// setSecurityHeaders sets the policy for a response with body page (nil for
// a non-HTML response) and the headers every response carries.
func setSecurityHeaders(h http.Header, page []byte) {
	h.Set("Content-Security-Policy", contentSecurityPolicy(page))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
}
