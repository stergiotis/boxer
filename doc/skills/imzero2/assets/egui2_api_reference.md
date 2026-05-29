---
type: reference
audience: agent reading this skill asset
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Machine-generated FFFI2 widget-catalogue overview; regenerated from the IDL on every `./generate.sh` run.

# fffi2 API Reference

## Summary

| Name | Type | Identity | Plain Args | Eval Args | Methods | Features |
|------|------|----------|------------|-----------|---------|----------|
| AddSpace | Procedural | No | 1 | 0 | - | - |
| AllocateUiAtRect | BuilderFactory | No | 4 | 0 | 0 | Immediate, BlockIterator |
| AnimateBoolResponsive | Procedural | No | 2 | 0 | - | - |
| AnimateBoolWithTime | Procedural | No | 3 | 0 | - | - |
| AnimateValueWithTime | Procedural | No | 3 | 0 | - | - |
| Atoms | BuilderFactory | No | 0 | 0 | 20 | Retained |
| Button | BuilderFactory | Yes | 0 | 1 | 8 | Immediate, Retained |
| CaptureAvailableSize | Procedural | No | 0 | 0 | - | - |
| CaptureUiRect | Procedural | No | 1 | 0 | - | - |
| Checkbox | BuilderFactory | Yes | 2 | 0 | 1 | Immediate |
| CodeView | BuilderFactory | Yes | 0 | 1 | 4 | Immediate, Retained |
| CodeViewJob | BuilderFactory | No | 1 | 0 | 1 | Retained |
| CollapsingHeader | BuilderFactory | Yes | 0 | 1 | 3 | Immediate, Retained, BlockIterator |
| Color | BuilderFactory | No | 0 | 0 | 34 | Retained |
| ComboBox | BuilderFactory | Yes | 0 | 2 | 4 | Immediate, Retained, BlockIterator |
| ContextInspectionUi | Procedural | No | 0 | 0 | - | - |
| ContextSendViewPortCommandClose | Procedural | No | 0 | 0 | - | - |
| DatePickerButton | BuilderFactory | Yes | 1 | 0 | 7 | Immediate, Retained |
| DateTimePickerButton | BuilderFactory | Yes | 1 | 0 | 7 | Immediate, Retained |
| DockAreaRaw | BuilderFactory | Yes | 3 | 0 | 0 | Immediate |
| DragValueF64 | BuilderFactory | Yes | 1 | 0 | 10 | Immediate, Retained |
| DragValueI64 | BuilderFactory | Yes | 1 | 0 | 10 | Immediate, Retained |
| DragValueU64 | BuilderFactory | Yes | 1 | 0 | 10 | Immediate, Retained |
| EnabledUi | BuilderFactory | No | 1 | 0 | 0 | Immediate, BlockIterator |
| End | Procedural | No | 0 | 0 | - | - |
| EndETable | BuilderFactory | Yes | 4 | 0 | 8 | Immediate, Retained |
| EndRow | Procedural | No | 0 | 0 | - | - |
| EtColumn | BuilderFactory | No | 1 | 0 | 3 | Immediate, Retained |
| EtHeaderText | BuilderFactory | No | 1 | 0 | 0 | Immediate, Retained |
| EtRowHeight | BuilderFactory | No | 1 | 0 | 0 | Immediate |
| ExportSvg | Procedural | No | 3 | 0 | - | - |
| ExportSvgWindow | Procedural | Yes | 4 | 0 | - | - |
| FetchF1KeyPressed | Fetcher | No | 0 | 0 | - | - |
| FetchFrameMetrics | Fetcher | No | 0 | 0 | - | - |
| FetchGraphEvents | Fetcher | No | 0 | 0 | - | - |
| FetchGraphMetrics | Fetcher | No | 0 | 0 | - | - |
| FetchGraphSelection | Fetcher | No | 0 | 0 | - | - |
| FetchR10 | Fetcher | No | 0 | 0 | - | - |
| FetchR14CanvasPointer | Fetcher | No | 0 | 0 | - | - |
| FetchR15PlotPointer | Fetcher | No | 0 | 0 | - | - |
| FetchR15WalkersCamera | Fetcher | No | 0 | 0 | - | - |
| FetchR16ScrollDelta | Fetcher | No | 0 | 0 | - | - |
| FetchR17Modifiers | Fetcher | No | 0 | 0 | - | - |
| FetchR18AvailableSize | Fetcher | No | 0 | 0 | - | - |
| FetchR19ZoomDelta | Fetcher | No | 0 | 0 | - | - |
| FetchR20Pointer | Fetcher | No | 0 | 0 | - | - |
| FetchR21UiRects | Fetcher | No | 0 | 0 | - | - |
| FetchR7 | Fetcher | No | 0 | 0 | - | - |
| FetchR9EtPrefetch | Fetcher | No | 0 | 0 | - | - |
| FetchR9F64 | Fetcher | No | 0 | 0 | - | - |
| FetchR9I64 | Fetcher | No | 0 | 0 | - | - |
| FetchR9S | Fetcher | No | 0 | 0 | - | - |
| FetchR9U64 | Fetcher | No | 0 | 0 | - | - |
| FetchSnarlEvents | Fetcher | No | 0 | 0 | - | - |
| Frame | BuilderFactory | Yes | 0 | 0 | 21 | Immediate, Retained, BlockIterator |
| Graph | BuilderFactory | Yes | 0 | 0 | 30 | Immediate, Retained |
| GraphEdge | BuilderFactory | No | 2 | 0 | 2 | Immediate |
| GraphNode | BuilderFactory | No | 2 | 0 | 1 | Immediate |
| Grid | BuilderFactory | Yes | 0 | 0 | 6 | Immediate, BlockIterator |
| Group | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| GuiZoomZoomMenuButtons | Procedural | No | 0 | 0 | - | - |
| H3CellsColored | BuilderFactory | No | 2 | 0 | 2 | Immediate |
| H3Region | BuilderFactory | No | 1 | 0 | 3 | Immediate |
| Horizontal | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| HorizontalCentered | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| HorizontalTop | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| HorizontalWrapped | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| HoverText | BuilderFactory | No | 1 | 0 | 0 | Immediate, BlockIterator |
| HoverUi | BuilderFactory | No | 0 | 0 | 0 | Immediate |
| Hyperlink | BuilderFactory | No | 1 | 0 | 1 | Immediate, Retained |
| HyperlinkTo | BuilderFactory | No | 2 | 0 | 1 | Immediate, Retained |
| Image | BuilderFactory | Yes | 9 | 0 | 0 | Immediate |
| ImageRelease | BuilderFactory | Yes | 0 | 0 | 0 | Immediate |
| Indent | BuilderFactory | Yes | 0 | 0 | 0 | Immediate, BlockIterator |
| Label | BuilderFactory | No | 1 | 0 | 4 | Immediate, Retained |
| LabelAtoms | BuilderFactory | No | 0 | 1 | 3 | Immediate, Retained |
| LabelWidgetText | BuilderFactory | No | 0 | 1 | 0 | Immediate, Retained |
| MapMarker | BuilderFactory | No | 3 | 0 | 3 | Immediate |
| MapPolyline | BuilderFactory | No | 2 | 0 | 2 | Immediate |
| MeasureText | Procedural | No | 4 | 0 | - | - |
| MemoryResetAreas | Procedural | No | 0 | 0 | - | - |
| MenuBar | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| MenuButton | BuilderFactory | No | 0 | 1 | 0 | BlockIterator |
| MoveWindowToTop | Procedural | Yes | 0 | 0 | - | - |
| NewTable | BuilderFactory | Yes | 0 | 0 | 7 | Immediate, Retained |
| NewTableColumn | BuilderFactory | No | 0 | 0 | 8 | Immediate, Retained |
| NewTableRowHeight | BuilderFactory | No | 1 | 0 | 0 | Immediate |
| NodeDir | BuilderFactory | Yes | 0 | 1 | 0 | Immediate, Retained |
| NodeDirClose | BuilderFactory | No | 1 | 0 | 0 | Immediate, Retained |
| NodeLeaf | BuilderFactory | Yes | 0 | 1 | 0 | Immediate, Retained |
| PaintAbsoluteOverlay | Procedural | No | 0 | 0 | - | - |
| PaintArrow | BuilderFactory | No | 6 | 0 | 0 | Immediate |
| PaintCanvas | BuilderFactory | Yes | 2 | 0 | 3 | Immediate, Retained |
| PaintCircleFilled | BuilderFactory | No | 4 | 0 | 0 | Immediate |
| PaintCircleStroke | BuilderFactory | No | 5 | 0 | 0 | Immediate |
| PaintCubicBezier | BuilderFactory | No | 10 | 0 | 0 | Immediate |
| PaintDashedLine | BuilderFactory | No | 8 | 0 | 0 | Immediate |
| PaintLine | BuilderFactory | No | 6 | 0 | 0 | Immediate |
| PaintPolyline | BuilderFactory | No | 4 | 0 | 0 | Immediate |
| PaintRectFilled | BuilderFactory | No | 6 | 0 | 0 | Immediate |
| PaintRectStroke | BuilderFactory | No | 7 | 0 | 0 | Immediate |
| PaintSenseRegion | BuilderFactory | Yes | 4 | 0 | 0 | Immediate |
| PaintText | BuilderFactory | No | 7 | 0 | 1 | Immediate |
| PanelBottom | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| PanelBottomInside | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| PanelCentral | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| PanelCentralInside | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| PanelLeft | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| PanelLeftInside | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| PanelRight | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| PanelRightInside | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| PanelTop | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| PanelTopInside | BuilderFactory | Yes | 0 | 0 | 3 | Immediate, BlockIterator |
| Passthrough | Procedural | Yes | 1 | 0 | - | - |
| Plot | BuilderFactory | Yes | 0 | 0 | 27 | Immediate, Retained |
| PlotBars | BuilderFactory | No | 3 | 0 | 4 | Immediate |
| PlotBoxes | BuilderFactory | No | 11 | 0 | 4 | Immediate |
| PlotHLine | BuilderFactory | No | 2 | 0 | 3 | Immediate |
| PlotLine | BuilderFactory | No | 3 | 0 | 4 | Immediate |
| PlotPolygon | BuilderFactory | No | 6 | 0 | 1 | Immediate |
| PlotScatter | BuilderFactory | No | 3 | 0 | 5 | Immediate |
| PlotText | BuilderFactory | No | 4 | 0 | 1 | Immediate |
| PlotVLine | BuilderFactory | No | 2 | 0 | 3 | Immediate |
| PrepareNextFrame | Procedural | No | 0 | 0 | - | - |
| ProgressBar | BuilderFactory | No | 1 | 0 | 7 | Immediate, Retained |
| PushId | BuilderFactory | Yes | 0 | 0 | 0 | Immediate, BlockIterator |
| RadioButton | BuilderFactory | Yes | 1 | 1 | 0 | Immediate |
| RequestRepaint | Procedural | No | 0 | 0 | - | - |
| RequestRepaintAfter | Procedural | No | 1 | 0 | - | - |
| RequestScreenshot | Procedural | No | 1 | 0 | - | - |
| RequestScreenshotRect | Procedural | No | 5 | 0 | - | - |
| ScalarSize | BuilderFactory | No | 0 | 0 | 2 | Retained |
| Scope | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| ScrollArea | BuilderFactory | No | 0 | 0 | 4 | Immediate, BlockIterator |
| ScrollToCursor | Procedural | No | 1 | 0 | - | - |
| ScrollingTexture | BuilderFactory | Yes | 9 | 0 | 0 | Immediate |
| ScrollingTextureRelease | BuilderFactory | Yes | 0 | 0 | 0 | Immediate |
| SelectableLabel | BuilderFactory | Yes | 2 | 0 | 0 | Immediate |
| Separator | BuilderFactory | No | 0 | 0 | 5 | Immediate |
| SetAnimationFreeze | Procedural | No | 1 | 0 | - | - |
| SetWindowCollapsed | Procedural | Yes | 1 | 0 | - | - |
| ShowDebugTools | Procedural | No | 0 | 0 | - | - |
| ShowPuffinProfiler | Procedural | No | 0 | 0 | - | - |
| SliderF64 | BuilderFactory | Yes | 3 | 0 | 19 | Immediate, Retained |
| SliderI64 | BuilderFactory | Yes | 3 | 0 | 19 | Immediate, Retained |
| SliderU64 | BuilderFactory | Yes | 3 | 0 | 19 | Immediate, Retained |
| SnarlConnection | BuilderFactory | No | 4 | 0 | 0 | Immediate |
| SnarlEditor | BuilderFactory | Yes | 0 | 0 | 9 | Immediate, Retained |
| SnarlNode | BuilderFactory | No | 5 | 0 | 2 | Immediate |
| SnarlPin | BuilderFactory | No | 5 | 0 | 0 | Immediate |
| Spinner | BuilderFactory | No | 0 | 0 | 1 | Immediate |
| Table | BuilderFactory | Yes | 2 | 0 | 5 | Immediate, Retained |
| TableCellRichText | BuilderFactory | No | 0 | 1 | 0 | Immediate, Retained |
| TableCellText | BuilderFactory | No | 1 | 0 | 0 | Immediate, Retained |
| TableColumn | BuilderFactory | No | 0 | 0 | 8 | Immediate, Retained |
| TableHeaderText | BuilderFactory | No | 1 | 0 | 0 | Immediate, Retained |
| TextEdit | BuilderFactory | Yes | 2 | 0 | 11 | Immediate |
| TimeRangePicker | BuilderFactory | Yes | 2 | 0 | 4 | Immediate, Retained |
| TintedScope | BuilderFactory | Yes | 1 | 0 | 4 | Immediate, Retained, BlockIterator |
| Tree | BuilderFactory | Yes | 0 | 0 | 0 | Immediate |
| UiDisable | Procedural | No | 0 | 0 | - | - |
| UiSetHeight | Procedural | No | 1 | 0 | - | - |
| UiSetMaxHeight | Procedural | No | 1 | 0 | - | - |
| UiSetMaxWidth | Procedural | No | 1 | 0 | - | - |
| UiSetMinHeight | Procedural | No | 1 | 0 | - | - |
| UiSetMinWidth | Procedural | No | 1 | 0 | - | - |
| UiSetWidth | Procedural | No | 1 | 0 | - | - |
| UiWithLayout | BuilderFactory | No | 0 | 0 | 10 | BlockIterator |
| VectorSize | BuilderFactory | No | 0 | 0 | 1 | Retained |
| Vertical | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| VerticalCentered | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| VerticalCenteredJustified | BuilderFactory | No | 0 | 0 | 0 | Immediate, BlockIterator |
| WalkersMap | BuilderFactory | Yes | 3 | 0 | 10 | Immediate, Retained |
| WarnIfDebugBuild | Procedural | No | 0 | 0 | - | - |
| WidgetText | BuilderFactory | No | 0 | 0 | 1 | Retained |
| WidgetsGlobalThemePreferenceButtons | Procedural | No | 0 | 0 | - | - |
| Window | BuilderFactory | Yes | 0 | 1 | 15 | Immediate, BlockIterator |


