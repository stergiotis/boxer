//go:build fffi_idl_code

package implot

import . "github.com/stergiotis/boxer/public/imzero/imgui"

var _ = ImVec2(0)

type ImAxis int

const (
	ImAxis_X1           = ImAxis(0) // enabled by default
	ImAxis_X2           = iota      // disabled by default
	ImAxis_X3           = iota      // disabled by default
	ImAxis_Y1           = iota      // enabled by default
	ImAxis_Y2           = iota      // disabled by default
	ImAxis_Y3           = iota      // disabled by default
	ImAxis_COUNT        = iota
	ImAxis_AUTO  ImAxis = -1 // auto value
)

type ImPlotFlags int

const (
	ImPlotFlags_None                    = ImPlotFlags(0)      // default
	ImPlotFlags_NoTitle                 = ImPlotFlags(1 << 0) // the plot title will not be displayed (titles are also hidden if preceeded by double hashes, e.g. "##MyPlot")
	ImPlotFlags_NoLegend                = ImPlotFlags(1 << 1) // the legend will not be displayed
	ImPlotFlags_NoMouseText             = ImPlotFlags(1 << 2) // the mouse position, in plot coordinates, will not be displayed inside of the plot
	ImPlotFlags_NoInputs                = ImPlotFlags(1 << 3) // the user will not be able to interact with the plot
	ImPlotFlags_NoMenus                 = ImPlotFlags(1 << 4) // the user will not be able to open context menus
	ImPlotFlags_NoBoxSelect             = ImPlotFlags(1 << 5) // the user will not be able to box-select
	ImPlotFlags_NoFrame                 = ImPlotFlags(1 << 6) // the ImGui frame will not be rendered
	ImPlotFlags_Equal                   = ImPlotFlags(1 << 7) // x and y axes pairs will be constrained to have the same units/pixel
	ImPlotFlags_Crosshairs              = ImPlotFlags(1 << 8) // the default mouse cursor will be replaced with a crosshair when hovered
	ImPlotFlags_CanvasOnly              = ImPlotFlags(ImPlotFlags_NoTitle | ImPlotFlags_NoLegend | ImPlotFlags_NoMenus | ImPlotFlags_NoBoxSelect | ImPlotFlags_NoMouseText)
	ImPlotFlags_AUTO        ImPlotFlags = -1 // auto value
)

type ImPlotAxisFlags int

const (
	ImPlotAxisFlags_None                          = ImPlotAxisFlags(0)       // default
	ImPlotAxisFlags_NoLabel                       = ImPlotAxisFlags(1 << 0)  // the axis label will not be displayed (axis labels are also hidden if the supplied string name is nullptr)
	ImPlotAxisFlags_NoGridLines                   = ImPlotAxisFlags(1 << 1)  // no grid lines will be displayed
	ImPlotAxisFlags_NoTickMarks                   = ImPlotAxisFlags(1 << 2)  // no tick marks will be displayed
	ImPlotAxisFlags_NoTickLabels                  = ImPlotAxisFlags(1 << 3)  // no text labels will be displayed
	ImPlotAxisFlags_NoInitialFit                  = ImPlotAxisFlags(1 << 4)  // axis will not be initially fit to data extents on the first rendered frame
	ImPlotAxisFlags_NoMenus                       = ImPlotAxisFlags(1 << 5)  // the user will not be able to open context menus with right-click
	ImPlotAxisFlags_NoSideSwitch                  = ImPlotAxisFlags(1 << 6)  // the user will not be able to switch the axis side by dragging it
	ImPlotAxisFlags_NoHighlight                   = ImPlotAxisFlags(1 << 7)  // the axis will not have its background highlighted when hovered or held
	ImPlotAxisFlags_Opposite                      = ImPlotAxisFlags(1 << 8)  // axis ticks and labels will be rendered on the conventionally opposite side (i.e, right or top)
	ImPlotAxisFlags_Foreground                    = ImPlotAxisFlags(1 << 9)  // grid lines will be displayed in the foreground (i.e. on top of data) instead of the background
	ImPlotAxisFlags_Invert                        = ImPlotAxisFlags(1 << 10) // the axis will be inverted
	ImPlotAxisFlags_AutoFit                       = ImPlotAxisFlags(1 << 11) // axis will be auto-fitting to data extents
	ImPlotAxisFlags_RangeFit                      = ImPlotAxisFlags(1 << 12) // axis will only fit points if the point is in the visible range of the **orthogonal** axis
	ImPlotAxisFlags_PanStretch                    = ImPlotAxisFlags(1 << 13) // panning in a locked or constrained state will cause the axis to stretch if possible
	ImPlotAxisFlags_LockMin                       = ImPlotAxisFlags(1 << 14) // the axis minimum value will be locked when panning/zooming
	ImPlotAxisFlags_LockMax                       = ImPlotAxisFlags(1 << 15) // the axis maximum value will be locked when panning/zooming
	ImPlotAxisFlags_Lock                          = ImPlotAxisFlags(ImPlotAxisFlags_LockMin | ImPlotAxisFlags_LockMax)
	ImPlotAxisFlags_NoDecorations                 = ImPlotAxisFlags(ImPlotAxisFlags_NoLabel | ImPlotAxisFlags_NoGridLines | ImPlotAxisFlags_NoTickMarks | ImPlotAxisFlags_NoTickLabels)
	ImPlotAxisFlags_AuxDefault                    = ImPlotAxisFlags(ImPlotAxisFlags_NoGridLines | ImPlotAxisFlags_Opposite)
	ImPlotAxisFlags_AUTO          ImPlotAxisFlags = -1 // auto value
)

