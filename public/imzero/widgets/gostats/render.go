//go:build !bootstrap

package gostats

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math"
	"runtime"
	"strings"
	"time"
)

type LinearPieChartBuilder struct {
	AnnotationFunc func(label string, val float64) string
	Labels         implot.NullSeparatedStringArray
	labelsBuilder  strings.Builder
	Annotations    []string
	Values         []float64
	Positions      []float64
	CutoffValue    float64
	offset         float64
}

func NewLinearPieChartBuilder() *LinearPieChartBuilder {
	return &LinearPieChartBuilder{
		CutoffValue: math.Inf(-1),
		AnnotationFunc: func(label string, val float64) string {
			return ""
		},
		Labels:        "",
		labelsBuilder: strings.Builder{},
		Annotations:   make([]string, 0, 12),
		Values:        make([]float64, 0, 12),
		Positions:     make([]float64, 0, 12),
		offset:        0.0,
	}
}
func (inst *LinearPieChartBuilder) AddValue(label string, val float64) {
	if val > inst.CutoffValue {
		first := inst.offset == 0.0
		inst.offset += val
		inst.Values = append(inst.Values, val)
		inst.Positions = append(inst.Positions, inst.offset-0.5*val)
		if !first {
			inst.labelsBuilder.WriteRune(0)
		}
		inst.labelsBuilder.WriteString(label)
		inst.Annotations = append(inst.Annotations, inst.AnnotationFunc(label, val))
		inst.Labels = implot.NullSeparatedStringArray(inst.labelsBuilder.String())
	}
}
func (inst *LinearPieChartBuilder) Reset() {
	inst.Values = inst.Values[:0]
	inst.Positions = inst.Positions[:0]
	inst.Annotations = inst.Annotations[:0]
	inst.labelsBuilder.Reset()
	inst.offset = 0.0
	inst.Labels = ""
}

type RingBuffer[T any] struct {
	Ring []T
	Pos  int
	len  int
	full bool
}

func NewRingBuffer[T any](n int) *RingBuffer[T] {
	return &RingBuffer[T]{
		Ring: make([]T, 0, n),
		Pos:  0,
		len:  n,
		full: false,
	}
}
func (inst *RingBuffer[T]) IsFull() bool {
	return inst.full
}
func (inst *RingBuffer[T]) AddValue(v T) (spilledValue T, spill bool) {
	if inst.full {
		r := inst.Ring
		p := (inst.Pos + 1) % inst.len
		inst.Pos = p
		spilledValue = r[p]
		r[p] = v
		spill = true
	} else {
		p := inst.Pos
		inst.Ring = append(inst.Ring, v)
		inst.Pos = p + 1
		inst.full = (p + 1) == inst.len
	}
	return
}