## BuilderFactory Nodes

### AllocateUiAtRect

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| minX | plain | f32 |
| minY | plain | f32 |
| maxX | plain | f32 |
| maxY | plain | f32 |

#### Return Type

Block

---

### Atoms

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Retained

#### Builder Methods

- **Text**(val: s)
- **RichText**(val: s)
- **RichTextColored**(val: s)
- **EndRichText**()
- **Size**(sz: f32)
- **ExtraLetterSpacing**(sp: f32)
- **LineHeight**(lh: f32)
- **LineHeightDefault**()
- **Heading**()
- **Monospace**()
- **Code**()
- **Strong**()
- **Weak**()
- **Underline**()
- **Strikethrough**()
- **Italics**()
- **Small**()
- **SmallRaised**()
- **Raised**()
- **TextStyleName**(name: s)

#### Return Type

Atoms

---

### Button

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| atoms | evaluated | Atoms (concrete) |

#### Builder Methods

- **Frame**(val: b)
- **Small**()
- **Wrap**()
- **Truncate**()
- **Selected**(selected: b)
- **FrameWhenInactive**(val: b)
- **RightText**(text: s)
- **ShortcutText**(text: s)

#### Return Type

Button

---

### Checkbox

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| checked | plain | b |
| text | plain | s |

#### Builder Methods

