// Package carrierclient drives an imzero2 headless host over the remote-access
// carrier of [ADR-0024], with the accessibility-tree channel, coordinate-free
// actuation and capture-on-demand added by [ADR-0154].
//
// It is the non-browser counterpart to the shipped viewer. Where the browser
// decodes the video stream and shows it to a person, this client ignores the
// pixels on the wire entirely and asks the host to write PNGs at moments it
// chooses — so a scripted run needs no compositor, no browser, no GPU
// compositing path and no `egui_inspection` port. That last part is the reason
// this exists at all: the inspection plugin the desktop host exposes to
// egui-mcp requires eframe, which the headless build exists to exclude, so the
// carrier is the only seam a headless host has.
//
// The wire types in input_pb.out.go are generated from the canonical contract
// at proto/boxer/imzero2/v1/input.proto, the same file the Rust host generates
// from, so neither end can drift from it. Regenerate with:
//
//	go generate ./public/thestack/imzero2/carrierclient/
//
// Typical use — connect, resolve a widget by name, actuate it by id, capture:
//
//	c, err := carrierclient.Connect(carrierclient.Config{URL: "ws://127.0.0.1:8089/"})
//	tree, err := c.Tree(5 * time.Second)
//	node := carrierclient.FindByName(tree, "Panes")
//	err = c.ClickNode(node.GetId())
//	done, err := c.Capture("panes-open", 5 * time.Second)
//
// Only the ACTIVE connection's input, tree requests and captures are honoured
// (ADR-0086); the first connection to a host is admitted active, so a driver
// pointed at a host someone is already watching will be passive and silently
// ineffective.
//
// [ADR-0024]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0024-imzero2-remote-access-browser-viewer.md
// [ADR-0154]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0154-headless-carrier-tree-and-driver.md
package carrierclient

//go:generate sh -c "go run -tags=\"$(cat ../../../../tags)\" github.com/stergiotis/boxer/public/app protogen --protoRoot ../../../../proto --out input_pb.out.go"
