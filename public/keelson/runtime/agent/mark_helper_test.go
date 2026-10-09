package agent

import "github.com/stergiotis/boxer/public/keelson/runtime/inscribe"

func inscribeMark(task string) inscribe.Mark {
	return inscribe.Mark{Task: task, Id: "x", Op: inscribe.OpHighlight, Targets: []inscribe.Anchor{{Window: 7}}}
}