type ImPlotSubplotFlags int

const (
	ImPlotSubplotFlags_None                          = ImPlotSubplotFlags(0)      // default
	ImPlotSubplotFlags_NoTitle                       = ImPlotSubplotFlags(1 << 0) // the subplot title will not be displayed (titles are also hidden if preceeded by double hashes, e.g. "##MySubplot")
	ImPlotSubplotFlags_NoLegend                      = ImPlotSubplotFlags(1 << 1) // the legend will not be displayed (only applicable if ImPlotSubplotFlags_ShareItems is enabled)
	ImPlotSubplotFlags_NoMenus                       = ImPlotSubplotFlags(1 << 2) // the user will not be able to open context menus with right-click
	ImPlotSubplotFlags_NoResize                      = ImPlotSubplotFlags(1 << 3) // resize splitters between subplot cells will be not be provided
	ImPlotSubplotFlags_NoAlign                       = ImPlotSubplotFlags(1 << 4) // subplot edges will not be aligned vertically or horizontally
	ImPlotSubplotFlags_ShareItems                    = ImPlotSubplotFlags(1 << 5) // items across all subplots will be shared and rendered into a single legend entry
	ImPlotSubplotFlags_LinkRows                      = ImPlotSubplotFlags(1 << 6) // link the y-axis limits of all plots in each row (does not apply to auxiliary axes)
	ImPlotSubplotFlags_LinkCols                      = ImPlotSubplotFlags(1 << 7) // link the x-axis limits of all plots in each column (does not apply to auxiliary axes)
	ImPlotSubplotFlags_LinkAllX                      = ImPlotSubplotFlags(1 << 8) // link the x-axis limits in every plot in the subplot (does not apply to auxiliary axes)
	ImPlotSubplotFlags_LinkAllY                      = ImPlotSubplotFlags(1 << 9) // link the y-axis limits in every plot in the subplot (does not apply to auxiliary axes)
	ImPlotSubplotFlags_ColMajor                      = ImPlotSubplotFlags(1 << 10)
	ImPlotSubplotFlags_AUTO       ImPlotSubplotFlags = -1 // auto value
)

type ImPlotLegendFlags int

const (
	ImPlotLegendFlags_None                              = ImPlotLegendFlags(0)      // default
	ImPlotLegendFlags_NoButtons                         = ImPlotLegendFlags(1 << 0) // legend icons will not function as hide/show buttons
	ImPlotLegendFlags_NoHighlightItem                   = ImPlotLegendFlags(1 << 1) // plot items will not be highlighted when their legend entry is hovered
	ImPlotLegendFlags_NoHighlightAxis                   = ImPlotLegendFlags(1 << 2) // axes will not be highlighted when legend entries are hovered (only relevant if x/y-axis count > 1)
	ImPlotLegendFlags_NoMenus                           = ImPlotLegendFlags(1 << 3) // the user will not be able to open context menus with right-click
	ImPlotLegendFlags_Outside                           = ImPlotLegendFlags(1 << 4) // legend will be rendered outside of the plot area
	ImPlotLegendFlags_Horizontal                        = ImPlotLegendFlags(1 << 5) // legend entries will be displayed horizontally
	ImPlotLegendFlags_Sort                              = ImPlotLegendFlags(1 << 6) // legend entries will be displayed in alphabetical order
	ImPlotLegendFlags_AUTO            ImPlotLegendFlags = -1                        // auto value
)