- **Indeterminate**(indeterminate: b)

#### Return Type

Checkbox

---

### CodeView

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| job | evaluated | CodeViewJob (concrete) |

#### Builder Methods

- **Selectable**(val: b)
- **Wrap**()
- **Truncate**()
- **Extend**()

#### Return Type

CodeView

---

### CodeViewJob

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| text | plain | s |

#### Builder Methods

- **Section**(byteStart: u32, byteStop: u32)

#### Return Type

CodeViewJob

---

### CollapsingHeader

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained, BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| label | evaluated | WidgetText (concrete) |

#### Builder Methods

- **DefaultOpen**(val: b)
- **Open**(val: b)
- **Close**(val: b)

#### Return Type

Block

---

### Color

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Retained

#### Builder Methods

- **FromRgb**(rv: u8, gv: u8, bv: u8)
- **FromRgbaUnmultiplied**(rv: u8, gv: u8, bv: u8, av: u8)
- **FromRgbaPremultiplied**(rv: u8, gv: u8, bv: u8, av: u8)
- **FromGray**(lv: u8)
- **FromBlackAlpha**(av: u8)
- **GammaMultiplyU8**(factor: u8)
- **GammaMultiplyF32**(factor: f32)
- **LinearMultiplyF32**(factor: f32)
- **ToOpaque**()
- **ColorTransparent**()
- **ColorBlack**()
- **ColorDarkGray**()
- **ColorGray**()
- **ColorLightGray**()
- **ColorWhite**()
- **ColorBrown**()
- **ColorDarkRed**()
- **ColorLightRed**()
- **ColorCyan**()
- **ColorMagenta**()
- **ColorYellow**()
- **ColorOrange**()
- **ColorLightYellow**()
- **ColorKhaki**()
- **ColorDarkGreen**()
- **ColorGreen**()
- **ColorLightGreen**()
- **ColorDarkBlue**()
- **ColorBlue**()
- **ColorLightBlue**()
- **ColorPurple**()
- **ColorGold**()
- **ColorDebugColor**()
- **ColorPlaceholder**()

#### Return Type

Color32

---

### ComboBox

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained, BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| label | evaluated | WidgetText (concrete) |
| selectedText | evaluated | WidgetText (concrete) |

#### Builder Methods

- **Width**(width: f32)
- **Height**(height: f32)
- **Wrap**()
- **Truncate**()

#### Return Type

Block

---

### DatePickerButton

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| packedYmd | plain | u64 |

#### Builder Methods

- **Format**(format: s)
- **HighlightWeekends**(enabled: b)
- **ShowIcon**(enabled: b)
- **Calendar**(enabled: b)
- **CalendarWeek**(enabled: b)
- **StartEndYears**(startYear: i16, endYear: i16)
- **Arrows**(enabled: b)

#### Return Type

DatePickerButton

---

### DateTimePickerButton

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| packedEpochMs | plain | u64 |

#### Builder Methods

- **Format**(format: s)
- **HighlightWeekends**(enabled: b)
- **ShowIcon**(enabled: b)
- **Calendar**(enabled: b)
- **CalendarWeek**(enabled: b)
- **StartEndYears**(startYear: i16, endYear: i16)
- **Arrows**(enabled: b)

#### Return Type

DateTimePickerButton

---

### DockAreaRaw

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| tabIds | plain | u64h |
| tabTitles | plain | sh |
| initialLayout | plain | u8h |

#### Deferred Block Maps

- **TabBody** — keys: (u64)

#### Return Type

DockAreaDummy

---

### DragValueF64

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| val | plain | f64 |

#### Builder Methods

