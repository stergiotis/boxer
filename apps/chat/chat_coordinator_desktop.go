package chat

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providersgui"
)

// The coordinator's window tools (ADR-0276): query_windows reads the
// desktop as keelson data under the chat's own per-table grant, which needs
// no task; arrange_windows, raise_window and place_window change it under
// the task's grant.

// maxWindowRows bounds what query_windows hands the model, in bytes of JSON
// lines; a desktop has tens of windows, so only a statement that ignores
// the table's shape reaches it.
const maxWindowRows = 16 << 10

func (inst *coordinator) queryWindows(ctx context.Context, table string, sql string) (content string, activity string) {
	if inst.kq == nil {
		return "error: this window cannot read keelson tables", "query_windows: no client"
	}
	if table != providersgui.TableWindows && table != providersgui.TableDesktop {
		return `error: table is "windows" or "desktop"`, "query_windows: no such table"
	}
	res, err := inst.kq.Query(ctx, table, sql, keelsonquery.FormatJSONEachRow)
	if err != nil {
		inst.refuse(err.Error())
		return "error: " + err.Error(), "query_windows: " + err.Error()
	}
	body := string(res.Body)
	if len(body) > maxWindowRows {
		body = body[:maxWindowRows] + "\n… cut at " + strconv.Itoa(maxWindowRows) + " bytes; select fewer columns or rows"
	}
	rows := strings.Count(strings.TrimSpace(body), "\n") + 1
	if strings.TrimSpace(body) == "" {
		rows = 0
	}
	activity = "read " + strconv.Itoa(rows) + " row(s) of keelson('" + table + "')"
	if table == providersgui.TableWindows {
		// Window titles are the apps' text, and the person's (ADR-0269 §SD7).
		inst.markTainted()
		return wrapUntrusted("keelson('windows')", body), activity
	}
	return body, activity
}

// windowVerb runs arrange_windows, raise_window or place_window.
func (inst *coordinator) windowVerb(ctx context.Context, asked agent.Asked, name string, args map[string]any) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "error: no task yet; call request_access first", name + ": no task"
	}
	num := func(k string) (f float64) { f, _ = args[k].(float64); return }
	window := uint64(num("window"))
	var out agent.Outcome
	var err error
	switch name {
	case "arrange_windows":
		command, _ := args["command"].(string)
		var keys []uint64
		if ws, ok := args["windows"].([]any); ok {
			for _, w := range ws {
				if f, isNum := w.(float64); isNum {
					keys = append(keys, uint64(f))
				}
			}
		}
		out, err = inst.cli.Arrange(ctx, h, asked, command, keys)
		activity = command + " " + windowsText(keys)
	case "raise_window":
		out, err = inst.cli.Raise(ctx, h, asked, window)
		activity = "raise window " + strconv.FormatUint(window, 10)
	case "place_window":
		out, err = inst.cli.Place(ctx, h, asked, window, float32(num("x")), float32(num("y")), float32(num("w")), float32(num("h")))
		activity = "place window " + strconv.FormatUint(window, 10)
	}
	if err != nil {
		inst.refuse(err.Error())
		return "error: " + err.Error(), activity + ": " + err.Error()
	}
	b, _ := json.Marshal(callOutcome{Phase: out.Phase, Reason: out.Reason})
	content = string(b)
	if out.Phase != "completed" {
		inst.refuse(out.Reason)
		if name == "arrange_windows" && strings.Contains(out.Reason, "desktop") {
			n, _ := json.Marshal(nextStep{Tool: "request_access", Args: map[string]any{"plan": "arrange the windows", "desktop": true},
				Then: "call arrange_windows again once the person granted the desktop"})
			content += "\nnext: " + string(n)
		}
		return content, activity + ": " + out.Phase
	}
	return content, activity
}

func windowsText(keys []uint64) (s string) {
	if len(keys) == 0 {
		return "every window"
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, strconv.FormatUint(k, 10))
	}
	return "windows " + strings.Join(parts, ", ")
}
