//go:build fffi_idl_code
package implot

import . "github.com/stergiotis/boxer/public/imzero/imgui"
var _ = ImVec2(0)


// GetStyleColorName Returns the null terminated string name for an ImPlotCol.
func GetStyleColorName(col ImPlotCol) (r string) {
  _ = `auto r = ImPlot::GetStyleColorName(col)`
  return
}
// GetMarkerName Returns the null terminated string name for an ImPlotMarker.
func GetMarkerName(marker ImPlotMarker) (r string) {
  _ = `auto r = ImPlot::GetMarkerName(marker)`
  return
}
// GetAutoColor Returns the automatically deduced style color.
func GetAutoColor(idx ImPlotCol) (r ImVec4) {
  _ = `auto r = ImPlot::GetAutoColor(idx)`
  return
}
// NiceNum Rounds x to powers of 2,5 and 10 for generating axis labels (from Graphics Gems 1 Chapter 11.2)
func NiceNum(x float64,round bool) (r float64) {
  _ = `auto r = ImPlot::NiceNum(x, round)`
  return
}
// BustPlotCache Busts the cache for every plot in the current context.
func BustPlotCache() {
  _ = `ImPlot::BustPlotCache()`
}
// SetupAxis Enables an axis or sets the label and/or flags for an existing axis. Leave label = nullptr for no label.The following API allows you to setup and customize various aspects of the current plot. The functions should be called immediately after BeginPlot and before any other API calls. Typical usage is as follows: if (BeginPlot(...)) { 1) begin a new plot SetupAxis(ImAxis_X1, "My X-Axis"); 2) make Setup calls SetupAxis(ImAxis_Y1, "My Y-Axis"); SetupLegend(ImPlotLocation_North); ... SetupFinish(); 3) [optional] explicitly finish setup PlotLine(...); 4) plot items ... EndPlot(); 5) end the plot } Important notes: Always call Setup code at the top of your BeginPlot conditional statement. Setup is locked once you start plotting or explicitly call SetupFinish. Do NOT call Setup code after you begin plotting or after you make any non-Setup API calls (e.g. utils like PlotToPixels also lock Setup) Calling SetupFinish is OPTIONAL, but probably good practice. If you do not call it yourself, then the first subsequent plotting or utility function will call it for you.
func SetupAxis(axis ImAxis) {
  _ = `ImPlot::SetupAxis(axis)`
}
// SetupAxisV Enables an axis or sets the label and/or flags for an existing axis. Leave label = nullptr for no label.The following API allows you to setup and customize various aspects of the current plot. The functions should be called immediately after BeginPlot and before any other API calls. Typical usage is as follows: if (BeginPlot(...)) { 1) begin a new plot SetupAxis(ImAxis_X1, "My X-Axis"); 2) make Setup calls SetupAxis(ImAxis_Y1, "My Y-Axis"); SetupLegend(ImPlotLocation_North); ... SetupFinish(); 3) [optional] explicitly finish setup PlotLine(...); 4) plot items ... EndPlot(); 5) end the plot } Important notes: Always call Setup code at the top of your BeginPlot conditional statement. Setup is locked once you start plotting or explicitly call SetupFinish. Do NOT call Setup code after you begin plotting or after you make any non-Setup API calls (e.g. utils like PlotToPixels also lock Setup) Calling SetupFinish is OPTIONAL, but probably good practice. If you do not call it yourself, then the first subsequent plotting or utility function will call it for you.
// * label const char * = nullptr
// * flags ImPlotAxisFlags = 0
func SetupAxisV(axis ImAxis,label string /* = nullptr*/,flags ImPlotAxisFlags /* = 0*/) {
  _ = `ImPlot::SetupAxis(axis, label, flags)`
}
// SetupAxisLimits Sets an axis range limits. If ImPlotCond_Always is used, the axes limits will be locked. Inversion with v_min > v_max is not supported; use SetupAxisLimits instead.
func SetupAxisLimits(idx ImAxis,min_lim float64,max_lim float64,cond ImPlotCond) {
  _ = `ImPlot::SetupAxisLimits(idx, min_lim, max_lim, cond)`
}
// SetupAxisFormat Sets the format of numeric axis labels via formater specifier (default="%g"). Formated values will be double (i.e. use f).
func SetupAxisFormat(idx ImAxis,fmt string) {
  _ = `ImPlot::SetupAxisFormat(idx, fmt)`
}
// SetupAxisScale Sets an axis' scale using built-in options.
func SetupAxisScale(idx ImAxis,scale ImPlotScale) {
  _ = `ImPlot::SetupAxisScale(idx, scale)`
}
// SetupAxisLimitsConstraints Sets an axis' limits constraints.
func SetupAxisLimitsConstraints(idx ImAxis,v_min float64,v_max float64) {
  _ = `ImPlot::SetupAxisLimitsConstraints(idx, v_min, v_max)`
}
// SetupAxisZoomConstraints Sets an axis' zoom constraints.
func SetupAxisZoomConstraints(idx ImAxis,z_min float64,z_max float64) {
  _ = `ImPlot::SetupAxisZoomConstraints(idx, z_min, z_max)`
}
// SetupAxes Sets the label and/or flags for primary X and Y axes (shorthand for two calls to SetupAxis).
func SetupAxes(x_label string,y_label string,x_flags ImPlotAxisFlags,y_flags ImPlotAxisFlags) {
  _ = `ImPlot::SetupAxes(x_label, y_label, x_flags, y_flags)`
}
// SetupAxesLimits Sets the primary X and Y axes range limits. If ImPlotCond_Always is used, the axes limits will be locked (shorthand for two calls to SetupAxisLimits).
func SetupAxesLimits(x_min float64,x_max float64,y_min float64,y_max float64,cond ImPlotCond) {
  _ = `ImPlot::SetupAxesLimits(x_min, x_max, y_min, y_max, cond)`
}
// SetupLegend Sets up the plot legend. This can also be called immediately after BeginSubplots when using ImPlotSubplotFlags_ShareItems.
func SetupLegend(location ImPlotLocation,flags ImPlotLegendFlags) {
  _ = `ImPlot::SetupLegend(location, flags)`
}
// SetupMouseText Set the location of the current plot's mouse position text (default = South|East).
func SetupMouseText(location ImPlotLocation,flags ImPlotMouseTextFlags) {
  _ = `ImPlot::SetupMouseText(location, flags)`
}
// SetNextAxisLimits Sets an upcoming axis range limits. If ImPlotCond_Always is used, the axes limits will be locked.Though you should default to the Setup API above, there are some scenarios where (re)configuring a plot or axis before BeginPlot is needed (e.g. if using a preceding button or slider widget to change the plot limits). In this case, you can use the SetNext API below. While this is not as feature rich as the Setup API, most common needs are provided. These functions can be called anwhere except for inside of Begin/EndPlot. For example: if (ImGui::Button("Center Plot")) ImPlot::SetNextPlotLimits(-1,1,-1,1); if (ImPlot::BeginPlot(...)) { ... ImPlot::EndPlot(); } Important notes: You must still enable non-default axes with SetupAxis for these functions to work properly.
func SetNextAxisLimits(axis ImAxis,v_min float64,v_max float64) {
  _ = `ImPlot::SetNextAxisLimits(axis, v_min, v_max)`
}
// SetNextAxisLimitsV Sets an upcoming axis range limits. If ImPlotCond_Always is used, the axes limits will be locked.Though you should default to the Setup API above, there are some scenarios where (re)configuring a plot or axis before BeginPlot is needed (e.g. if using a preceding button or slider widget to change the plot limits). In this case, you can use the SetNext API below. While this is not as feature rich as the Setup API, most common needs are provided. These functions can be called anwhere except for inside of Begin/EndPlot. For example: if (ImGui::Button("Center Plot")) ImPlot::SetNextPlotLimits(-1,1,-1,1); if (ImPlot::BeginPlot(...)) { ... ImPlot::EndPlot(); } Important notes: You must still enable non-default axes with SetupAxis for these functions to work properly.
// * cond ImPlotCond = ImPlotCond_Once
func SetNextAxisLimitsV(axis ImAxis,v_min float64,v_max float64,cond ImPlotCond /* = ImPlotCond_Once*/) {
  _ = `ImPlot::SetNextAxisLimits(axis, v_min, v_max, cond)`
}
// SetNextAxisToFit Set an upcoming axis to auto fit to its data.
func SetNextAxisToFit(axis ImAxis) {
  _ = `ImPlot::SetNextAxisToFit(axis)`
}
// SetNextAxesLimits Sets the upcoming primary X and Y axes range limits. If ImPlotCond_Always is used, the axes limits will be locked (shorthand for two calls to SetupAxisLimits).
func SetNextAxesLimits(x_min float64,x_max float64,y_min float64,y_max float64,cond ImPlotCond) {
  _ = `ImPlot::SetNextAxesLimits(x_min, x_max, y_min, y_max, cond)`
}
// SetNextAxesToFit Sets all upcoming axes to auto fit to their data.
func SetNextAxesToFit() {
  _ = `ImPlot::SetNextAxesToFit()`
}
// BeginPlot Starts a 2D plotting context. If this function returns true, EndPlot() MUST be called! You are encouraged to use the following convention: if (BeginPlot(...)) { PlotLine(...); ... EndPlot(); } Important notes: title_id must be unique to the current ImGui ID scope. If you need to avoid ID collisions or don't want to display a title in the plot, use double hashes (e.g. "MyPlot##HiddenIdText" or "##NoTitle"). size is the frame size of the plot widget, not the plot area. The default size of plots (i.e. when ImVec2(0,0)) can be modified in your ImPlotStyle.
func BeginPlot(title_id string) (r bool) {
  _ = `auto r = ImPlot::BeginPlot(title_id)`
  return
}
// BeginPlotV Starts a 2D plotting context. If this function returns true, EndPlot() MUST be called! You are encouraged to use the following convention: if (BeginPlot(...)) { PlotLine(...); ... EndPlot(); } Important notes: title_id must be unique to the current ImGui ID scope. If you need to avoid ID collisions or don't want to display a title in the plot, use double hashes (e.g. "MyPlot##HiddenIdText" or "##NoTitle"). size is the frame size of the plot widget, not the plot area. The default size of plots (i.e. when ImVec2(0,0)) can be modified in your ImPlotStyle.
// * size const ImVec2 & = ImVec2(-1, 0)
// * flags ImPlotFlags = 0
func BeginPlotV(title_id string,size ImVec2 /* = ImVec2(-1, 0)*/,flags ImPlotFlags /* = 0*/) (r bool) {
  _ = `auto r = ImPlot::BeginPlot(title_id, size, flags)`
  return
}
// SetupFinish Explicitly finalize plot setup. Once you call this, you cannot make anymore Setup calls for the current plot! Note that calling this function is OPTIONAL; it will be called by the first subsequent setup-locking API call.
func SetupFinish() {
  _ = `ImPlot::SetupFinish()`
}
// EndPlot Only call EndPlot() if BeginPlot() returns true! Typically called at the end of an if statement conditioned on BeginPlot(). See example above.
func EndPlot() {
  _ = `ImPlot::EndPlot()`
}
// SubplotNextCell Advances to next subplot.
func SubplotNextCell() {
  _ = `ImPlot::SubplotNextCell()`
}
// BeginSubplots -|--|-Starts a subdivided plotting context. If the function returns true, EndSubplots() MUST be called! Call BeginPlot/EndPlot AT MOST [rows*cols] times in between the begining and end of the subplot context. Plots are added in row major order. Example: if (BeginSubplots("My Subplot",2,3,ImVec2(800,400)) { for (int i = 0; i < 6; ++i) { if (BeginPlot(...)) { ImPlot::PlotLine(...); ... EndPlot(); } } EndSubplots(); } Produces: Important notes: title_id must be unique to the current ImGui ID scope. If you need to avoid ID collisions or don't want to display a title in the plot, use double hashes (e.g. "MySubplot##HiddenIdText" or "##NoTitle"). rows and cols must be greater than 0. size is the size of the entire grid of subplots, not the individual plots row_ratios and col_ratios must have AT LEAST rows and cols elements, respectively. These are the sizes of the rows and columns expressed in ratios. If the user adjusts the dimensions, the arrays are updated with new ratios. Important notes regarding BeginPlot from inside of BeginSubplots: The title_id parameter of BeginPlot (see above) does NOT have to be unique when called inside of a subplot context. Subplot IDs are hashed for your convenience so you don't have call PushID or generate unique title strings. Simply pass an empty string to BeginPlot unless you want to title each subplot. The size parameter of BeginPlot (see above) is ignored when inside of a subplot context. The actual size of the subplot will be based on the size value you pass to BeginSubplots and row/col_ratios if provided.
func BeginSubplots(title_id string,rows int,cols int,size ImVec2) (r bool) {
  _ = `auto r = ImPlot::BeginSubplots(title_id, rows, cols, size)`
  return
}
// EndSubplots Only call EndSubplots() if BeginSubplots() returns true! Typically called at the end of an if statement conditioned on BeginSublots(). See example above.
func EndSubplots() {
  _ = `ImPlot::EndSubplots()`
}
// SetAxis Select which axis/axes will be used for subsequent plot elements.
func SetAxis(axis ImAxis) {
  _ = `ImPlot::SetAxis(axis)`
}
func SetAxes(x_idx ImAxis,y_idx ImAxis) {
  _ = `ImPlot::SetAxes(x_idx, y_idx)`
}
func PixelsToPlot(x float32,y float32,x_idx ImAxis,y_idx ImAxis) (r ImPlotPoint) {
  _ = `auto r = ImPlot::PixelsToPlot(x, y, x_idx, y_idx)`
  return
}
// PixelsToPlot Convert pixels to a position in the current plot's coordinate system. Passing IMPLOT_AUTO uses the current axes.
func PixelsToPlotImVec2(pix ImVec2,x_idx ImAxis,y_idx ImAxis) (r ImPlotPoint) {
  _ = `auto r = ImPlot::PixelsToPlot(pix, x_idx, y_idx)`
  return
}
func PlotToPixels(x float64,y float64,x_idx ImAxis,y_idx ImAxis) (r ImVec2) {
  _ = `auto r = ImPlot::PlotToPixels(x, y, x_idx, y_idx)`
  return
}
// PlotToPixels Convert a position in the current plot's coordinate system to pixels. Passing IMPLOT_AUTO uses the current axes.
func PlotToPixelsImPlotPoint(plt ImPlotPoint,x_idx ImAxis,y_idx ImAxis) (r ImVec2) {
  _ = `auto r = ImPlot::PlotToPixels(plt, x_idx, y_idx)`
  return
}
// GetPlotPos Get the current Plot position (top-left) in pixels.
func GetPlotPos() (r ImVec2) {
  _ = `auto r = ImPlot::GetPlotPos()`
  return
}
// GetPlotSize Get the curent Plot size in pixels.
func GetPlotSize() (r ImVec2) {
  _ = `auto r = ImPlot::GetPlotSize()`
  return
}
// GetPlotMousePos Returns the mouse position in x,y coordinates of the current plot. Passing IMPLOT_AUTO uses the current axes.
func GetPlotMousePos(x_idx ImAxis,y_idx ImAxis) (r ImPlotPoint) {
  _ = `auto r = ImPlot::GetPlotMousePos(x_idx, y_idx)`
  return
}
// IsPlotHovered Returns true if the plot area in the current plot is hovered.
func IsPlotHovered() (r bool) {
  _ = `auto r = ImPlot::IsPlotHovered()`
  return
}
// IsAxisHovered Returns true if the axis label area in the current plot is hovered.
func IsAxisHovered(axis ImAxis) (r bool) {
  _ = `auto r = ImPlot::IsAxisHovered(axis)`
  return
}
// IsSubplotsHovered Returns true if the bounding frame of a subplot is hovered.
func IsSubplotsHovered() (r bool) {
  _ = `auto r = ImPlot::IsSubplotsHovered()`
  return
}
// IsPlotSelected Returns true if the current plot is being box selected.
func IsPlotSelected() (r bool) {
  _ = `auto r = ImPlot::IsPlotSelected()`
  return
}
// CancelPlotSelection Cancels a the current plot box selection.
func CancelPlotSelection() {
  _ = `ImPlot::CancelPlotSelection()`
}
// HideNextItem Hides or shows the next plot item (i.e. as if it were toggled from the legend). Use ImPlotCond_Always if you need to forcefully set this every frame.
func HideNextItem() {
  _ = `ImPlot::HideNextItem()`
}
// HideNextItemV Hides or shows the next plot item (i.e. as if it were toggled from the legend). Use ImPlotCond_Always if you need to forcefully set this every frame.
// * hidden bool = true
// * cond ImPlotCond = ImPlotCond_Once
func HideNextItemV(hidden bool /* = true*/,cond ImPlotCond /* = ImPlotCond_Once*/) {
  _ = `ImPlot::HideNextItem(hidden, cond)`
}
// Annotation Shows an annotation callout at a chosen point. Clamping keeps annotations in the plot area. Annotations are always rendered on top.
func Annotation(x float64,y float64,col ImVec4,offset ImVec2,clamp bool,round bool) {
  _ = `ImPlot::Annotation(x, y, col, offset, clamp, round)`
}
// TagX Shows a x-axis tag at the specified coordinate value.
func TagX(x float64,color ImVec4,round bool) {
  _ = `ImPlot::TagX(x, color, round)`
}
// TagY Shows a y-axis tag at the specified coordinate value.
func TagY(y float64,color ImVec4,round bool) {
  _ = `ImPlot::TagY(y, color, round)`
}
// IsLegendEntryHovered Returns true if a plot item legend entry is hovered.
func IsLegendEntryHovered(label_id string) (r bool) {
  _ = `auto r = ImPlot::IsLegendEntryHovered(label_id)`
  return
}
// BeginLegendPopup Begin a popup for a legend entry.
func BeginLegendPopup(label_id string,mouse_button ImGuiMouseButton) (r bool) {
  _ = `auto r = ImPlot::BeginLegendPopup(label_id, mouse_button)`
  return
}
// EndLegendPopup End a popup for a legend entry.
func EndLegendPopup() {
  _ = `ImPlot::EndLegendPopup()`
}
// ShowAltLegend Shows an alternate legend for the plot identified by title_id, outside of the plot frame (can be called before or after of Begin/EndPlot but must occur in the same ImGui window! This is not thoroughly tested nor scrollable!).
func ShowAltLegend(title_id string,vertical bool,size ImVec2,interactable bool) {
  _ = `ImPlot::ShowAltLegend(title_id, vertical, size, interactable)`
}
// BeginDragDropTargetPlot Turns the current plot's plotting area into a drag and drop target. Don't forget to call EndDragDropTarget!
func BeginDragDropTargetPlot() (r bool) {
  _ = `auto r = ImPlot::BeginDragDropTargetPlot()`
  return
}
// BeginDragDropTargetAxis Turns the current plot's X-axis into a drag and drop target. Don't forget to call EndDragDropTarget!
func BeginDragDropTargetAxis(axis ImAxis) (r bool) {
  _ = `auto r = ImPlot::BeginDragDropTargetAxis(axis)`
  return
}
// BeginDragDropTargetLegend Turns the current plot's legend into a drag and drop target. Don't forget to call EndDragDropTarget!
func BeginDragDropTargetLegend() (r bool) {
  _ = `auto r = ImPlot::BeginDragDropTargetLegend()`
  return
}
// EndDragDropTarget Ends a drag and drop target (currently just an alias for ImGui::EndDragDropTarget).
func EndDragDropTarget() {
  _ = `ImPlot::EndDragDropTarget()`
}
// BeginDragDropSourcePlot Turns the current plot's plotting area into a drag and drop source. You must hold Ctrl. Don't forget to call EndDragDropSource!NB: By default, plot and axes drag and drop sources require holding the Ctrl modifier to initiate the drag. You can change the modifier if desired. If ImGuiMod_None is provided, the axes will be locked from panning.
func BeginDragDropSourcePlot() (r bool) {
  _ = `auto r = ImPlot::BeginDragDropSourcePlot()`
  return
}
// BeginDragDropSourcePlotV Turns the current plot's plotting area into a drag and drop source. You must hold Ctrl. Don't forget to call EndDragDropSource!NB: By default, plot and axes drag and drop sources require holding the Ctrl modifier to initiate the drag. You can change the modifier if desired. If ImGuiMod_None is provided, the axes will be locked from panning.
// * flags ImGuiDragDropFlags = 0
func BeginDragDropSourcePlotV(flags ImGuiDragDropFlags /* = 0*/) (r bool) {
  _ = `auto r = ImPlot::BeginDragDropSourcePlot(flags)`
  return
}
// BeginDragDropSourceAxis Turns the current plot's X-axis into a drag and drop source. You must hold Ctrl. Don't forget to call EndDragDropSource!
func BeginDragDropSourceAxis(idx ImAxis,flags ImGuiDragDropFlags) (r bool) {
  _ = `auto r = ImPlot::BeginDragDropSourceAxis(idx, flags)`
  return
}
// BeginDragDropSourceItem Turns an item in the current plot's legend into drag and drop source. Don't forget to call EndDragDropSource!
func BeginDragDropSourceItem(label_id string,flags ImGuiDragDropFlags) (r bool) {
  _ = `auto r = ImPlot::BeginDragDropSourceItem(label_id, flags)`
  return
}
// EndDragDropSource Ends a drag and drop source (currently just an alias for ImGui::EndDragDropSource).
func EndDragDropSource() {
  _ = `ImPlot::EndDragDropSource()`
}
// BeginAlignedPlots Use the following around calls to Begin/EndPlot to align l/r/t/b padding. Consider using Begin/EndSubplots first. They are more feature rich and accomplish the same behaviour by default. The functions below offer lower level control of plot alignment. Align axis padding over multiple plots in a single row or column. group_id must be unique. If this function returns true, EndAlignedPlots() must be called.
func BeginAlignedPlots(group_id string) (r bool) {
  _ = `auto r = ImPlot::BeginAlignedPlots(group_id)`
  return
}
// BeginAlignedPlotsV Use the following around calls to Begin/EndPlot to align l/r/t/b padding. Consider using Begin/EndSubplots first. They are more feature rich and accomplish the same behaviour by default. The functions below offer lower level control of plot alignment. Align axis padding over multiple plots in a single row or column. group_id must be unique. If this function returns true, EndAlignedPlots() must be called.
// * vertical bool = true
func BeginAlignedPlotsV(group_id string,vertical bool /* = true*/) (r bool) {
  _ = `auto r = ImPlot::BeginAlignedPlots(group_id, vertical)`
  return
}
// EndAlignedPlots Only call EndAlignedPlots() if BeginAlignedPlots() returns true!
func EndAlignedPlots() {
  _ = `ImPlot::EndAlignedPlots()`
}
// PushStyleColor Temporarily modify a style color. Don't forget to call PopStyleColor!Use PushStyleX to temporarily modify your ImPlotStyle. The modification will last until the matching call to PopStyleX. You MUST call a pop for every push, otherwise you will leak memory! This behaves just like ImGui.
func PushStyleColor(idx ImPlotCol,col uint32) {
  _ = `ImPlot::PushStyleColor(idx, col)`
}
func PushStyleColorImVec4(idx ImPlotCol,col ImVec4) {
  _ = `ImPlot::PushStyleColor(idx, col)`
}
// PopStyleColor Undo temporary style color modification(s). Undo multiple pushes at once by increasing count.
func PopStyleColor(count int) {
  _ = `ImPlot::PopStyleColor(count)`
}
// PushStyleVar Temporarily modify a style variable of float type. Don't forget to call PopStyleVar!
func PushStyleVar(idx ImPlotStyleVar,val float32) {
  _ = `ImPlot::PushStyleVar(idx, val)`
}
// PushStyleVar Temporarily modify a style variable of int type. Don't forget to call PopStyleVar!
func PushStyleVarInt(idx ImPlotStyleVar,val int) {
  _ = `ImPlot::PushStyleVar(idx, val)`
}
// PushStyleVar Temporarily modify a style variable of ImVec2 type. Don't forget to call PopStyleVar!
func PushStyleVarImVec2(idx ImPlotStyleVar,val ImVec2) {
  _ = `ImPlot::PushStyleVar(idx, val)`
}
// PopStyleVar Undo temporary style variable modification(s). Undo multiple pushes at once by increasing count.
func PopStyleVar(count int) {
  _ = `ImPlot::PopStyleVar(count)`
}
// GetColormapCount Returns the number of available colormaps (i.e. the built-in + user-added count).
func GetColormapCount() (r int) {
  _ = `auto r = ImPlot::GetColormapCount()`
  return
}
// GetColormapName Returns a null terminated string name for a colormap given an index. Returns nullptr if index is invalid.
func GetColormapName(colormap ImPlotColormap) (r string) {
  _ = `auto r = ImPlot::GetColormapName(colormap)`
  return
}
// GetColormapIndex Returns an index number for a colormap given a valid string name. Returns -1 if name is invalid.
func GetColormapIndex(name string) (r ImPlotColormap) {
  _ = `auto r = ImPlot::GetColormapIndex(name)`
  return
}
// PushColormap Temporarily switch to one of the built-in (i.e. ImPlotColormap_XXX) or user-added colormaps (i.e. a return value of AddColormap). Don't forget to call PopColormap!
func PushColormapById(colormap ImPlotColormap) {
  _ = `ImPlot::PushColormap(colormap)`
}
// PushColormap Push a colormap by string name. Use built-in names such as "Default", "Deep", "Jet", etc. or a string you provided to AddColormap. Don't forget to call PopColormap!
func PushColormap(name string) {
  _ = `ImPlot::PushColormap(name)`
}
// PopColormap Undo temporary colormap modification(s). Undo multiple pushes at once by increasing count.
func PopColormap(count int) {
  _ = `ImPlot::PopColormap(count)`
}
// NextColormapColorU32 Returns the next unused colormap color and advances the colormap. Can be used to skip colors if desired.
func NextColormapColorU32() (r uint32) {
  _ = `auto r = ImPlot::NextColormapColorU32()`
  return
}
// NextColormapColor Returns the next color from the current colormap and advances the colormap for the current plot. Can also be used with no return value to skip colors if desired. You need to call this between Begin/EndPlot!
func NextColormapColor() (r ImVec4) {
  _ = `auto r = ImPlot::NextColormapColor()`
  return
}
// GetColormapSize Returns the size of a colormap.Colormap utils. If cmap = IMPLOT_AUTO (default), the current colormap is assumed. Pass an explicit colormap index (built-in or user-added) to specify otherwise.
func GetColormapSize() (r int) {
  _ = `auto r = ImPlot::GetColormapSize()`
  return
}
// GetColormapSizeV Returns the size of a colormap.Colormap utils. If cmap = IMPLOT_AUTO (default), the current colormap is assumed. Pass an explicit colormap index (built-in or user-added) to specify otherwise.
// * cmap ImPlotColormap = IMPLOT_AUTO
func GetColormapSizeV(cmap ImPlotColormap /* = IMPLOT_AUTO*/) (r int) {
  _ = `auto r = ImPlot::GetColormapSize(cmap)`
  return
}
// GetColormapColorU32 Returns a color from the Color map given an index >= 0 (modulo will be performed).
func GetColormapColorU32(idx int,cmap ImPlotColormap) (r uint32) {
  _ = `auto r = ImPlot::GetColormapColorU32(idx, cmap)`
  return
}
// GetColormapColor Returns a color from a colormap given an index >= 0 (modulo will be performed).
func GetColormapColor(idx int,cmap ImPlotColormap) (r ImVec4) {
  _ = `auto r = ImPlot::GetColormapColor(idx, cmap)`
  return
}
// SampleColormapU32 Linearly interpolates a color from the current colormap given t between 0 and 1.
func SampleColormapU32(t float32,cmap ImPlotColormap) (r uint32) {
  _ = `auto r = ImPlot::SampleColormapU32(t, cmap)`
  return
}
// SampleColormap Sample a color from the current colormap given t between 0 and 1.
func SampleColormap(t float32,cmap ImPlotColormap) (r ImVec4) {
  _ = `auto r = ImPlot::SampleColormap(t, cmap)`
  return
}
// ColormapScale Shows a vertical color scale with linear spaced ticks using the specified color map. Use double hashes to hide label (e.g. "##NoLabel"). If scale_min > scale_max, the scale to color mapping will be reversed.
func ColormapScale(label string,scale_min float64,scale_max float64,size ImVec2,format string,flags ImPlotColormapScaleFlags,cmap ImPlotColormap) {
  _ = `ImPlot::ColormapScale(label, scale_min, scale_max, size, format, flags, cmap)`
}
// ColormapButton Shows a button with a colormap gradient brackground.
func ColormapButton(label string,size_arg ImVec2,cmap ImPlotColormap) (r bool) {
  _ = `auto r = ImPlot::ColormapButton(label, size_arg, cmap)`
  return
}
// ItemIcon Render icons similar to those that appear in legends (nifty for data lists).
func ItemIcon(col ImVec4) {
  _ = `ImPlot::ItemIcon(col)`
}
func ItemIconUint32(col uint32) {
  _ = `ImPlot::ItemIcon(col)`
}
func ColormapIcon(cmap ImPlotColormap) {
  _ = `ImPlot::ColormapIcon(cmap)`
}
// GetPlotDrawList Get the plot draw list for custom rendering to the current plot area. Call between Begin/EndPlot.
func GetPlotDrawList() (r ImDrawListPtr) {
  _ = `auto r = ImPlot::GetPlotDrawList()`
  return
}
// PushPlotClipRect Push clip rect for rendering to current plot area. The rect can be expanded or contracted by expand pixels. Call between Begin/EndPlot.
func PushPlotClipRect(expand float32) {
  _ = `ImPlot::PushPlotClipRect(expand)`
}
// PopPlotClipRect Pop plot clip rect. Call between Begin/EndPlot.
func PopPlotClipRect() {
  _ = `ImPlot::PopPlotClipRect()`
}
// ShowStyleSelector Shows ImPlot style selector dropdown menu.
func ShowStyleSelector(label string) (r bool) {
  _ = `auto r = ImPlot::ShowStyleSelector(label)`
  return
}
// ShowColormapSelector Shows ImPlot colormap selector dropdown menu.
func ShowColormapSelector(label string) (r bool) {
  _ = `auto r = ImPlot::ShowColormapSelector(label)`
  return
}
// ShowInputMapSelector Shows ImPlot input map selector dropdown menu.
func ShowInputMapSelector(label string) (r bool) {
  _ = `auto r = ImPlot::ShowInputMapSelector(label)`
  return
}
// ShowUserGuide Add basic help/info block for end users (not a window).
func ShowUserGuide() {
  _ = `ImPlot::ShowUserGuide()`
}
// PlotImage Plots an axis-aligned image. bounds_min/bounds_max are in plot coordinates (y-up) and uv0/uv1 are in texture coordinates (y-down).
func PlotImage(label_id string,user_texture_id ImTextureID,bounds_min ImPlotPoint,bounds_max ImPlotPoint) {
  _ = `ImPlot::PlotImage(label_id, ImTextureID(user_texture_id), bounds_min, bounds_max)`
}
// PlotImageV Plots an axis-aligned image. bounds_min/bounds_max are in plot coordinates (y-up) and uv0/uv1 are in texture coordinates (y-down).
// * uv0 const ImVec2 & = ImVec2(0, 0)
// * uv1 const ImVec2 & = ImVec2(1, 1)
// * tint_col const ImVec4 & = ImVec4(1, 1, 1, 1)
// * flags ImPlotImageFlags = 0
func PlotImageV(label_id string,user_texture_id ImTextureID,bounds_min ImPlotPoint,bounds_max ImPlotPoint,uv0 ImVec2 /* = ImVec2(0, 0)*/,uv1 ImVec2 /* = ImVec2(1, 1)*/,tint_col ImVec4 /* = ImVec4(1, 1, 1, 1)*/,flags ImPlotImageFlags /* = 0*/) {
  _ = `ImPlot::PlotImage(label_id, ImTextureID(user_texture_id), bounds_min, bounds_max, uv0, uv1, tint_col, flags)`
}
// PlotText Plots a centered text label at point x,y with an optional pixel offset. Text color can be changed with ImPlot::PushStyleColor(ImPlotCol_InlayText, ...).
func PlotText(text string,x float64,y float64) {
  _ = `ImPlot::PlotText(text, x, y)`
}
// PlotTextV Plots a centered text label at point x,y with an optional pixel offset. Text color can be changed with ImPlot::PushStyleColor(ImPlotCol_InlayText, ...).
// * pix_offset const ImVec2 & = ImVec2(0, 0)
// * flags ImPlotTextFlags = 0
func PlotTextV(text string,x float64,y float64,pix_offset ImVec2 /* = ImVec2(0, 0)*/,flags ImPlotTextFlags /* = 0*/) {
  _ = `ImPlot::PlotText(text, x, y, pix_offset, flags)`
}
// PlotDummy Plots a dummy item (i.e. adds a legend entry colored by ImPlotCol_Line)
func PlotDummy(label_id string) {
  _ = `ImPlot::PlotDummy(label_id)`
}
// PlotDummyV Plots a dummy item (i.e. adds a legend entry colored by ImPlotCol_Line)
// * flags ImPlotDummyFlags = 0
func PlotDummyV(label_id string,flags ImPlotDummyFlags /* = 0*/) {
  _ = `ImPlot::PlotDummy(label_id, flags)`
}
// SetNextLineStyle Set the line color and weight for the next item only.The following can be used to modify the style of the next plot item ONLY. They do NOT require calls to PopStyleX. Leave style attributes you don't want modified to IMPLOT_AUTO or IMPLOT_AUTO_COL. Automatic styles will be deduced from the current values in your ImPlotStyle or from Colormap data.
func SetNextLineStyle() {
  _ = `ImPlot::SetNextLineStyle()`
}
// SetNextLineStyleV Set the line color and weight for the next item only.The following can be used to modify the style of the next plot item ONLY. They do NOT require calls to PopStyleX. Leave style attributes you don't want modified to IMPLOT_AUTO or IMPLOT_AUTO_COL. Automatic styles will be deduced from the current values in your ImPlotStyle or from Colormap data.
// * col const ImVec4 & = IMPLOT_AUTO_COL
// * weight float = IMPLOT_AUTO
func SetNextLineStyleV(col ImVec4 /* = IMPLOT_AUTO_COL*/,weight float32 /* = IMPLOT_AUTO*/) {
  _ = `ImPlot::SetNextLineStyle(col, weight)`
}
// SetNextFillStyle Set the fill color for the next item only.
func SetNextFillStyle() {
  _ = `ImPlot::SetNextFillStyle()`
}
// SetNextFillStyleV Set the fill color for the next item only.
// * col const ImVec4 & = IMPLOT_AUTO_COL
// * alpha_mod float = IMPLOT_AUTO
func SetNextFillStyleV(col ImVec4 /* = IMPLOT_AUTO_COL*/,alpha_mod float32 /* = IMPLOT_AUTO*/) {
  _ = `ImPlot::SetNextFillStyle(col, alpha_mod)`
}
// SetNextMarkerStyle Set the marker style for the next item only.
func SetNextMarkerStyle() {
  _ = `ImPlot::SetNextMarkerStyle()`
}
// SetNextMarkerStyleV Set the marker style for the next item only.
// * marker ImPlotMarker = IMPLOT_AUTO
// * size float = IMPLOT_AUTO
// * fill const ImVec4 & = IMPLOT_AUTO_COL
// * weight float = IMPLOT_AUTO
// * outline const ImVec4 & = IMPLOT_AUTO_COL
func SetNextMarkerStyleV(marker ImPlotMarker /* = IMPLOT_AUTO*/,size float32 /* = IMPLOT_AUTO*/,fill ImVec4 /* = IMPLOT_AUTO_COL*/,weight float32 /* = IMPLOT_AUTO*/,outline ImVec4 /* = IMPLOT_AUTO_COL*/) {
  _ = `ImPlot::SetNextMarkerStyle(marker, size, fill, weight, outline)`
}
// SetNextErrorBarStyle Set the error bar style for the next item only.
func SetNextErrorBarStyle() {
  _ = `ImPlot::SetNextErrorBarStyle()`
}
// SetNextErrorBarStyleV Set the error bar style for the next item only.
// * col const ImVec4 & = IMPLOT_AUTO_COL
// * size float = IMPLOT_AUTO
// * weight float = IMPLOT_AUTO
func SetNextErrorBarStyleV(col ImVec4 /* = IMPLOT_AUTO_COL*/,size float32 /* = IMPLOT_AUTO*/,weight float32 /* = IMPLOT_AUTO*/) {
  _ = `ImPlot::SetNextErrorBarStyle(col, size, weight)`
}
// GetLastItemColor Gets the last item primary color (i.e. its legend icon color)
func GetLastItemColor() (r ImVec4) {
  _ = `auto r = ImPlot::GetLastItemColor()`
  return
}
// BustColorCache When items in a plot sample their color from a colormap, the color is cached and does not change unless explicitly overriden. Therefore, if you change the colormap after the item has already been plotted, item colors will NOT update. If you need item colors to resample the new colormap, then use this function to bust the cached colors. If plot_title_id is nullptr, then every item in EVERY existing plot will be cache busted. Otherwise only the plot specified by plot_title_id will be busted. For the latter, this function must be called in the same ImGui ID scope that the plot is in. You should rarely if ever need this function, but it is available for applications that require runtime colormap swaps (e.g. Heatmaps demo).
func BustColorCache() {
  _ = `ImPlot::BustColorCache()`
}
// BustColorCacheV When items in a plot sample their color from a colormap, the color is cached and does not change unless explicitly overriden. Therefore, if you change the colormap after the item has already been plotted, item colors will NOT update. If you need item colors to resample the new colormap, then use this function to bust the cached colors. If plot_title_id is nullptr, then every item in EVERY existing plot will be cache busted. Otherwise only the plot specified by plot_title_id will be busted. For the latter, this function must be called in the same ImGui ID scope that the plot is in. You should rarely if ever need this function, but it is available for applications that require runtime colormap swaps (e.g. Heatmaps demo).
// * plot_title_id const char * = nullptr
func BustColorCacheV(plot_title_id string /* = nullptr*/) {
  _ = `ImPlot::BustColorCache(plot_title_id)`
}
// ShowDemoWindow Shows the ImPlot demo window (add implot_demo.cpp to your sources!)
func ShowDemoWindow() {
  _ = `ImPlot::ShowDemoWindow()`
}
// BeginItem Begins a new item. Returns false if the item should not be plotted. Pushes PlotClipRect.
func BeginItem(label_id string) (r bool) {
  _ = `auto r = ImPlot::BeginItem(label_id)`
  return
}
// BeginItemV Begins a new item. Returns false if the item should not be plotted. Pushes PlotClipRect.
// * flags ImPlotItemFlags = 0
// * recolor_from ImPlotCol = IMPLOT_AUTO
func BeginItemV(label_id string,flags ImPlotItemFlags /* = 0*/,recolor_from ImPlotCol /* = IMPLOT_AUTO*/) (r bool) {
  _ = `auto r = ImPlot::BeginItem(label_id, flags, recolor_from)`
  return
}
// EndItem Ends an item (call only if BeginItem returns true). Pops PlotClipRect.
func EndItem() {
  _ = `ImPlot::EndItem()`
}
// BustItemCache Busts the cache for every item for every plot in the current context.
func BustItemCache() {
  _ = `ImPlot::BustItemCache()`
}