- **Speed**(speed: f64)
- **Prefix**(prefix: s)
- **Suffix**(suffix: s)
- **MinDecimals**(digits: u32)
- **MaxDecimals**(digits: u32)
- **FixedDecimals**(digits: u32)
- **Binary**(minWidth: u32, twosComplement: b)
- **Octal**(minWidth: u32, twosComplement: b)
- **Hexadecimal**(minWidth: u32, twosComplement: b, upper: b)
- **UpdateWhileEditing**(update: b)

#### Return Type

DragValue

---

### DragValueI64

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| val | plain | i64 |

#### Builder Methods

- **Speed**(speed: f64)
- **Prefix**(prefix: s)
- **Suffix**(suffix: s)
- **MinDecimals**(digits: u32)
- **MaxDecimals**(digits: u32)
- **FixedDecimals**(digits: u32)
- **Binary**(minWidth: u32, twosComplement: b)
- **Octal**(minWidth: u32, twosComplement: b)
- **Hexadecimal**(minWidth: u32, twosComplement: b, upper: b)
- **UpdateWhileEditing**(update: b)

#### Return Type

DragValue

---

### DragValueU64

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| val | plain | u64 |

#### Builder Methods

- **Speed**(speed: f64)
- **Prefix**(prefix: s)
- **Suffix**(suffix: s)
- **MinDecimals**(digits: u32)
- **MaxDecimals**(digits: u32)
- **FixedDecimals**(digits: u32)
- **Binary**(minWidth: u32, twosComplement: b)
- **Octal**(minWidth: u32, twosComplement: b)
- **Hexadecimal**(minWidth: u32, twosComplement: b, upper: b)
- **UpdateWhileEditing**(update: b)

#### Return Type

DragValue

---

### EnabledUi

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| enabled | plain | b |

#### Return Type

Block

---

### EndETable

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| numRows | plain | u64 |
| defaultRowHeight | plain | f32 |
| numStickyHeaders | plain | u32 |
| numStickyCols | plain | u32 |

#### Builder Methods

- **ScrollToRow**(row: u64, align: u8)
- **ScrollToColumn**(col: u32, align: u8)
- **ScrollToRows**(rowBegin: u64, rowEnd: u64, align: u8)
- **ScrollToColumns**(colBegin: u32, colEnd: u32, align: u8)
- **AutoSizeMode**(mode: u8)
- **Striped**(val: b)
- **SelectedRow**(row: u64)
- **MaxHeight**(height: f32)

#### Deferred Block Maps

- **Cells** — keys: (u64, u32)
- **Headers** — keys: (u32, u32)

#### Return Type

EtDummy

---

### EtColumn

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| currentWidth | plain | f32 |

#### Builder Methods

- **Resizable**(val: b)
- **RangeMinMax**(min: f32, max: f32)
- **AutoSizeThisFrame**(val: b)

#### Return Type

EtColumn

---

### EtHeaderText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| text | plain | s |

#### Return Type

EtHeaderText

---

### EtRowHeight

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| height | plain | f32 |

#### Return Type

EtHeaderText

---

### Frame

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained, BlockIterator

#### Builder Methods

- **InnerMargin**(val: f32)
- **OuterMargin**(val: f32)
- **CornerRadius**(val: f32)
- **InnerMarginSides**(left: f32, right: f32, top: f32, bottom: f32)
- **OuterMarginSides**(left: f32, right: f32, top: f32, bottom: f32)
- **CornerRadiusSides**(nw: u8, ne: u8, sw: u8, se: u8)
- **Fill**()
- **Stroke**(width: f32)
- **Shadow**(offsetX: f32, offsetY: f32, blur: u8, spread: u8)
- **MultiplyWithOpacity**(val: f32)
- **SenseClick**()
- **SenseDrag**()
- **HoverCursorPointer**()
- **PresetGroup**()
- **PresetWindow**()
- **PresetPopup**()
- **PresetMenu**()
- **PresetCanvas**()
- **PresetDarkCanvas**()
- **PresetSideTopPanel**()
- **PresetCentralPanel**()

#### Return Type

Block

---

### Graph

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Builder Methods

- **Width**(wi: f32)
- **Height**(he: f32)
- **DraggingEnabled**(vl: b)
- **HoverEnabled**(vl: b)
- **NodeClickingEnabled**(vl: b)
- **NodeSelectionEnabled**(vl: b)
- **NodeSelectionMultiEnabled**(vl: b)
- **EdgeClickingEnabled**(vl: b)
- **EdgeSelectionEnabled**(vl: b)
- **EdgeSelectionMultiEnabled**(vl: b)
- **FitToScreen**(vl: b)
- **ZoomAndPan**(vl: b)
- **FitPadding**(pd: f32)
- **ZoomSpeed**(sp: f32)
- **LabelsAlways**(vl: b)
- **Layout**(kind: u8)
- **ResetLayout**()
- **FastForwardSteps**(st: u32)
- **LayoutDt**(dt: f32)
- **LayoutDamping**(dp: f32)
- **LayoutEpsilon**(ep: f32)
- **LayoutMaxStep**(ms: f32)
- **LayoutKScale**(ks: f32)
- **LayoutCAttract**(ca: f32)
- **LayoutCRepulse**(cr: f32)
- **LayoutRunning**(vl: b)
- **LayoutRowDist**(rd: f32)
- **LayoutColDist**(cd: f32)
- **LayoutCenterParent**(vl: b)
- **LayoutOrientation**(or: u8)

#### Return Type

GraphDrain

---

### GraphEdge

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| fromId | plain | u64 |
| toId | plain | u64 |

#### Builder Methods

- **Color**(col: u32)
- **Label**(text: s)

#### Return Type

GraphEdge

---

### GraphNode

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| nodeId | plain | u64 |
| label | plain | s |

#### Builder Methods

- **Color**(col: u32)

#### Return Type

GraphNode

---

### Grid

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **NumColumns**(val: u32)
- **Striped**(val: b)
- **MinColWidth**(val: f32)
- **MinRowHeight**(val: f32)
- **MaxColWidth**(val: f32)
- **StartRow**(val: u64)

#### Return Type

Block

---

### Group

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### H3CellsColored

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| cellIds | plain | u64h |
| cols | plain | u32h |

#### Builder Methods

- **StrokeWidth**(width: f32)
- **StrokeColor**(col: u32)

#### Return Type

H3CellsColored

---

### H3Region

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| cellIds | plain | u64h |

#### Builder Methods

- **Fill**(col: u32)
- **Stroke**(col: u32, width: f32)
- **Label**(text: s)

#### Return Type

H3Region

---

### Horizontal

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### HorizontalCentered

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### HorizontalTop

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### HorizontalWrapped

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### HoverText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| text | plain | s |

#### Return Type

Block

---

### HoverUi

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Deferred Block Maps

- **Tip** — keys: (u32)
- **Target** — keys: (u32)

#### Return Type

HoverUiDummy

---

### Hyperlink

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| url | plain | s |

#### Builder Methods