type ImPlotMouseTextFlags int

const (
	ImPlotMouseTextFlags_None                            = ImPlotMouseTextFlags(0)      // default
	ImPlotMouseTextFlags_NoAuxAxes                       = ImPlotMouseTextFlags(1 << 0) // only show the mouse position for primary axes
	ImPlotMouseTextFlags_NoFormat                        = ImPlotMouseTextFlags(1 << 1) // axes label formatters won't be used to render text
	ImPlotMouseTextFlags_ShowAlways                      = ImPlotMouseTextFlags(1 << 2) // always display mouse position even if plot not hovered
	ImPlotMouseTextFlags_AUTO       ImPlotMouseTextFlags = -1                           // auto value
)

type ImPlotDragToolFlags int

const (
	ImPlotDragToolFlags_None                          = ImPlotDragToolFlags(0)      // default
	ImPlotDragToolFlags_NoCursors                     = ImPlotDragToolFlags(1 << 0) // drag tools won't change cursor icons when hovered or held
	ImPlotDragToolFlags_NoFit                         = ImPlotDragToolFlags(1 << 1) // the drag tool won't be considered for plot fits
	ImPlotDragToolFlags_NoInputs                      = ImPlotDragToolFlags(1 << 2) // lock the tool from user inputs
	ImPlotDragToolFlags_Delayed                       = ImPlotDragToolFlags(1 << 3) // tool rendering will be delayed one frame; useful when applying position-constraints
	ImPlotDragToolFlags_AUTO      ImPlotDragToolFlags = -1                          // auto value
)

type ImPlotColormapScaleFlags int

const (
	ImPlotColormapScaleFlags_None                              = ImPlotColormapScaleFlags(0)      // default
	ImPlotColormapScaleFlags_NoLabel                           = ImPlotColormapScaleFlags(1 << 0) // the colormap axis label will not be displayed
	ImPlotColormapScaleFlags_Opposite                          = ImPlotColormapScaleFlags(1 << 1) // render the colormap label and tick labels on the opposite side
	ImPlotColormapScaleFlags_Invert                            = ImPlotColormapScaleFlags(1 << 2) // invert the colormap bar and axis scale (this only affects rendering; if you only want to reverse the scale mapping, make scale_min > scale_max)
	ImPlotColormapScaleFlags_AUTO     ImPlotColormapScaleFlags = -1                               // auto value
)

type ImPlotItemFlags int

const (
	ImPlotItemFlags_None                     = ImPlotItemFlags(0)
	ImPlotItemFlags_NoLegend                 = ImPlotItemFlags(1 << 0) // the item won't have a legend entry displayed
	ImPlotItemFlags_NoFit                    = ImPlotItemFlags(1 << 1) // the item won't be considered for plot fits
	ImPlotItemFlags_AUTO     ImPlotItemFlags = -1                      // auto value
)

type ImPlotLineFlags int

const (
	ImPlotLineFlags_None                     = ImPlotLineFlags(0)       // default
	ImPlotLineFlags_Segments                 = ImPlotLineFlags(1 << 10) // a line segment will be rendered from every two consecutive points
	ImPlotLineFlags_Loop                     = ImPlotLineFlags(1 << 11) // the last and first point will be connected to form a closed loop
	ImPlotLineFlags_SkipNaN                  = ImPlotLineFlags(1 << 12) // NaNs values will be skipped instead of rendered as missing data
	ImPlotLineFlags_NoClip                   = ImPlotLineFlags(1 << 13) // markers (if displayed) on the edge of a plot will not be clipped
	ImPlotLineFlags_Shaded                   = ImPlotLineFlags(1 << 14) // a filled region between the line and horizontal origin will be rendered; use PlotShaded for more advanced cases
	ImPlotLineFlags_AUTO     ImPlotLineFlags = -1                       // auto value
)

