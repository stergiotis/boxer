package definition

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
)

func definitionsBlock() (blocks []*ir.BuilderFactoryNode) {
	blocks = make([]*ir.BuilderFactoryNode, 0, 64)
	blocks = append(blocks, idl.NewBuilderFactoryNode("tree").
		WithIdentityId(true).
		WithSettingImmediate(true).
		WithConstructionCodeClientRust(rustClientCode("egui_ltreeview::TreeViewSettings::default();\n")).
		WithApplyCodeClientRust(rustClientCode(`if {{EguiUiOptionalOuter}}.is_some() {
	let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
	// egui_ltreeview 0.7 quirk: TreeViewBuilder::close_dir reads ui.clip_rect()
	// and calls Rect::clamp on it (builder.rs draw_indent_hint). Rect::clamp
	// panics in f32::clamp when the rect is negative (min > max) or NaN. A
	// negative clip can appear inside nested ScrollAreas when the inner
	// content is pinched horizontally by the outer scrollbar's reservation.
	// Skip the tree in that case; queued node commands are drained by
	// prepare_next_frame so the protocol stream stays balanced.
	let clip = ui.clip_rect();
	if clip.is_finite() && !clip.is_negative() {
	let tree = TreeView::new({{Id}});
	let mut closed_ids = Vec::with_capacity(32); // NOTE: necessary as state.node_states is private
    let mut state = egui_ltreeview::TreeViewState::load(ui, {{Id}}).unwrap_or_default();
	let (response, _actions) = tree.with_settings({{Instance}}).show_state(ui, &mut state, |tv| {
		for cmd in self.r3_node_cmds.drain(..) {
			match cmd {
				NodeCommand::NodeDir(node) => {
					let id = *node.id();
					if !tv.node(node) {
						closed_ids.push(id);
					}
				}
				NodeCommand::NodeLeaf(node) => {
					tv.node(node);
				}
				NodeCommand::NodeDirClose(child_count) => {
					tv.close_dir_in(child_count);
				}
			}
		}
	});
    for s in state.selected().iter() {
        self.r7_push(*s, ResponseFlags::NODELIKE_SELECTED);
    }
    for id in closed_ids.drain(..) {
	    self.r7_push(id, ResponseFlags::BLOCK_SKIPPED);
    }
    state.store(ui,i);
    //for action in actions {
 	//	match action {
 	//		egui_ltreeview::Action::Activate(egui_ltreeview::Activate {selected , modifiers: _}) => {
 	//			for s in selected.iter() {
 	//				self.r7_push(*s, ResponseFlags::NODELIKE_ACTIVATED);
 	//			}
 	//		},
 	//		egui_ltreeview::Action::SetSelected(selected) => {
 	//			for s in selected.iter() {
 	//				self.r7_push(*s, ResponseFlags::NODELIKE_ACTIVATED);
 	//			}
 	//		},
 	//		_ => {
 	//		}
 	//	}
 	//}
	let _ = response;
	}
}
`)).Build())
	blocks = append(blocks,
		idl.NewBuilderFactoryNode("window").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("label", structWidgetText()).Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("defaultOpen").Arg("val", ctabb.B).EndMethod().
				BeginMethod("enabled").Arg("val", ctabb.B).EndMethod().
				BeginMethod("interactable").Arg("val", ctabb.B).EndMethod().
				BeginMethod("movable").Arg("val", ctabb.B).EndMethod().
				BeginMethod("resizable").Arg("val", ctabb.B).EndMethod().
				BeginMethod("collapsible").Arg("val", ctabb.B).EndMethod().
				BeginMethod("titleBar").Arg("val", ctabb.B).EndMethod().
				BeginMethod("defaultWidth").Arg("width", ctabb.F32).
				CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.default_width(width);\n")).EndMethod().
				BeginMethod("defaultHeight").Arg("height", ctabb.F32).
				CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.default_height(height);\n")).EndMethod().
				BeginMethod("defaultSize").Arg("width", ctabb.F32).Arg("height", ctabb.F32).
				CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.default_size(egui::vec2(width, height));\n")).EndMethod().
				// defaultPos sets the initial-open position in egui logical
				// pixels (viewport-relative top-left origin). egui retains
				// the user's dragged position after first open, so emitting
				// this every frame is safe — only the first frame the window
				// has no persisted position consumes the hint. Use for
				// click-anchored popups (e.g. fsmview), tooltip-style
				// surfaces, or any "open near my caller" affordance.
				BeginMethod("defaultPos").Arg("posX", ctabb.F32).Arg("posY", ctabb.F32).
				CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.default_pos(egui::pos2(pos_x, pos_y));\n")).EndMethod().
				BeginMethod("minWidth").Arg("width", ctabb.F32).
				CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.min_width(width);\n")).EndMethod().
				BeginMethod("minHeight").Arg("height", ctabb.F32).
				CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.min_height(height);\n")).EndMethod().
				// alwaysOnTop lifts the window from the default
				// egui::Order::Middle layer to egui::Order::Foreground so
				// it floats above ordinary windows (and below tooltips,
				// which sit on Order::Tooltip). Use for inspector-style
				// floating surfaces that tether to a caller widget — if
				// the surface they reference can be obscured by a normal
				// window, the visual tether (bezier connector etc.)
				// reads backwards. egui orders within the same Order
				// layer by LayerId / interaction recency, so multiple
				// always-on-top windows still draw above any
				// Order::Middle siblings without colliding with the
				// foreground bezier overlay (which lives on its own
				// LayerId, `imzero-absolute-overlay`).
				BeginMethod("alwaysOnTop").Arg("val", ctabb.B).
				CodeClientRust(rustClientCode("if val { {{Instance}} = {{Instance}}.order(egui::Order::Foreground); }\n")).EndMethod().
				// openBound wires egui::Window's `.open(&mut bool)` close
				// affordance to a Go-side bool via the r10 databinding
				// channel. Pass a non-zero bindingId on every frame the
				// window should render; the matching Go-side call site
				// is StateManager.AddR10Databinding(bindingId, &openFlag).
				// When the user clicks the title-bar X, egui flips the
				// bool to false; the Window apply code records the
				// transition on r10 so the next Sync writes through to
				// the caller's openFlag, which the WindowHost reads to
				// trigger Close+Unmount on the next frame. bindingId==0
				// disables the close affordance (the default; preserves
				// pre-binding behaviour for any in-tree c.Window usage).
				BeginMethod("openBound").Arg("bindingId", ctabb.U64).
				CodeClientRust(rustClientCode("self.scratch_open_binding_id = binding_id;\n")).EndMethod().
				Build()...).
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			// The construction code expands inside `let mut w = <here>` —
			// only one expression fits in that RHS. The OpenBound scratch
			// state lives on ImZeroFffi (scratch_open_binding_id) so the
			// openBound method body can write to self.* and the apply
			// block below drains it with std::mem::take; no outer-scope
			// let is needed and the construction stays a single
			// expression (see ImZeroFffi struct comment).
			WithConstructionCodeClientRust(
				rustClientCode("egui::Window::new(label).id({{Id}});\n")).
			WithApplyCodeClientRust(rustClientCode(`
				let open_binding_id = std::mem::take(&mut self.scratch_open_binding_id);
				// window_open always defaults to true: Go re-emitting this
				// opcode IS the "I want to be open" signal. egui itself
				// doesn't persist window visibility across frames (only
				// position/size/collapsed are stored in egui::Memory keyed
				// by .id()), so reseeding to true every apply matches the
				// caller's intent. If the user clicks the title-bar X
				// inside the show() body, egui mutates window_open to false
				// via the &mut bool; the post-show transition check pushes
				// that change to r10 so Go's *openFlag flips to false and
				// the next frame skips this opcode entirely. Without the
				// cache reseed, the X-then-toggle reopen path was broken:
				// the stale "false" persisted and w.open(&mut false)
				// silently returned None on every subsequent emit.
				let mut window_open: bool = true;
				let was_open = window_open;
				let retr = if open_binding_id != 0 {
					{{Instance}}.open(&mut window_open).show(c, |ui| {
						let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
					})
				} else {
					{{Instance}}.show(c, |ui| {
						let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
					})
				};
				if open_binding_id != 0 && was_open != window_open {
					self.r10_push(open_binding_id, window_open);
				}
				let mut resp2 = ResponseFlags::empty();
                if retr.is_none() {
                    // closed (egui::Window::open(false), or the user
                    // clicked the title-bar X on this frame)
					resp2.insert(ResponseFlags::BLOCK_SKIPPED);
                    self.interpret_outer({{EguiContext}}, &mut None)?;
                } else {
                    let inner = retr.unwrap();
                    resp2.populate(&inner.response);
                    if inner.inner.is_none() {
                        // collapsed
                        resp2.insert(ResponseFlags::BLOCK_SKIPPED);
                        self.interpret_outer({{EguiContext}}, &mut None)?;
                    } else {
                        // open
                    }
                }
				self.r7_push({{Id}}.value(), resp2);
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("collapsingHeader").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("label", structWidgetText()).Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("defaultOpen").Arg("val", ctabb.B).EndMethod().
			BeginMethod("open").Arg("val", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.open(Some(true))")).EndMethod().
			BeginMethod("close").Arg("val", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.open(Some(false))")).EndMethod().
			Build()...).
		WithSettingBlockIterator(true).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode("egui::CollapsingHeader::new(label).id_salt({{Id}});\n")).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						if {{Instance}}.show({{EguiUiOptionalOuter}}.as_mut().unwrap(), |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						}).body_returned.is_none() {
							self.r7_push({{Id}}.value(), ResponseFlags::BLOCK_SKIPPED);
							self.interpret_outer({{EguiContext}}, &mut None)?;
						}
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("frame").
		WithIdentityId(true).
		AddMethods(idl.NewMethodBuilder().
			// Uniform margin/radius (existing)
			BeginMethod("innerMargin").Arg("val", ctabb.F32).EndMethod().
			BeginMethod("outerMargin").Arg("val", ctabb.F32).EndMethod().
			BeginMethod("cornerRadius").Arg("val", ctabb.F32).EndMethod().
			// Per-side margins
			BeginMethod("innerMarginSides").Arg("left", ctabb.F32).Arg("right", ctabb.F32).Arg("top", ctabb.F32).Arg("bottom", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.inner_margin(egui::Margin{left: left as i8, right: right as i8, top: top as i8, bottom: bottom as i8});\n")).EndMethod().
			BeginMethod("outerMarginSides").Arg("left", ctabb.F32).Arg("right", ctabb.F32).Arg("top", ctabb.F32).Arg("bottom", ctabb.F32).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.outer_margin(egui::Margin{left: left as i8, right: right as i8, top: top as i8, bottom: bottom as i8});\n")).EndMethod().
			// Per-corner radius
			BeginMethod("cornerRadiusSides").Arg("nw", ctabb.U8).Arg("ne", ctabb.U8).Arg("sw", ctabb.U8).Arg("se", ctabb.U8).
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.corner_radius(egui::CornerRadius{nw, ne, sw, se});\n")).EndMethod().
			// Colors
			BeginMethod("fill").EvaluatedArg("col", structColor32()).AsColor().
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.fill(col);\n")).EndMethod().
			BeginMethod("stroke").Arg("width", ctabb.F32).EvaluatedArg("col", structColor32()).AsColor().
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.stroke(egui::Stroke::new(width, col));\n")).EndMethod().
			// Shadow
			BeginMethod("shadow").Arg("offsetX", ctabb.F32).Arg("offsetY", ctabb.F32).Arg("blur", ctabb.U8).Arg("spread", ctabb.U8).EvaluatedArg("col", structColor32()).AsColor().
			CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.shadow(egui::Shadow{offset: [offset_x as i8, offset_y as i8], blur, spread, color: col});\n")).EndMethod().
			// Opacity
			BeginMethod("multiplyWithOpacity").Arg("val", ctabb.F32).EndMethod().
			// Interaction sensing (Frame only reports hover by default)
			BeginMethod("senseClick").
			CodeClientRust(rustClientCode("sense_click = true;\n")).EndMethod().
			BeginMethod("senseDrag").
			CodeClientRust(rustClientCode("sense_drag = true;\n")).EndMethod().
			// hoverCursorPointer changes the OS cursor to a pointing
			// hand whenever the pointer is over this Frame — the
			// universal "this is clickable" cue. Only meaningful when
			// the Frame also calls .senseClick() / .senseDrag(), since
			// the cursor is applied to the synthetic Sense layer that
			// those methods install (Frame without sense.* has no
			// response surface to attach a cursor to). Adds nothing to
			// the at-rest visual — discoverability comes from the
			// cursor flip alone, so the Frame stays as quiet as before
			// for non-hovering eyes.
			BeginMethod("hoverCursorPointer").
			CodeClientRust(rustClientCode("hover_cursor_pointer = true;\n")).EndMethod().
			// Preset constructors (reset to themed defaults)
			BeginMethod("presetGroup").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::group({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			BeginMethod("presetWindow").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::window({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			BeginMethod("presetPopup").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::popup({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			BeginMethod("presetMenu").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::menu({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			BeginMethod("presetCanvas").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::canvas({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			BeginMethod("presetDarkCanvas").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::dark_canvas({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			BeginMethod("presetSideTopPanel").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::side_top_panel({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			BeginMethod("presetCentralPanel").
			CodeClientRust(rustClientCode("{{Instance}} = egui::Frame::central_panel({{EguiContext}}.style_of({{EguiContext}}.theme()).as_ref());\n")).EndMethod().
			Build()...).
		WithSettingBlockIterator(true).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithConstructionCodeClientRust(rustClientCode("egui::Frame::new();\nlet mut sense_click = false;\nlet mut sense_drag = false;\nlet mut hover_cursor_pointer = false;\n")).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						let r2 = {{Instance}}.show(ui, |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
						let mut resp2 = ResponseFlags::empty();
						if sense_click || sense_drag {
							let mut sense = egui::Sense::hover();
							if sense_click { sense = sense | egui::Sense::click(); }
							if sense_drag { sense = sense | egui::Sense::drag(); }
							let response = ui.interact(r2.response.rect, egui::Id::new({{Id}}.value()).with("sense"), sense);
							let response = if hover_cursor_pointer {
								response.on_hover_cursor(egui::CursorIcon::PointingHand)
							} else {
								response
							};
							resp2.populate(&response);
						} else {
							resp2.populate(&r2.response);
						}
						self.r7_push({{Id}}.value(), resp2);
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks,
		idl.NewBuilderFactoryNode("comboBox").
			WithIdentityId(true).
			AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("label", structWidgetText()).Build()).
			AddArguments(idl.NewArgumentsBuilder().
				EvaluatedArg("selectedText", structWidgetText()).
				Build()).
			AddMethods(idl.NewMethodBuilder().
				BeginMethod("width").Arg("width", ctabb.F32).EndMethod().
				BeginMethod("height").Arg("height", ctabb.F32).EndMethod().
				BeginMethod("wrap").EndMethod().
				BeginMethod("truncate").EndMethod().
				Build()...).
			WithConstructionCodeClientRust(rustClientCode("egui::ComboBox::new({{Id}},label).selected_text(selected_text);\n")).
			WithSettingImmediate(true).
			WithSettingRetained(true).
			WithSettingBlockIterator(true).
			WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						if {{Instance}}.show_ui({{EguiUiOptionalOuter}}.as_mut().unwrap(), |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
							return Some(true);
						}).inner.is_none() {
							self.r7_push({{Id}}.value(), ResponseFlags::BLOCK_SKIPPED);
							self.interpret_outer({{EguiContext}}, &mut None)?;
						}
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("scrollArea").
		AddMethods(
			idl.NewMethodBuilder().
				BeginMethod("hscroll").Arg("val", ctabb.B).EndMethod().
				BeginMethod("vscroll").Arg("val", ctabb.B).EndMethod().
				BeginMethod("animated").Arg("val", ctabb.B).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.animated(val && !self.animation_freeze);\n")).EndMethod().
				// autoShrink mirrors egui::ScrollArea::auto_shrink([h, v]).
				// false on an axis lets the ScrollArea fill the parent's
				// available space along that axis instead of shrinking to
				// content — load-bearing for "ScrollArea inside a fixed-
				// size panel" patterns where we want the panel size to
				// stay stable as the inner content collapses/expands.
				BeginMethod("autoShrink").Arg("horiz", ctabb.B).Arg("vert", ctabb.B).
				CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.auto_shrink([horiz, vert]);\n")).
				EndMethod().
				Build()...).
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(rustClientCode("egui::ScrollArea::neither();\n")).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{Instance}}.show({{EguiUiOptionalOuter}}.as_mut().unwrap(), |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())

	{
		blocks = append(blocks, idl.NewBuilderFactoryNode("horizontal").
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(ir.EmptyCode).
			WithApplyCodeClientRust(
				rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().horizontal(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
		blocks = append(blocks, idl.NewBuilderFactoryNode("horizontalCentered").
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(ir.EmptyCode).
			WithApplyCodeClientRust(
				rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().horizontal_centered(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
		blocks = append(blocks, idl.NewBuilderFactoryNode("horizontalTop").
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(ir.EmptyCode).
			WithApplyCodeClientRust(
				rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().horizontal_top(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
		blocks = append(blocks, idl.NewBuilderFactoryNode("horizontalWrapped").
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(ir.EmptyCode).
			WithApplyCodeClientRust(
				rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().horizontal_top(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
		blocks = append(blocks, idl.NewBuilderFactoryNode("vertical").
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(ir.EmptyCode).
			WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().vertical(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
		blocks = append(blocks, idl.NewBuilderFactoryNode("verticalCentered").
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(ir.EmptyCode).
			WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().vertical_centered(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
		blocks = append(blocks, idl.NewBuilderFactoryNode("verticalCenteredJustified").
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(ir.EmptyCode).
			WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().vertical_centered_justified(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	}
	// tintedScope — paints a colored background fill at the current ui's
	// max_rect before running the inner block. Cheaper than Frame when all
	// you want is a row/cell tint (Frame allocates a child ui sized to its
	// content, which doesn't fill the parent's available area without
	// extra coercion). Useful inside table cells where you want to colour
	// the entire cell rect behind the cell content (egui_extras gives each
	// cell ui a max_rect equal to the column rect).
	blocks = append(blocks, idl.NewBuilderFactoryNode("tintedScope").
		WithIdentityId(true).
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("col", ctabb.U32).AsColor().
			Build()).
		AddMethods(idl.NewMethodBuilder().
			// senseClick — adds an `ui.interact(outer_rect, …, Sense::click() | hover())`
			// over the scope after content runs, and pushes the resulting
			// ResponseFlags to r7 keyed by the tinted scope's id. Without
			// this method the scope is purely decorative (paint + body).
			BeginMethod("senseClick").
			CodeClientRust(rustClientCode("sense_click = true;\n")).EndMethod().
			// stroke — paints a rect_stroke around the (margin-shrunk)
			// outer rect after the fill and before the content, producing
			// a coloured outline of the scope. Caller may pass a
			// transparent fill in the constructor to get a border-only
			// look.
			BeginMethod("stroke").Arg("width", ctabb.F32).Arg("strokeCol", ctabb.U32).AsColor().
			CodeClientRust(rustClientCode("stroke_width = width;\nstroke_col_u32 = stroke_col;\nstroke_set = true;\n")).EndMethod().
			// outerMargin — gap (in points) between the cell's max_rect
			// edge and the painted fill / stroke / click rect. Default 0.
			BeginMethod("outerMargin").Arg("width", ctabb.F32).
			CodeClientRust(rustClientCode("outer_margin = width;\n")).EndMethod().
			// innerMargin — additional inset (in points) between the
			// painted outer rect and the rect handed to the inner ui
			// builder. Default 0. With both margins 0 the behaviour
			// matches the unadorned tintedScope (no scope_builder).
			BeginMethod("innerMargin").Arg("width", ctabb.F32).
			CodeClientRust(rustClientCode("inner_margin = width;\n")).EndMethod().
			Build()...).
		WithSettingImmediate(true).
		WithSettingRetained(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(rustClientCode(`0u8;
let mut sense_click: bool = false;
let mut stroke_set: bool = false;
let mut stroke_width: f32 = 0.0;
let mut stroke_col_u32: u32 = 0;
let mut outer_margin: f32 = 0.0;
let mut inner_margin: f32 = 0.0;
`)).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						let cell_rect = ui.max_rect();
						let outer_rect = cell_rect.shrink(outer_margin);
						let fill_col = color32_from_rgba_u32(col);
						if fill_col.a() > 0 {
							ui.painter().rect_filled(outer_rect, 0.0, fill_col);
						}
						if stroke_set && stroke_width > 0.0 {
							let stroke = egui::Stroke::new(stroke_width, color32_from_rgba_u32(stroke_col_u32));
							ui.painter().rect_stroke(outer_rect, 0.0, stroke, egui::StrokeKind::Inside);
						}
						if outer_margin == 0.0 && inner_margin == 0.0 {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						} else {
							let inner_rect = outer_rect.shrink(inner_margin);
							ui.scope_builder(
								egui::UiBuilder::new().max_rect(inner_rect),
								|ui| {
									let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
								},
							);
						}
						if sense_click {
							let response = ui.interact(
								outer_rect,
								egui::Id::new({{Id}}.value()).with("tinted_scope_sense"),
								egui::Sense::click() | egui::Sense::hover(),
							);
							let mut resp_flags = ResponseFlags::empty();
							resp_flags.populate(&response);
							self.r7_push({{Id}}.value(), resp_flags);
						}
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("group").
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().group(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("scope").
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().scope(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("indent").
		WithIdentityId(true).
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().indent({{Id}}, |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("pushId").
		WithIdentityId(true).
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().push_id({{Id}}, |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("enabledUi").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("enabled", ctabb.B).Build()).
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().add_enabled_ui(enabled, |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("menuBar").
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(rustClientCode("egui::MenuBar::new();")).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{Instance}}.ui({{EguiUiOptionalOuter}}.as_mut().unwrap(), |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("menuButton").
		WithSettingBlockIterator(true).
		AddArguments(idl.NewArgumentsBuilder().EvaluatedArg("atoms", structAtoms()).Build()).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let retr = {{EguiUiOptionalOuter}}.as_mut().unwrap().menu_button(atoms, |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
						if retr.inner.is_none() {
							self.interpret_outer({{EguiContext}}, &mut None)?;
						}
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("allocateUiAtRect").
		AddArguments(idl.NewArgumentsBuilder().
			PlainArg("minX", ctabb.F32).PlainArg("minY", ctabb.F32).
			PlainArg("maxX", ctabb.F32).PlainArg("maxY", ctabb.F32).
			Build()).
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let parent_ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						let origin = parent_ui.min_rect().min;
						parent_ui.scope_builder(
							egui::UiBuilder::new().max_rect(egui::Rect::from_min_max(
								egui::pos2(origin.x + min_x, origin.y + min_y),
								egui::pos2(origin.x + max_x, origin.y + max_y),
							)),
							|ui| {
								let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
							},
						);
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	blocks = append(blocks, idl.NewBuilderFactoryNode("uiWithLayout").
		WithSettingBlockIterator(true).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("mainDirLeftToRight").CodeClientRust(rustClientCode("layout.main_dir = egui::Direction::LeftToRight;")).EndMethod().
			BeginMethod("mainDirRightToLeft").CodeClientRust(rustClientCode("layout.main_dir = egui::Direction::RightToLeft;")).EndMethod().
			BeginMethod("mainDirTopDown").CodeClientRust(rustClientCode("layout.main_dir = egui::Direction::TopDown;")).EndMethod().
			BeginMethod("mainDirBottomUp").CodeClientRust(rustClientCode("layout.main_dir = egui::Direction::BottomUp;")).EndMethod().
			BeginMethod("mainWrap").Arg("wrap", ctabb.B).CodeClientRust(rustClientCode("layout.main_wrap = wrap;")).EndMethod().
			BeginMethod("mainJustify").Arg("justify", ctabb.B).CodeClientRust(rustClientCode("layout.main_justify = justify;")).EndMethod().
			BeginMethod("crossAlignMin").CodeClientRust(rustClientCode("layout.cross_align = egui::Align::Min;")).EndMethod().
			BeginMethod("crossAlignCenter").CodeClientRust(rustClientCode("layout.cross_align = egui::Align::Center;")).EndMethod().
			BeginMethod("crossAlignMax").CodeClientRust(rustClientCode("layout.cross_align = egui::Align::Max;")).EndMethod().
			BeginMethod("crossJustify").Arg("justify", ctabb.B).CodeClientRust(rustClientCode("layout.cross_justify = justify;")).EndMethod().
			Build()...).
		WithConstructionCodeClientRust(rustClientCode(`false;
let mut layout = egui::Layout::default();`)).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{EguiUiOptionalOuter}}.as_mut().unwrap().with_layout(layout, |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	for _, item := range []struct {
		suffix    string
		applyCode string
	}{
		{
			applyCode: `
					if {{EguiUiOptionalOuter}}.is_some() {
						{{Instance}}.show({{EguiUiOptionalOuter}}.as_mut().unwrap(), |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`,
			suffix: "Inside",
		},
		{
			// egui 0.35 removed `Panel::show(&Context)`; a panel now renders
			// inside a `Ui`. The root variant shows into the top-level `Ui`
			// that `interpret_commands_outer` threads through as
			// `EguiUiOptionalOuter` (its `else` arm still drains the body when
			// no `Ui` is present, keeping the opcode stream balanced).
			applyCode: `
					if {{EguiUiOptionalOuter}}.is_some() {
						{{Instance}}.show({{EguiUiOptionalOuter}}.as_mut().unwrap(), |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`,
			suffix: "",
		},
	} {
		{
			methods := idl.NewMethodBuilder().
				BeginMethod("resizable").Arg("val", ctabb.B).EndMethod().
				BeginMethod("defaultSize").Arg("val", ctabb.F32).EndMethod().
				BeginMethod("exactSize").Arg("val", ctabb.F32).EndMethod().
				Build()
			blocks = append(blocks, idl.NewBuilderFactoryNode(naming.MustBeValidStylableName("panelTop"+item.suffix)).
				AddMethods(methods...).
				WithIdentityId(true).
				WithSettingImmediate(true).
				WithSettingBlockIterator(true).
				WithConstructionCodeClientRust(rustClientCode("egui::Panel::top({{Id}});\n")).
				WithApplyCodeClientRust(rustClientCode(item.applyCode)).Build())
			blocks = append(blocks, idl.NewBuilderFactoryNode(naming.MustBeValidStylableName("panelBottom"+item.suffix)).
				AddMethods(methods...).
				WithIdentityId(true).
				WithSettingImmediate(true).
				WithSettingBlockIterator(true).
				WithConstructionCodeClientRust(rustClientCode("egui::Panel::bottom({{Id}});\n")).
				WithApplyCodeClientRust(rustClientCode(item.applyCode)).Build())
		}
		{
			methods := idl.NewMethodBuilder().
				BeginMethod("resizable").Arg("val", ctabb.B).EndMethod().
				BeginMethod("defaultSize").Arg("val", ctabb.F32).EndMethod().
				BeginMethod("exactSize").Arg("val", ctabb.F32).EndMethod().
				Build()
			blocks = append(blocks, idl.NewBuilderFactoryNode(naming.MustBeValidStylableName("panelLeft"+item.suffix)).
				AddMethods(methods...).
				WithIdentityId(true).
				WithSettingImmediate(true).
				WithSettingBlockIterator(true).
				WithConstructionCodeClientRust(rustClientCode("egui::Panel::left({{Id}});\n")).
				WithApplyCodeClientRust(rustClientCode(item.applyCode)).Build())
			blocks = append(blocks, idl.NewBuilderFactoryNode(naming.MustBeValidStylableName("panelRight"+item.suffix)).
				AddMethods(methods...).
				WithIdentityId(true).
				WithSettingImmediate(true).
				WithSettingBlockIterator(true).
				WithConstructionCodeClientRust(rustClientCode("egui::Panel::right({{Id}});\n")).
				WithApplyCodeClientRust(rustClientCode(item.applyCode)).Build())
		}
		// egui::CentralPanel has no id and no size methods — it fills the
		// remaining area. egui 0.35 unified panel showing: CentralPanel::show
		// now takes a `&mut Ui` like the side/top panels, so both the root and
		// inside variants use the shared item.applyCode (show into
		// EguiUiOptionalOuter when present; the root Ui comes from
		// interpret_commands_outer).
		{
			centralApply := item.applyCode
			blocks = append(blocks, idl.NewBuilderFactoryNode(naming.MustBeValidStylableName("panelCentral"+item.suffix)).
				WithSettingImmediate(true).
				WithSettingBlockIterator(true).
				WithConstructionCodeClientRust(rustClientCode("egui::CentralPanel::default();\n")).
				WithApplyCodeClientRust(rustClientCode(centralApply)).Build())
		}
	}
	{
		// Note: this applyCode is shared between Grid below and any other
		// `Instance.show(ui, |ui| ...)`-shaped block iterators. The else-arm
		// drains body opcodes when u=None so the Rust drain reads a balanced
		// stream and doesn't terminate early on an inner `End` (ADR-0012).
		applyCode := rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						{{Instance}}.show({{EguiUiOptionalOuter}}.as_mut().unwrap(), |ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						});
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)
		methods := idl.NewMethodBuilder().
			BeginMethod("numColumns").Arg("val", ctabb.U32).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.num_columns(val as usize);\n")).EndMethod().
			BeginMethod("striped").Arg("val", ctabb.B).EndMethod().
			BeginMethod("minColWidth").Arg("val", ctabb.F32).EndMethod().
			BeginMethod("minRowHeight").Arg("val", ctabb.F32).EndMethod().
			BeginMethod("maxColWidth").Arg("val", ctabb.F32).EndMethod().
			BeginMethod("startRow").Arg("val", ctabb.U64).CodeClientRust(rustClientCode("{{Instance}} = {{Instance}}.start_row(val as usize);\n")).EndMethod().
			Build()
		blocks = append(blocks, idl.NewBuilderFactoryNode("grid").
			AddMethods(methods...).
			WithIdentityId(true).
			WithSettingImmediate(true).
			WithSettingBlockIterator(true).
			WithConstructionCodeClientRust(rustClientCode("egui::Grid::new({{Id}});\n")).
			WithApplyCodeClientRust(applyCode).Build())
	}
	// hoverText wraps its body in a `ui.scope(...)` and attaches a plain-text
	// tooltip to an overlay interact-widget placed over the scope's rect.
	//
	// Why the extra `ui.interact(...)` after the scope: egui's `ui.scope`
	// registers its own response widget at child-ui construction (before the
	// body runs), so in the back-to-front hit-test order the scope sits
	// *behind* its children. The interaction snapshot only marks a
	// non-interactive widget as hovered when it lies *above* the topmost
	// interactive widget, so the scope's response reports hovered=false
	// while the pointer is over an inner button and `on_hover_text` returns
	// without scheduling a tooltip. Re-interacting after the body runs adds
	// a widget to the top of the stack whose response reports hover
	// correctly. Same technique as the Frame block uses for senseClick.
	blocks = append(blocks, idl.NewBuilderFactoryNode("hoverText").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
		WithSettingImmediate(true).
		WithSettingBlockIterator(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						let scope_resp = ui.scope(|ui| {
							let _ = self.interpret_outer_logged({{EguiContext}}, &mut Some(ui));
						}).response;
						let hover_resp = ui.interact(
							scope_resp.rect,
							scope_resp.id.with("imzero2_hover_text"),
							egui::Sense::hover(),
						);
						let _ = hover_resp.on_hover_text(text);
					} else {
						self.interpret_outer({{EguiContext}}, &mut None)?;
					}
`)).Build())
	// hoverUi captures BOTH a tooltip body and a target body as deferred
	// blocks. At render time the target body runs inside a `ui.scope(...)` and
	// an overlay interact-widget on top of the scope's rect is decorated with
	// `on_hover_ui(|ui| replay_tip)`. Each block map has a single entry keyed
	// by 0; the framework has no zero-key variant, so we reuse the u32-keyed
	// map with a dummy key. See hoverText for why the explicit ui.interact is
	// needed.
	blocks = append(blocks, idl.NewBuilderFactoryNode("hoverUi").
		WithDeferredBlockMap("tip", ctabb.U32).
		WithDeferredBlockMap("target", ctabb.U32).
		WithSettingImmediate(true).
		WithConstructionCodeClientRust(ir.EmptyCode).
		WithApplyCodeClientRust(rustClientCode(`
					if {{EguiUiOptionalOuter}}.is_some() {
						let mut tip_blocks = self.io.read_deferred_block_map_u32()?;
						let mut target_blocks = self.io.read_deferred_block_map_u32()?;
						let tip = tip_blocks.drain().next().map(|(_, v)| v);
						let target = target_blocks.drain().next().map(|(_, v)| v);
						let ui = {{EguiUiOptionalOuter}}.as_mut().unwrap();
						let scope_resp = ui.scope(|ui| {
							if let Some(block) = &target {
								let _ = self.replay_deferred_block_logged({{EguiContext}}, ui, block);
							}
						}).response;
						let hover_resp = ui.interact(
							scope_resp.rect,
							scope_resp.id.with("imzero2_hover_ui"),
							egui::Sense::hover(),
						);
						if let Some(block) = tip {
							let ctx_cloned = hover_resp.ctx.clone();
							let _ = hover_resp.on_hover_ui(|ui| {
								let _ = self.replay_deferred_block_logged(&ctx_cloned, ui, &block);
							});
						}
					} else {
						self.io.skip_deferred_block_map_u32()?;
						self.io.skip_deferred_block_map_u32()?;
					}
`)).
		WithReturnType(structHoverUiDummy()).
		Build())
	blocks = append(blocks, definitionsTableBlock()...)

	for _, w := range blocks {
		if w.ReturnType == nil {
			w.ReturnType = traitBlock()
		}
	}
	return
}