- **OpenInNewTab**(enabled: b)

#### Return Type

Hyperlink

---

### HyperlinkTo

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| label | plain | s |
| url | plain | s |

#### Builder Methods

- **OpenInNewTab**(enabled: b)

#### Return Type

Hyperlink

---

### Image

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| widthPx | plain | u32 |
| heightPx | plain | u32 |
| contentVersion | plain | u64 |
| fit | plain | u8 |
| fixedW | plain | u32 |
| fixedH | plain | u32 |
| filter | plain | u8 |
| tintRgba | plain | u32 |
| pixels | plain | u32h |

#### Return Type

Image

---

### ImageRelease

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Return Type

Image

---

### Indent

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### Label

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| text | plain | s |

#### Builder Methods

- **Selectable**(val: b)
- **Wrap**()
- **Truncate**()
- **Extend**()

#### Return Type

Label

---

### LabelAtoms

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| atoms | evaluated | Atoms (concrete) |

#### Builder Methods

- **Wrap**()
- **Truncate**()
- **Extend**()

#### Return Type

Label

---

### LabelWidgetText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| widgetText | evaluated | WidgetText (concrete) |

#### Return Type

Label

---

### MapMarker

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| markerId | plain | u64 |
| lat | plain | f64 |
| lon | plain | f64 |

#### Builder Methods

- **Label**(text: s)
- **Color**(col: u32)
- **Radius**(radius: f32)

#### Return Type

MapMarker

---

### MapPolyline

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| lats | plain | f64h |
| lons | plain | f64h |

#### Builder Methods

- **Stroke**(col: u32, width: f32)
- **Closed**(closed: b)

#### Return Type

MapPolyline

---

### MenuBar

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### MenuButton

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| atoms | evaluated | Atoms (concrete) |

#### Return Type

Block

---

### NewTable

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Builder Methods

- **Striped**(val: b)
- **Vscroll**(val: b)
- **MinScrolledHeight**(val: f32)
- **MaxScrollHeight**(val: f32)
- **ScrollToRow**(row: u64)
- **HeaderHeight**(val: f32)
- **AutoShrink**(horiz: b, vert: b)

#### Deferred Block Maps

- **Headers** — keys: (u32, u32)
- **Rows** — keys: (u64, u32)

#### Return Type

NewTableDummy

---

### NewTableColumn

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Builder Methods

- **Auto**()
- **Exact**(width: f32)
- **Initial**(width: f32)
- **Remainder**()
- **AtLeast**(minWidth: f32)
- **AtMost**(maxWidth: f32)
- **Resizable**(val: b)
- **ClipContents**(val: b)

#### Return Type

NewTableColumn

---

### NewTableRowHeight

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| height | plain | f32 |

#### Return Type

NewTableHeight

---

### NodeDir

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| label | evaluated | WidgetText (concrete) |

#### Return Type

NodeCommand

---

### NodeDirClose

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| childCount | plain | u32 |

#### Return Type

NodeCommand

---

### NodeLeaf

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| label | evaluated | WidgetText (concrete) |

#### Return Type

NodeCommand

---

### PaintArrow

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| ox | plain | f32 |
| oy | plain | f32 |
| dx | plain | f32 |
| dy | plain | f32 |
| col | plain | u32 |
| strokeWidth | plain | f32 |

#### Return Type

PaintCmd

---

### PaintCanvas

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| canvasWidth | plain | f32 |
| canvasHeight | plain | f32 |

#### Builder Methods

- **Background**(col: u32)
- **Opacity**(op: f32)
- **Sense**(click: b, drag: b, hover: b)

#### Return Type

PaintCanvas

---

### PaintCircleFilled

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| cx | plain | f32 |
| cy | plain | f32 |
| radius | plain | f32 |
| col | plain | u32 |

#### Return Type

PaintCmd

---

### PaintCircleStroke

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| cx | plain | f32 |
| cy | plain | f32 |
| radius | plain | f32 |
| col | plain | u32 |
| strokeWidth | plain | f32 |

#### Return Type

PaintCmd

---

### PaintCubicBezier

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| startX | plain | f32 |
| startY | plain | f32 |
| cp1x | plain | f32 |
| cp1y | plain | f32 |
| cp2x | plain | f32 |
| cp2y | plain | f32 |
| endX | plain | f32 |
| endY | plain | f32 |
| col | plain | u32 |
| strokeWidth | plain | f32 |

#### Return Type

PaintCmd

---

### PaintDashedLine

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| fromX | plain | f32 |
| fromY | plain | f32 |
| toX | plain | f32 |
| toY | plain | f32 |
| dashLen | plain | f32 |
| gapLen | plain | f32 |
| col | plain | u32 |
| strokeWidth | plain | f32 |

#### Return Type

PaintCmd

---

### PaintLine

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| fromX | plain | f32 |
| fromY | plain | f32 |
| toX | plain | f32 |
| toY | plain | f32 |
| col | plain | u32 |
| strokeWidth | plain | f32 |

#### Return Type

PaintCmd

---

### PaintPolyline

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| xs | plain | f32h |
| ys | plain | f32h |
| col | plain | u32 |
| strokeWidth | plain | f32 |

#### Return Type

PaintCmd

---

### PaintRectFilled

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| minX | plain | f32 |
| minY | plain | f32 |
| maxX | plain | f32 |
| maxY | plain | f32 |
| rounding | plain | f32 |
| col | plain | u32 |

#### Return Type

PaintCmd

---

### PaintRectStroke

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| minX | plain | f32 |
| minY | plain | f32 |
| maxX | plain | f32 |
| maxY | plain | f32 |
| rounding | plain | f32 |
| col | plain | u32 |
| strokeWidth | plain | f32 |

#### Return Type

PaintCmd

---

### PaintSenseRegion

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| px | plain | f32 |
| py | plain | f32 |
| sw | plain | f32 |
| sh | plain | f32 |

#### Return Type

PaintCmd

---

### PaintText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| px | plain | f32 |
| py | plain | f32 |
| anchorH | plain | u8 |
| anchorV | plain | u8 |
| text | plain | s |
| fontSize | plain | f32 |
| col | plain | u32 |

#### Builder Methods

- **Monospace**()

#### Return Type

PaintCmd

---

### PanelBottom

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### PanelBottomInside

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### PanelCentral

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### PanelCentralInside

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### PanelLeft

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### PanelLeftInside

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### PanelRight

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### PanelRightInside

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### PanelTop

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### PanelTopInside

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Resizable**(val: b)
- **DefaultSize**(val: f32)
- **ExactSize**(val: f32)

#### Return Type

Block

---

### Plot

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Builder Methods