func RenderLinearPieChart(lin *LinearPieChartBuilder, plotLabel string) {
	if len(lin.Values) > 0 {
		if implot.BeginPlotV(plotLabel, imgui.MakeImVec2(-1.0, 120.0), implot.ImPlotFlags_NoLegend) {
			implot.SetupAxes("", "", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_NoDecorations)
			pos := lin.Positions
			implot.SetupAxisTicks(implot.ImAxis_X1, pos, lin.Labels, false)
			//implot.SetupAxis(implot.ImAxis_X2)
			//implot.SetupAxisTicks(implot.ImAxis_X2, []float64{}, "", true)
			implot.SetupFinish()
			implot.PlotBarGroupsFloat64V(lin.Labels, lin.Values, 1, 1.0, 0.0, implot.ImPlotBarGroupsFlags_Stacked|implot.ImPlotBarGroupsFlags_Horizontal)
			for j, ann := range lin.Annotations {
				implot.AnnotationText(pos[j], 0.0, implot.ImPlotAutoCol, 0, true, ann)
			}
			implot.EndPlot()
		}
	}
}
func MakeStatRenderer() (r func(lastDeltaTime time.Duration, writtenBytes int)) {
	memstat := runtime.MemStats{}
	memstatCurrent := false
	i := 0
	pauses := make([]float64, 256, 256)
	var pausesA []float64
	pausesPos := []uint32{0}
	allocationsLiveSizes := make([]float64, 61, 61)
	allocationsClassSize := make([]float32, 61, 61)
	allocationSizeClasses := make([]string, 61, 61)
	allocationSizeClasseX := make([]float64, 61, 61)
	for j := 0; j < len(allocationSizeClasseX); j++ {
		allocationSizeClasseX[j] = float64(j)
	}
	collect := true
	var allocationSizeClassesS implot.NullSeparatedStringArray
	_ = allocationSizeClassesS

	tds := make([]string, 0, 1024)

	memUsageSysBuilder := NewLinearPieChartBuilder()
	memUsageSysBuilder.CutoffValue = 1024.0
	memUsageSysBuilder.AnnotationFunc = func(label string, val float64) string {
		return humanize.Bytes(uint64(val))
	}
	memUsageInUseBuilder := NewLinearPieChartBuilder()
	memUsageInUseBuilder.CutoffValue = memUsageSysBuilder.CutoffValue
	memUsageInUseBuilder.AnnotationFunc = memUsageSysBuilder.AnnotationFunc

	deltaTimesRing := NewRingBuffer[float32](256)
	writtenBytesRing := NewRingBuffer[int](256)

	ensureMemstatPopuated := func() {
		if !memstatCurrent {
			runtime.ReadMemStats(&memstat)
		}
	}

	r = func(lastDeltaTime time.Duration, writtenBytes int) {
		memstatCurrent = false

		if collect {
			deltaTimesRing.AddValue(float32(lastDeltaTime.Milliseconds()))
			writtenBytesRing.AddValue(writtenBytes)
		}
		if imgui.Begin("stats") {
			var collectChanged bool
			collect, collectChanged = imgui.Toggle("Collect", collect)

			if imgui.TreeNode("Transferred data") {
				m := lastDeltaTime.Milliseconds()
				if m > 0 {
					imgui.Text("delta T = %d ms = %d frames/s", m, 1000/m)
				}
				imgui.Text("transferred data = %s", humanize.Bytes(uint64(writtenBytes)))
				imgui.Text("transferred rate = %s", humanize.SI(float64(writtenBytes)*1000.0/float64(m), "B/s"))
				imgui.TreePop()
			}

			imgui.SetNextItemOpenV(true, imgui.ImGuiCond_Once)
			if imgui.TreeNode("go render") {
				if implot.BeginPlot("Δt time series") {
					implot.SetupAxes("time step", "Δt in ms", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					implot.SetupFinish()
					implot.PlotLineFloat32("Δt", deltaTimesRing.Ring)
					implot.EndPlot()
				}
				if implot.BeginPlot("Δt histogram") {
					implot.SetupAxes("Δt in ms", "Count", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					implot.SetupFinish()
					implot.PlotHistogramFloat32("Δt", deltaTimesRing.Ring)
					implot.EndPlot()
				}
				if implot.BeginPlot("transferred bytes time series") {
					implot.SetupAxes("time step", "transferred bytes", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					implot.SetupFinish()
					implot.PlotLineInt("", writtenBytesRing.Ring)
					implot.EndPlot()
				}
				if implot.BeginPlot("transferred bytes histogram") {
					implot.SetupAxes("transferred bytes", "Count", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					implot.SetupFinish()
					implot.PlotHistogramInt("", writtenBytesRing.Ring)
					implot.EndPlot()
				}
				imgui.TreePop()
			}

			if collectChanged {
				i = 0
			}
			i = (i + 1) % 64

			if imgui.TreeNodeEx("Garbage Collection") {
				if collect && i == 0 {
					ensureMemstatPopuated()
					for j, p := range memstat.PauseNs {
						pauses[j] = float64(p)
					}
					pausesPos[0] = memstat.NumGC % 256
					pausesA = pauses
					if memstat.NumGC < 256 {
						pausesA = pauses[:memstat.NumGC]
					}
				}

				if imgui.Button("Force GC") {
					runtime.GC()
				}
				if implot.BeginPlot("GC Pauses Histogram") {
					implot.SetupAxes("Pause in ns", "Count", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					implot.SetupFinish()
					implot.PlotHistogramFloat64("GC Pauses", pausesA)
					implot.EndPlot()
				}
				if implot.BeginPlot("GC Pauses Time Series") {
					implot.SetupAxes("Pause in ns", "Collection #", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					implot.SetupFinish()
					implot.PlotStemsFloat64("GC Pauses", pausesA)
					implot.PlotInfLinesUInt32("Current", pausesPos)
					implot.EndPlot()
				}
				imgui.TreePop()
			}

			imgui.SetNextItemOpenV(true, imgui.ImGuiCond_Once)
			if imgui.TreeNode("Memory Usage") {
				if i == 0 && collect {
					ensureMemstatPopuated()
					memUsageSysBuilder.Reset()
					memUsageSysBuilder.AddValue("Heap", float64(memstat.HeapSys))
					memUsageSysBuilder.AddValue("Stack", float64(memstat.StackSys))
					memUsageSysBuilder.AddValue("MSpan", float64(memstat.MSpanSys))
					memUsageSysBuilder.AddValue("MCache", float64(memstat.MCacheSys))
					memUsageSysBuilder.AddValue("BuckHash", float64(memstat.BuckHashSys))
					memUsageSysBuilder.AddValue("GC", float64(memstat.GCSys))
					memUsageSysBuilder.AddValue("Other", float64(memstat.OtherSys))

					memUsageInUseBuilder.Reset()
					memUsageInUseBuilder.AddValue("Heap In-Use", float64(memstat.HeapInuse))
					memUsageInUseBuilder.AddValue("Heap Idle", float64(memstat.HeapIdle))
					memUsageInUseBuilder.AddValue("Stack In-Use", float64(memstat.StackInuse))
					memUsageInUseBuilder.AddValue("MSpan In-Use", float64(memstat.MSpanInuse))
					memUsageInUseBuilder.AddValue("MCache In-Use", float64(memstat.MCacheInuse))
				}
				RenderLinearPieChart(memUsageSysBuilder, "Reserved Memory Usage")
				RenderLinearPieChart(memUsageInUseBuilder, "In-Use Memory Usage")
				imgui.TreePop()
			}

			if imgui.TreeNode("Allocations") {
				if i == 0 && collect {
					ensureMemstatPopuated()

					tds = tds[:0]
					tds = append(tds, "Cumulative Allocated Memory")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.TotalAlloc))
					tds = append(tds, humanize.Bytes(memstat.TotalAlloc))
					tds = append(tds, "Virtual Memory")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.Sys))
					tds = append(tds, humanize.Bytes(memstat.Sys))
					tds = append(tds, "Cumulative Count Allocated Objects")
					tds = append(tds, fmt.Sprintf("%d", memstat.Mallocs))
					tds = append(tds, "")
					tds = append(tds, "Cumulative Count Freed Objects")
					tds = append(tds, fmt.Sprintf("%d", memstat.Frees))
					tds = append(tds, "")
					tds = append(tds, "Heap Memory Allocated Objects Memory")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.HeapAlloc))
					tds = append(tds, humanize.Bytes(memstat.HeapAlloc))
					tds = append(tds, "Heap Memory Allocated Objects Count")
					tds = append(tds, fmt.Sprintf("%d", memstat.HeapObjects))
					tds = append(tds, "")
					tds = append(tds, "Heap Memory Allocated Memory (OS)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.HeapSys))
					tds = append(tds, humanize.Bytes(memstat.HeapSys))
					tds = append(tds, "Heap Memory Allocated Memory (idle)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.HeapIdle))
					tds = append(tds, humanize.Bytes(memstat.HeapIdle))
					tds = append(tds, "Heap Memory Allocated Memory (in-use)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.HeapInuse))
					tds = append(tds, humanize.Bytes(memstat.HeapInuse))
					tds = append(tds, "Heap Memory Released Memory")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.HeapReleased))
					tds = append(tds, humanize.Bytes(memstat.HeapReleased))
					tds = append(tds, "Stack Memory Allocated Memory (in-use)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.StackInuse))
					tds = append(tds, humanize.Bytes(memstat.StackInuse))
					tds = append(tds, "Stack Memory Allocated Memory (OS)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.StackSys))
					tds = append(tds, humanize.Bytes(memstat.StackSys))
					tds = append(tds, "Off-heap Memory Allocated Memory (in-use)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.MSpanInuse))
					tds = append(tds, humanize.Bytes(memstat.MSpanInuse))
					tds = append(tds, "Off-heap Memory Allocated Memory (OS)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.MSpanSys))
					tds = append(tds, humanize.Bytes(memstat.MSpanSys))
					tds = append(tds, "MCache Memory Allocated Memory (in-use)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.MCacheInuse))
					tds = append(tds, humanize.Bytes(memstat.MCacheInuse))
					tds = append(tds, "MCache Memory Allocated Memory (OS)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.MCacheSys))
					tds = append(tds, humanize.Bytes(memstat.MCacheSys))
					tds = append(tds, "Profiling Buck Allocated Memory (OS)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.BuckHashSys))
					tds = append(tds, humanize.Bytes(memstat.BuckHashSys))
					tds = append(tds, "GC Metadata Allocated Memory (OS)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.GCSys))
					tds = append(tds, humanize.Bytes(memstat.GCSys))
					tds = append(tds, "Misc Allocated Memory (OS)")
					tds = append(tds, fmt.Sprintf("%d Bytes", memstat.OtherSys))
					tds = append(tds, humanize.Bytes(memstat.OtherSys))

					last := uint32(0)
					for j, p := range memstat.BySize {
						allocationsClassSize[j] = float32(p.Size)
						allocationsLiveSizes[j] = float64(p.Mallocs-p.Frees) * float64(p.Size)
						var s string
						if j == 0 {
							s = fmt.Sprintf("(0,%d]", p.Size)
						} else {
							s = fmt.Sprintf("(%d,%d]", last, p.Size)
						}
						last = p.Size
						allocationSizeClasses[j] = s
					}
					allocationSizeClassesS = implot.MakeNullSeparatedStringArray(allocationSizeClasses...)
				}
				if imgui.BeginTableV("##memstat", 3,
					imgui.ImGuiTableFlags_RowBg|
						imgui.ImGuiTableFlags_Borders|
						imgui.ImGuiTableFlags_SizingFixedFit,
					0.0, 0.0) {
					imgui.TableSetupColumnV("Key", imgui.ImGuiTableColumnFlags_None, 0, 0)
					imgui.TableSetupColumnV("Value", imgui.ImGuiTableColumnFlags_None, 0, 1)
					imgui.TableSetupColumnV("Pretty Value", imgui.ImGuiTableColumnFlags_None, 0, 2)
					imgui.TableSetupScrollFreeze(0, 1)
					imgui.TableHeadersRow()
					for _, td := range tds {
						imgui.TableNextColumn()
						imgui.TextUnformatted(td)
					}
					imgui.EndTable()
				}

				if implot.BeginPlot("Allocation Statistics") {
					// TODO implot.SetupAxesLinks
					implot.SetupAxes("Size class", "Total live size in Bytes", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					//implot.SetupAxisTicks(implot.ImAxis_X1, allocationSizeClasseX, allocationSizeClassesS, false)
					implot.SetupFinish()
					implot.PlotBarsFloat64("Live size", allocationsLiveSizes)
					implot.EndPlot()
				}
				if implot.BeginPlot("Allocation Size Classes") {
					implot.SetupAxisScale(implot.ImAxis_Y1, implot.ImPlotScale_Log10)
					implot.SetupAxes("Size class", "Size in Bytes", implot.ImPlotAxisFlags_AutoFit, implot.ImPlotAxisFlags_AutoFit)
					//implot.SetupAxisTicks(implot.ImAxis_X1, allocationSizeClasseX, allocationSizeClassesS, false)
					implot.SetupFinish()
					implot.PlotStairsFloat32("Allocation Size Class", allocationsClassSize)
					implot.EndPlot()
				}
				imgui.TreePop()
			}
		}
		imgui.End()
	}
	return
}
