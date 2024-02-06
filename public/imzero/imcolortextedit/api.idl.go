//go:build fffi_idl_code

package imcolortextedit

import "github.com/stergiotis/boxer/public/imzero/imgui"

func NewImColorEditorForeignPtr() (ptr ImColorEditorForeignPtr) {
	_ = `ptr = (uintptr_t)(new TextEditor())`
	return
}
func (foreignptr ImColorEditorForeignPtr) Destroy() {
	_ = `delete ((TextEditor*) foreignptr)`
}

func (foreignptr ImColorEditorForeignPtr) Render(title string) {
	_ = `((TextEditor*)foreignptr)->Render(title)`
}
func (foreignptr ImColorEditorForeignPtr) RenderV(title string, aSize imgui.ImVec2, aBorder bool) {
	_ = `((TextEditor*)foreignptr)->Render(title, aSize, aBorder)`
}
func (foreignptr ImColorEditorForeignPtr) SetText(text string) {
	_ = `((TextEditor*)foreignptr)->SetText(text)`
}

func (foreignptr ImColorEditorForeignPtr) GetText() (text string) {
	_ = `auto text = ((TextEditor*)foreignptr)->GetText()`
	return
}
func (foreignptr ImColorEditorForeignPtr) GetSelectedText() (text string) {
	_ = `auto text = ((TextEditor*)foreignptr)->GetSelectedText()`
	return
}
func (foreignptr ImColorEditorForeignPtr) GetCurrentLineText() (text string) {
	_ = `auto text = ((TextEditor*)foreignptr)->GetCurrentLineText()`
	return
}
func (foreignptr ImColorEditorForeignPtr) GetTotalLines() (lines int) {
	_ = `lines = ((TextEditor*)foreignptr)->GetTotalLines()`
	return
}
func (foreignptr ImColorEditorForeignPtr) IsOverwrite() (overwrite bool) {
	_ = `overwrite = ((TextEditor*)foreignptr)->IsOverwrite()`
	return
}
func (foreignptr ImColorEditorForeignPtr) SetReadOnly(v bool) {
	_ = `((TextEditor*)foreignptr)->SetReadOnly(v)`
}
func (foreignptr ImColorEditorForeignPtr) IsReadOnly() (readonly bool) {
	_ = `readonly = ((TextEditor*)foreignptr)->IsReadOnly()`
	return
}
func (foreignptr ImColorEditorForeignPtr) IsChanged() (text bool, cursor bool) {
	_ = `auto p = ((TextEditor*)foreignptr);
cursor = p->IsCursorPositionChanged();
text = p->IsTextChanged()`
	return
}
func (foreignptr ImColorEditorForeignPtr) IsColorizerEnabled() (enabled bool) {
	_ = `enabled = ((TextEditor*)foreignptr)->IsColorizerEnabled()`
	return
}
func (foreignptr ImColorEditorForeignPtr) SetColorizerEnable(v bool) {
	_ = `((TextEditor*)foreignptr)->SetColorizerEnable(v)`
}

func (foreignptr ImColorEditorForeignPtr) SetHandleMouseInputs(v bool) {
	_ = `((TextEditor*)foreignptr)->SetHandleMouseInputs(v)`
}
func (foreignptr ImColorEditorForeignPtr) IsHandleMouseInputsEnabled() (v bool) {
	_ = `v = ((TextEditor*)foreignptr)->IsHandleMouseInputsEnabled()`
	return
}
func (foreignptr ImColorEditorForeignPtr) SetHandleKeyboardInputs(v bool) {
	_ = `((TextEditor*)foreignptr)->SetHandleKeyboardInputs(v)`
}
func (foreignptr ImColorEditorForeignPtr) IsHandleKeyboardInputsEnabled() (v bool) {
	_ = `v = ((TextEditor*)foreignptr)->IsHandleKeyboardInputsEnabled()`
	return
}
func (foreignptr ImColorEditorForeignPtr) SetImGuiChildIgnored(v bool) {
	_ = `((TextEditor*)foreignptr)->SetImGuiChildIgnored(v)`
}
func (foreignptr ImColorEditorForeignPtr) IsImGuiChildIgnored() (v bool) {
	_ = `v = ((TextEditor*)foreignptr)->IsImGuiChildIgnored()`
	return
}
func (foreignptr ImColorEditorForeignPtr) SetShowWhitespaces(v bool) {
	_ = `((TextEditor*)foreignptr)->SetShowWhitespaces(v)`
}
func (foreignptr ImColorEditorForeignPtr) IsShowingWhitespaces() (v bool) {
	_ = `v = ((TextEditor*)foreignptr)->IsShowingWhitespaces()`
	return
}
func (foreignptr ImColorEditorForeignPtr) SetTabSize(v int) {
	_ = `((TextEditor*)foreignptr)->SetTabSize(v)`
}
func (foreignptr ImColorEditorForeignPtr) GetTabSize() (v int) {
	_ = `v = ((TextEditor*)foreignptr)->GetTabSize()`
	return
}