- **Width**(wi: f32)
- **Height**(he: f32)
- **ViewAspect**(va: f32)
- **DataAspect**(da: f32)
- **XAxisLabel**(label: s)
- **YAxisLabel**(label: s)
- **Legend**()
- **AllowZoom**(val: b)
- **AllowDrag**(val: b)
- **AllowScroll**(val: b)
- **AllowZoom2**(xa: b, ya: b)
- **AllowDrag2**(xa: b, ya: b)
- **AllowScroll2**(xa: b, ya: b)
- **AllowBoxedZoom**(val: b)
- **AllowDoubleClickReset**(val: b)
- **ShowGrid**(gx: b, gy: b)
- **ShowAxes**(ax: b, ay: b)
- **ShowBackground**(val: b)
- **IncludeX**(ix: f64)
- **IncludeY**(iy: f64)
- **IncludeXRange**(lo: f64, hi: f64)
- **IncludeYRange**(lo: f64, hi: f64)
- **CenterXAxis**(val: b)
- **CenterYAxis**(val: b)
- **YGridMarks**(values: f64h, labels: sh)
- **ClampX**(lo: f64, hi: f64)
- **ClampY**(lo: f64, hi: f64)

#### Return Type

PlotDrain

---

### PlotBars

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| arguments | plain | f64h |
| values | plain | f64h |

#### Builder Methods

- **Color**(col: u32)
- **Width**(wi: f64)
- **Horizontal**()
- **Highlight**(val: b)

#### Return Type

PlotElement

---

### PlotBoxes

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| arguments | plain | f64h |
| q1s | plain | f64h |
| medians | plain | f64h |
| q3s | plain | f64h |
| whiskerMins | plain | f64h |
| whiskerMaxes | plain | f64h |
| boxWidths | plain | f64h |
| fillColors | plain | u32h |
| strokeColors | plain | u32h |
| strokeWidths | plain | f32h |

#### Builder Methods

- **Horizontal**()
- **Highlight**(val: b)
- **AllowHover**(val: b)
- **SuppressElementText**()

#### Return Type

PlotElement

---

### PlotHLine

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| yy | plain | f64 |

#### Builder Methods

- **Color**(col: u32)
- **Width**(wi: f32)
- **Highlight**(val: b)

#### Return Type

PlotElement

---

### PlotLine

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| xs | plain | f64h |
| ys | plain | f64h |

#### Builder Methods

- **Color**(col: u32)
- **Width**(wi: f32)
- **Highlight**(val: b)
- **Fill**(fy: f64)

#### Return Type

PlotElement

---

### PlotPolygon

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| xs | plain | f64h |
| ys | plain | f64h |
| fillColor | plain | u32 |
| strokeColor | plain | u32 |
| strokeWidth | plain | f32 |

#### Builder Methods

- **Highlight**(val: b)

#### Return Type

PlotElement

---

### PlotScatter

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| xs | plain | f64h |
| ys | plain | f64h |

#### Builder Methods

- **Color**(col: u32)
- **Radius**(ra: f32)
- **Shape**(sa: u8)
- **Highlight**(val: b)
- **Filled**(val: b)

#### Return Type

PlotElement

---

### PlotText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| px | plain | f64 |
| py | plain | f64 |
| text | plain | s |

#### Builder Methods

- **Color**(col: u32)

#### Return Type

PlotElement

---

### PlotVLine

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| name | plain | s |
| xx | plain | f64 |

#### Builder Methods

- **Color**(col: u32)
- **Width**(wi: f32)
- **Highlight**(val: b)

#### Return Type

PlotElement

---

### ProgressBar

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| progress | plain | f32 |

#### Builder Methods

- **Text**(text: s)
- **Animate**(enabled: b)
- **ShowPercentage**()
- **DesiredWidth**(width: f32)
- **DesiredHeight**(height: f32)
- **CornerRadius**(radius: u8)
- **Fill**()

#### Return Type

ProgressBar

---

### PushId

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### RadioButton

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| checked | plain | b |
| atoms | evaluated | Atoms (concrete) |

#### Return Type

Checkbox

---

### ScalarSize

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Retained

#### Builder Methods

- **AvailableWidth**()
- **AvailableHeight**()

#### Return Type

ScalarSize

---

### Scope

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### ScrollArea

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Builder Methods

- **Hscroll**(val: b)
- **Vscroll**(val: b)
- **Animated**(val: b)
- **AutoShrink**(horiz: b, vert: b)

#### Return Type

Block

---

### ScrollingTexture

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| widthSlots | plain | u32 |
| heightSlots | plain | u32 |
| orientation | plain | u8 |
| filter | plain | u8 |
| head | plain | u32 |
| newCount | plain | u32 |
| newColumns | plain | u32h |
| displayWidthPx | plain | f32 |
| displayHeightPx | plain | f32 |

#### Return Type

ScrollingTexture

---

### ScrollingTextureRelease

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Return Type

ScrollingTexture

---

### SelectableLabel

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| checked | plain | b |
| text | plain | s |

#### Return Type

SelectableLabel

---

### Separator

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Builder Methods

- **Horizontal**()
- **Vertical**()
- **Spacing**(spacing: f32)
- **Grow**(extra: f32)
- **Shrink**(shrink: f32)

#### Return Type

Widget

---

### SliderF64

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| val | plain | f64 |
| rangeBeginIncl | plain | f64 |
| rangeEndIncl | plain | f64 |

#### Builder Methods

- **ShowValue**(enabled: b)
- **Prefix**(prefix: s)
- **Suffix**(suffix: s)
- **Text**(text: s)
- **Vertical**()
- **Logarithmic**(enabled: b)
- **SmallestPositive**(smallestNum: f64)
- **LargestFinite**(largestNum: f64)
- **SmartAim**(enabled: b)
- **DragValueSpeed**(speed: f64)
- **MinDecimals**(digits: u32)
- **MaxDecimals**(digits: u32)
- **FixedDecimals**(digits: u32)
- **TrailingFill**(enabled: b)
- **Binary**(minWidth: u32, twosComplement: b)
- **Octal**(minWidth: u32, twosComplement: b)
- **Hexadecimal**(minWidth: u32, twosComplement: b, upper: b)
- **Integer**()
- **UpdateWhileEditing**(update: b)

#### Return Type

Slider

---

### SliderI64

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| val | plain | i64 |
| rangeBeginIncl | plain | i64 |
| rangeEndIncl | plain | i64 |

#### Builder Methods

- **ShowValue**(enabled: b)
- **Prefix**(prefix: s)
- **Suffix**(suffix: s)
- **Text**(text: s)
- **Vertical**()
- **Logarithmic**(enabled: b)
- **SmallestPositive**(smallestNum: f64)
- **LargestFinite**(largestNum: f64)
- **SmartAim**(enabled: b)
- **DragValueSpeed**(speed: f64)
- **MinDecimals**(digits: u32)
- **MaxDecimals**(digits: u32)
- **FixedDecimals**(digits: u32)
- **TrailingFill**(enabled: b)
- **Binary**(minWidth: u32, twosComplement: b)
- **Octal**(minWidth: u32, twosComplement: b)
- **Hexadecimal**(minWidth: u32, twosComplement: b, upper: b)
- **Integer**()
- **UpdateWhileEditing**(update: b)