type ImPlotScatterFlags int

const (
	ImPlotScatterFlags_None                      = ImPlotScatterFlags(0)       // default
	ImPlotScatterFlags_NoClip                    = ImPlotScatterFlags(1 << 10) // markers on the edge of a plot will not be clipped
	ImPlotScatterFlags_AUTO   ImPlotScatterFlags = -1                          // auto value
)

type ImPlotStairsFlags int

const (
	ImPlotStairsFlags_None                      = ImPlotStairsFlags(0)       // default
	ImPlotStairsFlags_PreStep                   = ImPlotStairsFlags(1 << 10) // the y value is continued constantly to the left from every x position, i.e. the interval (x[i-1], x[i]] has the value y[i]
	ImPlotStairsFlags_Shaded                    = ImPlotStairsFlags(1 << 11)
	ImPlotStairsFlags_AUTO    ImPlotStairsFlags = -1 // auto value
)

type ImPlotShadedFlags int

const (
	ImPlotShadedFlags_None                   = ImPlotShadedFlags(0)
	ImPlotShadedFlags_AUTO ImPlotShadedFlags = -1 // auto value
)

type ImPlotBarsFlags int

const (
	ImPlotBarsFlags_None                       = ImPlotBarsFlags(0)       // default
	ImPlotBarsFlags_Horizontal                 = ImPlotBarsFlags(1 << 10) // bars will be rendered horizontally on the current y-axis
	ImPlotBarsFlags_AUTO       ImPlotBarsFlags = -1                       // auto value
)

type ImPlotBarGroupsFlags int

const (
	ImPlotBarGroupsFlags_None                            = ImPlotBarGroupsFlags(0)       // default
	ImPlotBarGroupsFlags_Horizontal                      = ImPlotBarGroupsFlags(1 << 10) // bar groups will be rendered horizontally on the current y-axis
	ImPlotBarGroupsFlags_Stacked                         = ImPlotBarGroupsFlags(1 << 11) // items in a group will be stacked on top of each other
	ImPlotBarGroupsFlags_AUTO       ImPlotBarGroupsFlags = -1                            // auto value
)

type ImPlotErrorBarsFlags int

const (
	ImPlotErrorBarsFlags_None                            = ImPlotErrorBarsFlags(0)       // default
	ImPlotErrorBarsFlags_Horizontal                      = ImPlotErrorBarsFlags(1 << 10) // error bars will be rendered horizontally on the current y-axis
	ImPlotErrorBarsFlags_AUTO       ImPlotErrorBarsFlags = -1                            // auto value
)

type ImPlotStemsFlags int

const (
	ImPlotStemsFlags_None                        = ImPlotStemsFlags(0)       // default
	ImPlotStemsFlags_Horizontal                  = ImPlotStemsFlags(1 << 10) // stems will be rendered horizontally on the current y-axis
	ImPlotStemsFlags_AUTO       ImPlotStemsFlags = -1                        // auto value
)

type ImPlotInfLinesFlags int

const (
	ImPlotInfLinesFlags_None                           = ImPlotInfLinesFlags(0) // default
	ImPlotInfLinesFlags_Horizontal                     = ImPlotInfLinesFlags(1 << 10)
	ImPlotInfLinesFlags_AUTO       ImPlotInfLinesFlags = -1 // auto value
)

type ImPlotPieChartFlags int

const (
	ImPlotPieChartFlags_None                             = ImPlotPieChartFlags(0)       // default
	ImPlotPieChartFlags_Normalize                        = ImPlotPieChartFlags(1 << 10) // force normalization of pie chart values (i.e. always make a full circle if sum < 0)
	ImPlotPieChartFlags_IgnoreHidden                     = ImPlotPieChartFlags(1 << 11)
	ImPlotPieChartFlags_AUTO         ImPlotPieChartFlags = -1 // auto value
)

type ImPlotHeatmapFlags int

const (
	ImPlotHeatmapFlags_None                        = ImPlotHeatmapFlags(0)       // default
	ImPlotHeatmapFlags_ColMajor                    = ImPlotHeatmapFlags(1 << 10) // data will be read in column major order
	ImPlotHeatmapFlags_AUTO     ImPlotHeatmapFlags = -1                          // auto value
)

type ImPlotHistogramFlags int

