package agent

import "github.com/stergiotis/boxer/public/keelson/runtime/inscribe"

func inscribeAnnotation(task string) inscribe.Annotation {
	return inscribe.Annotation{Task: task, Id: "x", Op: inscribe.OpHighlight, Targets: []inscribe.Anchor{{Window: 7}}}
}