#### Return Type

Slider

---

### SliderU64

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| val | plain | u64 |
| rangeBeginIncl | plain | u64 |
| rangeEndIncl | plain | u64 |

#### Builder Methods

- **ShowValue**(enabled: b)
- **Prefix**(prefix: s)
- **Suffix**(suffix: s)
- **Text**(text: s)
- **Vertical**()
- **Logarithmic**(enabled: b)
- **SmallestPositive**(smallestNum: f64)
- **LargestFinite**(largestNum: f64)
- **SmartAim**(enabled: b)
- **DragValueSpeed**(speed: f64)
- **MinDecimals**(digits: u32)
- **MaxDecimals**(digits: u32)
- **FixedDecimals**(digits: u32)
- **TrailingFill**(enabled: b)
- **Binary**(minWidth: u32, twosComplement: b)
- **Octal**(minWidth: u32, twosComplement: b)
- **Hexadecimal**(minWidth: u32, twosComplement: b, upper: b)
- **Integer**()
- **UpdateWhileEditing**(update: b)

#### Return Type

Slider

---

### SnarlConnection

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| srcNodeId | plain | u64 |
| srcPort | plain | u32 |
| dstNodeId | plain | u64 |
| dstPort | plain | u32 |

#### Return Type

SnarlConnection

---

### SnarlEditor

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Builder Methods

- **Width**(wi: f32)
- **Height**(he: f32)
- **PersistPositions**(vl: b)
- **WireStyle**(ws: u8)
- **BgPattern**(bp: u8)
- **MinScale**(ms: f32)
- **MaxScale**(ms: f32)
- **Centering**(vl: b)
- **CrispMagnifiedText**(vl: b)

#### Deferred Block Maps

- **NodeBody** — keys: (u64)

#### Return Type

SnarlEditor

---

### SnarlNode

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| nodeId | plain | u64 |
| posX | plain | f32 |
| posY | plain | f32 |
| kind | plain | u32 |
| title | plain | s |

#### Builder Methods

- **NumInputs**(ni: u32)
- **NumOutputs**(no: u32)

#### Return Type

SnarlNode

---

### SnarlPin

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| nodeId | plain | u64 |
| side | plain | u8 |
| pinIdx | plain | u32 |
| label | plain | s |
| kind | plain | u32 |

#### Return Type

SnarlPin

---

### Spinner

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate

#### Builder Methods

- **Size**(size: f32)

#### Return Type

Spinner

---

### Table

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| rowHeight | plain | f32 |
| numRows | plain | u64 |

#### Builder Methods

- **Striped**(val: b)
- **Vscroll**(val: b)
- **ScrollToRow**(row: u64)
- **MinScrolledHeight**(val: f32)
- **MaxScrollHeight**(val: f32)

#### Return Type

Block

---

### TableCellRichText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| widgetText | evaluated | WidgetText (concrete) |

#### Return Type

TableCell

---

### TableCellText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| text | plain | s |

#### Return Type

TableCell

---

### TableColumn

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Builder Methods

- **Auto**()
- **Exact**(width: f32)
- **Initial**(width: f32)
- **Remainder**()
- **AtLeast**(minWidth: f32)
- **AtMost**(maxWidth: f32)
- **Resizable**(val: b)
- **ClipContents**(val: b)

#### Return Type

TableColumn

---

### TableHeaderText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| text | plain | s |

#### Return Type

TableHeaderText

---

### TextEdit

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| text | plain | s |
| multiline | plain | b |

#### Builder Methods

- **CodeEditor**()
- **Frame**(frame: b)
- **HintText**(hint: s)
- **Password**(password: b)
- **Interactive**(interactive: b)
- **DesiredWidth**(width: f32)
- **DesiredRows**(rows: u32)
- **LockFocus**(lock: b)
- **CursorAtEnd**(val: b)
- **ClipText**(val: b)
- **CharLimit**(chars: u32)

#### Return Type

TextEdit

---

### TimeRangePicker

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| fromInitial | plain | s |
| toInitial | plain | s |

#### Builder Methods

- **AddPreset**(label: s, fromSql: s, toSql: s)
- **Tz**(zone: s)
- **RefreshInterval**(intervalMs: u32)
- **EvaluatedBounds**(fromMs: i64, toMs: i64)

#### Return Type

TimeRangePicker

---

### TintedScope

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained, BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| col | plain | u32 |

#### Builder Methods

- **SenseClick**()
- **Stroke**(width: f32, strokeCol: u32)
- **OuterMargin**(width: f32)
- **InnerMargin**(width: f32)

#### Return Type

Block

---

### Tree

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate

#### Return Type

Block

---

### UiWithLayout

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** BlockIterator

#### Builder Methods

- **MainDirLeftToRight**()
- **MainDirRightToLeft**()
- **MainDirTopDown**()
- **MainDirBottomUp**()
- **MainWrap**(wrap: b)
- **MainJustify**(justify: b)
- **CrossAlignMin**()
- **CrossAlignCenter**()
- **CrossAlignMax**()
- **CrossJustify**(justify: b)

#### Return Type

Block

---

### VectorSize

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Retained

#### Builder Methods

- **AvailableSize**()

#### Return Type

ScalarSize

---

### Vertical

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### VerticalCentered

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### VerticalCenteredJustified

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Immediate, BlockIterator

#### Return Type

Block

---

### WalkersMap

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, Retained

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| initLat | plain | f64 |
| initLon | plain | f64 |
| noTiles | plain | b |

#### Builder Methods

- **Width**(wi: f32)
- **Height**(he: f32)
- **SetZoom**(zoom: f64)
- **CenterAt**(lat: f64, lon: f64)
- **ZoomGesture**(enabled: b)
- **Panning**(enabled: b)
- **TileUrl**(url: s)
- **TileAttribution**(text: s)
- **TileMaxZoom**(zoom: u8)
- **TileSize**(size: u32)

#### Return Type

WalkersMap

---

### WidgetText

- **Type:** BuilderFactory
- **Identity:** No
- **Features:** Retained

#### Builder Methods

- **Text**(val: s)

#### Return Type

WidgetText

---

### Window

- **Type:** BuilderFactory
- **Identity:** Yes
- **Features:** Immediate, BlockIterator

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| label | evaluated | WidgetText (concrete) |

#### Builder Methods

