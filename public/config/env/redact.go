package env

import (
	"net/url"
	"strings"
)

// redactedMark replaces a secret inside a value that is otherwise shown.
const redactedMark = "xxxxx"

// unparsableRedacted stands for a whole value that names a credential but
// does not parse, so its parts cannot be told apart.
const unparsableRedacted = "<redacted: unparsable URL with credentials>"

// credentialQueryKeys are query parameters that carry a secret. ClickHouse's
// HTTP interface takes `password` as one; the rest are the common spellings.
var credentialQueryKeys = []string{"password", "passwd", "pass", "pwd", "secret", "token", "access_token", "api_key", "apikey", "key", "sig", "signature"}

// RedactURL returns raw with its secrets replaced: the password in the
// userinfo (url.URL.Redacted's rule) and the value of every query parameter
// that names a credential. A value with nothing to redact is returned
// unchanged, byte for byte, so it is safe to pass any setting through. A
// value that does not parse but carries an '@' or a credential-looking
// parameter is replaced whole, since its secret cannot be found reliably.
//
// It is what every log line, error and caller-facing listing that shows a
// URL-valued setting (CLICKHOUSE_URL, a NATS URL) passes the value through.
func RedactURL(raw string) (out string) {
	out = raw
	if !strings.Contains(raw, "@") && !strings.Contains(raw, "?") {
		return
	}
	u, err := url.Parse(raw)
	if err != nil {
		if strings.Contains(raw, "@") || hasCredentialKey(strings.ToLower(raw)) {
			out = unparsableRedacted
		}
		return
	}
	changed := false
	if u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.UserPassword(u.User.Username(), redactedMark)
			changed = true
		}
	}
	if u.RawQuery != "" {
		q, qErr := url.ParseQuery(u.RawQuery)
		if qErr != nil {
			if hasCredentialKey(strings.ToLower(u.RawQuery)) {
				u.RawQuery = redactedMark
				changed = true
			}
		} else {
			queryChanged := false
			for k, vs := range q {
				if !isCredentialKey(k) {
					continue
				}
				for i := range vs {
					vs[i] = redactedMark
				}
				queryChanged = true
			}
			if queryChanged {
				u.RawQuery = q.Encode()
				changed = true
			}
		}
	}
	if changed {
		out = u.String()
	}
	return
}

func isCredentialKey(k string) (yes bool) {
	k = strings.ToLower(k)
	for _, c := range credentialQueryKeys {
		if k == c {
			return true
		}
	}
	return
}

func hasCredentialKey(lower string) (yes bool) {
	for _, c := range credentialQueryKeys {
		if strings.Contains(lower, c+"=") {
			return true
		}
	}
	return
}