const (
	ImPlotHistogramFlags_None                            = ImPlotHistogramFlags(0)       // default
	ImPlotHistogramFlags_Horizontal                      = ImPlotHistogramFlags(1 << 10) // histogram bars will be rendered horizontally (not supported by PlotHistogram2D)
	ImPlotHistogramFlags_Cumulative                      = ImPlotHistogramFlags(1 << 11) // each bin will contain its count plus the counts of all previous bins (not supported by PlotHistogram2D)
	ImPlotHistogramFlags_Density                         = ImPlotHistogramFlags(1 << 12) // counts will be normalized, i.e. the PDF will be visualized, or the CDF will be visualized if Cumulative is also set
	ImPlotHistogramFlags_NoOutliers                      = ImPlotHistogramFlags(1 << 13) // exclude values outside the specifed histogram range from the count toward normalizing and cumulative counts
	ImPlotHistogramFlags_ColMajor                        = ImPlotHistogramFlags(1 << 14)
	ImPlotHistogramFlags_AUTO       ImPlotHistogramFlags = -1 // auto value
)

type ImPlotDigitalFlags int

const (
	ImPlotDigitalFlags_None                    = ImPlotDigitalFlags(0)
	ImPlotDigitalFlags_AUTO ImPlotDigitalFlags = -1 // auto value
)

type ImPlotImageFlags int

const (
	ImPlotImageFlags_None                  = ImPlotImageFlags(0)
	ImPlotImageFlags_AUTO ImPlotImageFlags = -1 // auto value
)

type ImPlotTextFlags int

const (
	ImPlotTextFlags_None                     = ImPlotTextFlags(0) // default
	ImPlotTextFlags_Vertical                 = ImPlotTextFlags(1 << 10)
	ImPlotTextFlags_AUTO     ImPlotTextFlags = -1 // auto value
)

type ImPlotDummyFlags int

const (
	ImPlotDummyFlags_None                  = ImPlotDummyFlags(0)
	ImPlotDummyFlags_AUTO ImPlotDummyFlags = -1 // auto value
)

type ImPlotCond int

const (
	ImPlotCond_None              = ImPlotCond(ImGuiCond_None)   // No condition (always set the variable), same as _Always
	ImPlotCond_Always            = ImPlotCond(ImGuiCond_Always) // No condition (always set the variable)
	ImPlotCond_Once              = ImPlotCond(ImGuiCond_Once)   // Set the variable once per runtime session (only the first call will succeed)
	ImPlotCond_AUTO   ImPlotCond = -1                           // auto value
)

type ImPlotCol int

const (
	ImPlotCol_Line                    = iota // plot line/outline color (defaults to next unused color in current colormap)
	ImPlotCol_Fill                    = iota // plot fill color for bars (defaults to the current line color)
	ImPlotCol_MarkerOutline           = iota // marker outline color (defaults to the current line color)
	ImPlotCol_MarkerFill              = iota // marker fill color (defaults to the current line color)
	ImPlotCol_ErrorBar                = iota // error bar color (defaults to ImGuiCol_Text)
	ImPlotCol_FrameBg                 = iota // plot frame background color (defaults to ImGuiCol_FrameBg)
	ImPlotCol_PlotBg                  = iota // plot area background color (defaults to ImGuiCol_WindowBg)
	ImPlotCol_PlotBorder              = iota // plot area border color (defaults to ImGuiCol_Border)
	ImPlotCol_LegendBg                = iota // legend background color (defaults to ImGuiCol_PopupBg)
	ImPlotCol_LegendBorder            = iota // legend border color (defaults to ImPlotCol_PlotBorder)
	ImPlotCol_LegendText              = iota // legend text color (defaults to ImPlotCol_InlayText)
	ImPlotCol_TitleText               = iota // plot title text color (defaults to ImGuiCol_Text)
	ImPlotCol_InlayText               = iota // color of text appearing inside of plots (defaults to ImGuiCol_Text)
	ImPlotCol_AxisText                = iota // axis label and tick lables color (defaults to ImGuiCol_Text)
	ImPlotCol_AxisGrid                = iota // axis grid color (defaults to 25% ImPlotCol_AxisText)
	ImPlotCol_AxisTick                = iota // axis tick color (defaults to AxisGrid)
	ImPlotCol_AxisBg                  = iota // background color of axis hover region (defaults to transparent)
	ImPlotCol_AxisBgHovered           = iota // axis hover color (defaults to ImGuiCol_ButtonHovered)
	ImPlotCol_AxisBgActive            = iota // axis active color (defaults to ImGuiCol_ButtonActive)
	ImPlotCol_Selection               = iota // box-selection color (defaults to yellow)
	ImPlotCol_Crosshairs              = iota // crosshairs color (defaults to ImPlotCol_PlotBorder)
	ImPlotCol_COUNT                   = iota
	ImPlotCol_AUTO          ImPlotCol = -1 // auto value
)

