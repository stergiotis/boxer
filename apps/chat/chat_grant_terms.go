package chat

import (
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
)

// grantTermsLine is the task's bounds as the model reads them in the
// answer to request_access: how many calls it may make, when the task ends
// and what it may reach. The person may approve less than was asked, and a
// model that knows its budget plans by it instead of finding the bounds by
// being refused. Empty when the host reported no terms.
func grantTermsLine(t *agent.GrantTerms, now time.Time) string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("the task may make ")
	b.WriteString(strconv.Itoa(t.CallsBudget))
	b.WriteString(" calls, ")
	b.WriteString(strconv.Itoa(t.CallsUsed))
	b.WriteString(" made")
	if !t.Deadline.IsZero() {
		b.WriteString("; it ends in ")
		b.WriteString(roughDuration(t.Deadline.Sub(now)))
	}
	b.WriteString("; destinations: ")
	if len(t.Destinations) == 0 {
		b.WriteString("none")
	} else {
		b.WriteString(strings.Join(t.Destinations, ", "))
	}
	b.WriteString("\n")
	return b.String()
}

// roughDuration is d to the minute, as a person says it.
func roughDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "under a minute"
	}
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	switch {
	case h == 0:
		return strconv.Itoa(m) + " min"
	case m == 0:
		return strconv.Itoa(h) + " h"
	}
	return strconv.Itoa(h) + " h " + strconv.Itoa(m) + " min"
}
