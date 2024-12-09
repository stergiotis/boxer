//go:build fffi_idl_code

package imgui

func NewHexEditor() (r ImHexEditorPtr) {
	_ = `r = (uintptr_t)(new HexEditor())`
	return
}

func (foreignptr ImHexEditorPtr) Destroy() {
	_ = `delete((HexEditor*)foreignptr)`
}

// readOnly           disable any editing.
// cols               number of columns to display.
// showOptions        display options button/context menu. when disabled, options will be locked unless you provide your own UI for them.
// showDataPreview    display a footer previewing the decimal/binary/hex/float representation of the currently selected bytes.
// showHexII          display values in HexII representation instead of regular hexadecimal: hide null/zero bytes, ascii values as ".X".
// showAscii          display ASCII representation on the right side.
// greyOutZeroes      display null/zero bytes using the TextDisabled color.
// upperCaseHex       display hexadecimal values as "FF" instead of "ff".
// midColsCount       set to 0 to disable extra spacing between every mid-cols.
// addrDigitsCount    number of addr digits to display (default calculated based on maximum displayed addr).
// footerExtraHeight  space to reserve at the bottom of the widget to add custom widgets
// highlightColor     background color of highlighted bytes.
func (foreignptr ImHexEditorPtr) GetSettings() (readOnly bool, cols int, showOptions bool, showDataPreview bool, showHexII bool, showAscii bool, greyOutZeroes bool, upperCaseHex bool, midColsCount int, addrDigitsCount int, footerExtraHeight float32, highlightColor uint32) {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto t = e->memEditor;
#define ASSIGN(l,r) ((l) = (r))
ASSIGN(readOnly, t->ReadOnly);
ASSIGN(cols, t->Cols);
ASSIGN(showOptions, t->OptShowOptions);
ASSIGN(showDataPreview, t->OptShowDataPreview);
ASSIGN(showHexII, t->OptShowHexII);
ASSIGN(showAscii, t->OptShowAscii);
ASSIGN(greyOutZeroes, t->OptGreyOutZeroes);
ASSIGN(upperCaseHex, t->OptUpperCaseHex);
ASSIGN(midColsCount, t->OptMidColsCount);
ASSIGN(addrDigitsCount, t->OptAddrDigitsCount);
ASSIGN(footerExtraHeight, t->OptFooterExtraHeight);
ASSIGN(highlightColor, t->HighlightColor);
#undef ASSIGN
`
	return
}

// readOnly           disable any editing.
// cols               number of columns to display.
// showOptions        display options button/context menu. when disabled, options will be locked unless you provide your own UI for them.
// showDataPreview    display a footer previewing the decimal/binary/hex/float representation of the currently selected bytes.
// showHexII          display values in HexII representation instead of regular hexadecimal: hide null/zero bytes, ascii values as ".X".
// showAscii          display ASCII representation on the right side.
// greyOutZeroes      display null/zero bytes using the TextDisabled color.
// upperCaseHex       display hexadecimal values as "FF" instead of "ff".
// midColsCount       set to 0 to disable extra spacing between every mid-cols.
// addrDigitsCount    number of addr digits to display (default calculated based on maximum displayed addr).
// footerExtraHeight  space to reserve at the bottom of the widget to add custom widgets
// highlightColor     background color of highlighted bytes.
func (foreignptr ImHexEditorPtr) SetSettings(readOnly bool, cols int, showOptions bool, showDataPreview bool, showHexII bool, showAscii bool, greyOutZeroes bool, upperCaseHex bool, midColsCount int, addrDigitsCount int, footerExtraHeight float32, highlightColor uint32) {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto t = e->memEditor;
#define ASSIGN(l,r) ((r) = (l))
ASSIGN(readOnly, t->ReadOnly);
ASSIGN(cols, t->Cols);
ASSIGN(showOptions, t->OptShowOptions);
ASSIGN(showDataPreview, t->OptShowDataPreview);
ASSIGN(showHexII, t->OptShowHexII);
ASSIGN(showAscii, t->OptShowAscii);
ASSIGN(greyOutZeroes, t->OptGreyOutZeroes);
ASSIGN(upperCaseHex, t->OptUpperCaseHex);
ASSIGN(midColsCount, t->OptMidColsCount);
ASSIGN(addrDigitsCount, t->OptAddrDigitsCount);
ASSIGN(footerExtraHeight, t->OptFooterExtraHeight);
ASSIGN(highlightColor, t->HighlightColor);
#undef ASSIGN
`
}

func (foreignptr ImHexEditorPtr) GotoAddrAndHighlight(addrMin Size_t, addrMax Size_t) {
	_ = `((HexEditor*)foreignptr)->memEditor->GotoAddrAndHighlight(addrMin,addrMax)`
}

func (foreignptr ImHexEditorPtr) DrawWindow(title string, data []byte) {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto t = e->memEditor;
t->DrawWindow(title,e->data,e->data_length);
`
}

func (foreignptr ImHexEditorPtr) DrawWindowV(title string, baseDisplayAddr Size_t) {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto t = e->memEditor;
t->DrawWindow(title,e->data,e->data_length,baseDisplayAddr);
`
}

func (foreignptr ImHexEditorPtr) DrawContents() {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto t = e->memEditor;
t->DrawContents(e->data,e->data_length);
`
}

func (foreignptr ImHexEditorPtr) DrawContentV(baseDisplayAddr Size_t) {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto t = e->memEditor;
t->DrawContents(e->data,e->data_length,baseDisplayAddr);
`
}

func (foreignptr ImHexEditorPtr) SetData(data []byte) {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto sz = getSliceLength(data);
e->data_length = sz;
if(e->data != nullptr) {
   free(e->data);
}
if(sz != 0) {
   e->data = (decltype(e->data))malloc(sz);
   assert(e->data != nullptr);
   memcpy(e->data,data,sz);
} else {
   e->data = nullptr;
}
`
}

func (foreignptr ImHexEditorPtr) GetData() (data []byte) {
	_ = `
auto e = ((HexEditor*)foreignptr);
auto data = (const uint8_t*)e->data;
auto data_len = e->data_length;
`
	return
}