type ImPlotStyleVar int

const (
	ImPlotStyleVar_LineWeight                        = iota // float, plot item line weight in pixels
	ImPlotStyleVar_Marker                            = iota // int, marker specification
	ImPlotStyleVar_MarkerSize                        = iota // float, marker size in pixels (roughly the marker's "radius")
	ImPlotStyleVar_MarkerWeight                      = iota // float, plot outline weight of markers in pixels
	ImPlotStyleVar_FillAlpha                         = iota // float, alpha modifier applied to all plot item fills
	ImPlotStyleVar_ErrorBarSize                      = iota // float, error bar whisker width in pixels
	ImPlotStyleVar_ErrorBarWeight                    = iota // float, error bar whisker weight in pixels
	ImPlotStyleVar_DigitalBitHeight                  = iota // float, digital channels bit height (at 1) in pixels
	ImPlotStyleVar_DigitalBitGap                     = iota // float, digital channels bit padding gap in pixels
	ImPlotStyleVar_PlotBorderSize                    = iota // float, thickness of border around plot area
	ImPlotStyleVar_MinorAlpha                        = iota // float, alpha multiplier applied to minor axis grid lines
	ImPlotStyleVar_MajorTickLen                      = iota // ImVec2, major tick lengths for X and Y axes
	ImPlotStyleVar_MinorTickLen                      = iota // ImVec2, minor tick lengths for X and Y axes
	ImPlotStyleVar_MajorTickSize                     = iota // ImVec2, line thickness of major ticks
	ImPlotStyleVar_MinorTickSize                     = iota // ImVec2, line thickness of minor ticks
	ImPlotStyleVar_MajorGridSize                     = iota // ImVec2, line thickness of major grid lines
	ImPlotStyleVar_MinorGridSize                     = iota // ImVec2, line thickness of minor grid lines
	ImPlotStyleVar_PlotPadding                       = iota // ImVec2, padding between widget frame and plot area, labels, or outside legends (i.e. main padding)
	ImPlotStyleVar_LabelPadding                      = iota // ImVec2, padding between axes labels, tick labels, and plot edge
	ImPlotStyleVar_LegendPadding                     = iota // ImVec2, legend padding from plot edges
	ImPlotStyleVar_LegendInnerPadding                = iota // ImVec2, legend inner padding from legend edges
	ImPlotStyleVar_LegendSpacing                     = iota // ImVec2, spacing between legend entries
	ImPlotStyleVar_MousePosPadding                   = iota // ImVec2, padding between plot edge and interior info text
	ImPlotStyleVar_AnnotationPadding                 = iota // ImVec2, text padding around annotation labels
	ImPlotStyleVar_FitPadding                        = iota // ImVec2, additional fit padding as a percentage of the fit extents (e.g. ImVec2(0.1f,0.1f) adds 10% to the fit extents of X and Y)
	ImPlotStyleVar_PlotDefaultSize                   = iota // ImVec2, default size used when ImVec2(0,0) is passed to BeginPlot
	ImPlotStyleVar_PlotMinSize                       = iota // ImVec2, minimum size plot frame can be when shrunk
	ImPlotStyleVar_COUNT                             = iota
	ImPlotStyleVar_AUTO               ImPlotStyleVar = -1 // auto value
)

type ImPlotScale int

const (
	ImPlotScale_Linear             = ImPlotScale(0) // default linear scale
	ImPlotScale_Time               = iota           // date/time scale
	ImPlotScale_Log10              = iota           // base 10 logartithmic scale
	ImPlotScale_SymLog             = iota           // symmetric log scale
	ImPlotScale_AUTO   ImPlotScale = -1             // auto value
)

type ImPlotColormap int