- **DefaultOpen**(val: b)
- **Enabled**(val: b)
- **Interactable**(val: b)
- **Movable**(val: b)
- **Resizable**(val: b)
- **Collapsible**(val: b)
- **TitleBar**(val: b)
- **DefaultWidth**(width: f32)
- **DefaultHeight**(height: f32)
- **DefaultSize**(width: f32, height: f32)
- **DefaultPos**(posX: f32, posY: f32)
- **MinWidth**(width: f32)
- **MinHeight**(height: f32)
- **AlwaysOnTop**(val: b)
- **OpenBound**(bindingId: u64)

#### Return Type

Block

---

## Procedural Nodes

### AddSpace

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| amount | plain | f32 |

---

### AnimateBoolResponsive

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| animId | plain | u64 |
| target | plain | b |

---

### AnimateBoolWithTime

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| animId | plain | u64 |
| target | plain | b |
| durSecs | plain | f32 |

---

### AnimateValueWithTime

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| animId | plain | u64 |
| target | plain | f32 |
| durSecs | plain | f32 |

---

### CaptureAvailableSize

- **Type:** Procedural
- **Identity:** No

---

### CaptureUiRect

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| seq | plain | u64 |

---

### ContextInspectionUi

- **Type:** Procedural
- **Identity:** No

---

### ContextSendViewPortCommandClose

- **Type:** Procedural
- **Identity:** No

---

### End

- **Type:** Procedural
- **Identity:** No

---

### EndRow

- **Type:** Procedural
- **Identity:** No

---

### ExportSvg

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| path | plain | s |
| embedFonts | plain | b |
| bgRgba | plain | u32 |

---

### ExportSvgWindow

- **Type:** Procedural
- **Identity:** Yes

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| path | plain | s |
| embedFonts | plain | b |
| mode | plain | u8 |
| bgRgba | plain | u32 |

---

### GuiZoomZoomMenuButtons

- **Type:** Procedural
- **Identity:** No

---

### MeasureText

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| measureId | plain | u64 |
| text | plain | s |
| fontSize | plain | f32 |
| monospace | plain | b |

---

### MemoryResetAreas

- **Type:** Procedural
- **Identity:** No

---

### MoveWindowToTop

- **Type:** Procedural
- **Identity:** Yes

---

### PaintAbsoluteOverlay

- **Type:** Procedural
- **Identity:** No

---

### Passthrough

- **Type:** Procedural
- **Identity:** Yes

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| input | plain | u64 |

---

### PrepareNextFrame

- **Type:** Procedural
- **Identity:** No

---

### RequestRepaint

- **Type:** Procedural
- **Identity:** No

---

### RequestRepaintAfter

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| durSecs | plain | f64 |

---

### RequestScreenshot

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| path | plain | s |

---

### RequestScreenshotRect

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| path | plain | s |
| rectX | plain | f32 |
| rectY | plain | f32 |
| rectW | plain | f32 |
| rectH | plain | f32 |

---

### ScrollToCursor

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| align | plain | u8 |

---

### SetAnimationFreeze

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| freeze | plain | b |

---

### SetWindowCollapsed

- **Type:** Procedural
- **Identity:** Yes

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| collapsed | plain | b |

---

### ShowDebugTools

- **Type:** Procedural
- **Identity:** No

---

### ShowPuffinProfiler

- **Type:** Procedural
- **Identity:** No

---

### UiDisable

- **Type:** Procedural
- **Identity:** No

---

### UiSetHeight

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| height | plain | f32 |

---

### UiSetMaxHeight

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| height | plain | f32 |

---

### UiSetMaxWidth

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| width | plain | f32 |

---

### UiSetMinHeight

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| height | plain | f32 |

---

### UiSetMinWidth

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| width | plain | f32 |

---

### UiSetWidth

- **Type:** Procedural
- **Identity:** No

#### Constructor Arguments

| Name | Kind | Type |
|------|------|------|
| width | plain | f32 |

---

### WarnIfDebugBuild

- **Type:** Procedural
- **Identity:** No

---

### WidgetsGlobalThemePreferenceButtons

- **Type:** Procedural
- **Identity:** No

---

## Fetcher Nodes

### FetchF1KeyPressed

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| pressed | b |

---

### FetchFrameMetrics

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| interpretUs | u64 |
| passNr | u64 |

---

### FetchGraphEvents

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| graphIds | u64h |
| kinds | u32h |
| keyA | u64h |
| keyB | u64h |

---

### FetchGraphMetrics

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| graphIds | u64h |
| nodeCount | u32h |
| edgeCount | u32h |
| frSteps | u64h |
| frLastDisp | f32h |

---

### FetchGraphSelection

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| graphIds | u64h |
| kinds | u32h |
| keyA | u64h |
| keyB | u64h |

---

### FetchR10

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| idsTrue | u64h |
| idsFalse | u64h |

---

### FetchR14CanvasPointer

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| hoverX | f32 |
| hoverY | f32 |
| clicked | b |

---

### FetchR15PlotPointer

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| plotId | u64 |
| x | f64 |
| y | f64 |
| clicked | b |
| hoverPlotId | u64 |
| hoverX | f64 |
| hoverY | f64 |

---

### FetchR15WalkersCamera

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| found | b |
| mapId | u64 |
| zoom | f64 |
| centerLat | f64 |
| centerLon | f64 |
| minLat | f64 |
| minLon | f64 |
| maxLat | f64 |
| maxLon | f64 |
| screenWidthPx | f32 |
| screenHeightPx | f32 |
| hoverLat | f64 |
| hoverLon | f64 |
| hoverValid | b |
| clicked | b |
| viewHash | u64 |

---

### FetchR16ScrollDelta

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| x | f32 |
| y | f32 |

---

### FetchR17Modifiers

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| alt | b |
| ctrl | b |
| shift | b |
| macCmd | b |
| command | b |

---

### FetchR18AvailableSize

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| w | f32 |
| h | f32 |

---

### FetchR19ZoomDelta

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| zoom | f32 |

---

### FetchR20Pointer

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| x | f32 |
| y | f32 |
| valid | b |

---

### FetchR21UiRects

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| seqs | u64h |
| minX | f32h |
| minY | f32h |
| maxX | f32h |
| maxY | f32h |

---

### FetchR7

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| ids | u64h |
| responses | u32h |

---

### FetchR9EtPrefetch

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| ids | u64h |
| values | u64h |

---

### FetchR9F64

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| ids | u64h |
| values | f64h |

---

### FetchR9I64

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| ids | u64h |
| values | i64h |

---

### FetchR9S

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| ids | u64h |
| values | sh |

---

### FetchR9U64

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| ids | u64h |
| values | u64h |

---

### FetchSnarlEvents

- **Type:** Fetcher

#### Return Values

| Name | Type |
|------|------|
| editorIds | u64h |
| kinds | u32h |
| nodeIds | u64h |
| portsA | u32h |
| nodeIdsB | u64h |
| portsB | u32h |
| xs | f32h |
| ys | f32h |

---