func (foreignptr ImColorEditorForeignPtr) InsertText(text string) {
	_ = `((TextEditor*)foreignptr)->InsertText(text)`
}
func (foreignptr ImColorEditorForeignPtr) MoveUp() {
	_ = `((TextEditor*)foreignptr)->MoveUp()`
}
func (foreignptr ImColorEditorForeignPtr) MoveUpV(amount int, selectP bool) {
	_ = `((TextEditor*)foreignptr)->MoveUp(amount,selectP)`
}
func (foreignptr ImColorEditorForeignPtr) MoveDown() {
	_ = `((TextEditor*)foreignptr)->MoveDown()`
}
func (foreignptr ImColorEditorForeignPtr) MoveDownV(amount int, selectP bool) {
	_ = `((TextEditor*)foreignptr)->MoveDown(amount,selectP)`
}
func (foreignptr ImColorEditorForeignPtr) MoveLeft() {
	_ = `((TextEditor*)foreignptr)->MoveLeft()`
}
func (foreignptr ImColorEditorForeignPtr) MoveLeftV(amount int, selectP bool, wordMode bool) {
	_ = `((TextEditor*)foreignptr)->MoveLeft(amount,selectP,wordMode)`
}
func (foreignptr ImColorEditorForeignPtr) MoveRight() {
	_ = `((TextEditor*)foreignptr)->MoveRight()`
}
func (foreignptr ImColorEditorForeignPtr) MoveRightV(amount int, selectP bool, wordMode bool) {
	_ = `((TextEditor*)foreignptr)->MoveRight(amount,selectP,wordMode)`
}
func (foreignptr ImColorEditorForeignPtr) MoveTop() {
	_ = `((TextEditor*)foreignptr)->MoveTop()`
}
func (foreignptr ImColorEditorForeignPtr) MoveTopV(selectP bool) {
	_ = `((TextEditor*)foreignptr)->MoveTop(selectP)`
}
func (foreignptr ImColorEditorForeignPtr) MoveBottom() {
	_ = `((TextEditor*)foreignptr)->MoveBottom()`
}
func (foreignptr ImColorEditorForeignPtr) MoveBottomV(selectP bool) {
	_ = `((TextEditor*)foreignptr)->MoveBottom(selectP)`
}
func (foreignptr ImColorEditorForeignPtr) MoveHome() {
	_ = `((TextEditor*)foreignptr)->MoveHome()`
}
func (foreignptr ImColorEditorForeignPtr) MoveHomeV(selectP bool) {
	_ = `((TextEditor*)foreignptr)->MoveHome(selectP)`
}
func (foreignptr ImColorEditorForeignPtr) MoveEnd() {
	_ = `((TextEditor*)foreignptr)->MoveEnd()`
}
func (foreignptr ImColorEditorForeignPtr) MoveEndV(selectP bool) {
	_ = `((TextEditor*)foreignptr)->MoveEnd(selectP)`
}

func (foreignptr ImColorEditorForeignPtr) SelectWordUnderCursor() {
	_ = `((TextEditor*)foreignptr)->SelectWordUnderCursor()`
}
func (foreignptr ImColorEditorForeignPtr) SelectAll() {
	_ = `((TextEditor*)foreignptr)->SelectAll()`
}
func (foreignptr ImColorEditorForeignPtr) HasSelection() (has bool) {
	_ = `has = ((TextEditor*)foreignptr)->HasSelection()`
	return
}

func (foreignptr ImColorEditorForeignPtr) Copy() {
	_ = `((TextEditor*)foreignptr)->Copy()`
}
func (foreignptr ImColorEditorForeignPtr) Cut() {
	_ = `((TextEditor*)foreignptr)->Cut()`
}
func (foreignptr ImColorEditorForeignPtr) Paste() {
	_ = `((TextEditor*)foreignptr)->Paste()`
}
func (foreignptr ImColorEditorForeignPtr) Delete() {
	_ = `((TextEditor*)foreignptr)->Delete()`
}

func (foreignptr ImColorEditorForeignPtr) CanUndo() (can bool) {
	_ = `can = ((TextEditor*)foreignptr)->CanUndo()`
	return
}
func (foreignptr ImColorEditorForeignPtr) CanRedo() (can bool) {
	_ = `can = ((TextEditor*)foreignptr)->CanRedo()`
	return
}

func (foreignptr ImColorEditorForeignPtr) Undo() {
	_ = `((TextEditor*)foreignptr)->Undo()`
}
func (foreignptr ImColorEditorForeignPtr) UndoV(steps int) {
	_ = `((TextEditor*)foreignptr)->Undo(steps)`
}
func (foreignptr ImColorEditorForeignPtr) Redo() {
	_ = `((TextEditor*)foreignptr)->Redo()`
}
func (foreignptr ImColorEditorForeignPtr) RedoV(steps int) {
	_ = `((TextEditor*)foreignptr)->Redo(steps)`
}

/*
 */