const (
	ImPlotColormap_Deep                    = ImPlotColormap(0)  // a.k.a. seaborn deep (qual=true, n=10) (default)
	ImPlotColormap_Dark                    = ImPlotColormap(1)  // a.k.a. matplotlib "Set1" (qual=true, n=9 )
	ImPlotColormap_Pastel                  = ImPlotColormap(2)  // a.k.a. matplotlib "Pastel1" (qual=true, n=9 )
	ImPlotColormap_Paired                  = ImPlotColormap(3)  // a.k.a. matplotlib "Paired" (qual=true, n=12)
	ImPlotColormap_Viridis                 = ImPlotColormap(4)  // a.k.a. matplotlib "viridis" (qual=false, n=11)
	ImPlotColormap_Plasma                  = ImPlotColormap(5)  // a.k.a. matplotlib "plasma" (qual=false, n=11)
	ImPlotColormap_Hot                     = ImPlotColormap(6)  // a.k.a. matplotlib/MATLAB "hot" (qual=false, n=11)
	ImPlotColormap_Cool                    = ImPlotColormap(7)  // a.k.a. matplotlib/MATLAB "cool" (qual=false, n=11)
	ImPlotColormap_Pink                    = ImPlotColormap(8)  // a.k.a. matplotlib/MATLAB "pink" (qual=false, n=11)
	ImPlotColormap_Jet                     = ImPlotColormap(9)  // a.k.a. MATLAB "jet" (qual=false, n=11)
	ImPlotColormap_Twilight                = ImPlotColormap(10) // a.k.a. matplotlib "twilight" (qual=false, n=11)
	ImPlotColormap_RdBu                    = ImPlotColormap(11) // red/blue, Color Brewer (qual=false, n=11)
	ImPlotColormap_BrBG                    = ImPlotColormap(12) // brown/blue-green, Color Brewer (qual=false, n=11)
	ImPlotColormap_PiYG                    = ImPlotColormap(13) // pink/yellow-green, Color Brewer (qual=false, n=11)
	ImPlotColormap_Spectral                = ImPlotColormap(14) // color spectrum, Color Brewer (qual=false, n=11)
	ImPlotColormap_Greys                   = ImPlotColormap(15) // white/black (qual=false, n=2 )
	ImPlotColormap_AUTO     ImPlotColormap = -1                 // auto value
)

type ImPlotLocation int

const (
	ImPlotLocation_Center                   = ImPlotLocation(0)                                          // center-center
	ImPlotLocation_North                    = ImPlotLocation(1 << 0)                                     // top-center
	ImPlotLocation_South                    = ImPlotLocation(1 << 1)                                     // bottom-center
	ImPlotLocation_West                     = ImPlotLocation(1 << 2)                                     // center-left
	ImPlotLocation_East                     = ImPlotLocation(1 << 3)                                     // center-right
	ImPlotLocation_NorthWest                = ImPlotLocation(ImPlotLocation_North | ImPlotLocation_West) // top-left
	ImPlotLocation_NorthEast                = ImPlotLocation(ImPlotLocation_North | ImPlotLocation_East) // top-right
	ImPlotLocation_SouthWest                = ImPlotLocation(ImPlotLocation_South | ImPlotLocation_West) // bottom-left
	ImPlotLocation_SouthEast                = ImPlotLocation(ImPlotLocation_South | ImPlotLocation_East)
	ImPlotLocation_AUTO      ImPlotLocation = -1 // auto value
)

type ImPlotBin int

const (
	ImPlotBin_Sqrt              = ImPlotBin(-1) // k = sqrt(n)
	ImPlotBin_Sturges           = ImPlotBin(-2) // k = 1 + log2(n)
	ImPlotBin_Rice              = ImPlotBin(-3) // k = 2 * cbrt(n)
	ImPlotBin_Scott             = ImPlotBin(-4) // w = 3.49 * sigma / cbrt(n)
	ImPlotBin_AUTO    ImPlotBin = -1            // auto value
)

type ImPlotFlagsObsolete int

const (
	ImPlotFlags_YAxis2                           = ImPlotFlagsObsolete(1 << 20)
	ImPlotFlags_YAxis3                           = ImPlotFlagsObsolete(1 << 21)
	ImPlotFlagsObsolete_AUTO ImPlotFlagsObsolete = -1 // auto value
)
