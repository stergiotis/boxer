package porteddemo

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"math"
)

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
func ternary[T comparable](c bool, a T, b T) T {
	if c {
		return a
	}
	return b
}
func MakeDigitalDemo() (r demofunc) {
	paused := false
	dataDigitalX := []*RingBuffer[float64]{NewRingBuffer[float64](2000), NewRingBuffer[float64](2000)}
	dataDigitalY := []*RingBuffer[float64]{NewRingBuffer[float64](2000), NewRingBuffer[float64](2000)}
	dataAnalogX := []*RingBuffer[float64]{NewRingBuffer[float64](2000), NewRingBuffer[float64](2000)}
	dataAnalogY := []*RingBuffer[float64]{NewRingBuffer[float64](2000), NewRingBuffer[float64](2000)}
	showDigital := []bool{true, false}
	showAnalog := []bool{true, false}
	digitalLabels := []string{"digital_0", "digital_1"}
	analogLabels := []string{"analog_0", "analog_1"}
	var t float64
	r = func() {
		imgui.BulletText("Digital plots do not respond to Y drag and zoom, so that")
		imgui.Indent()
		imgui.TextUnformatted("you can drag analog plots over rising/falling digital edge.")
		imgui.Unindent()

		showDigital[0], _ = imgui.Toggle("digital_0", showDigital[0])
		imgui.SameLine()
		showDigital[1], _ = imgui.Toggle("digital_1", showDigital[1])
		imgui.SameLine()
		showAnalog[0], _ = imgui.Toggle("analog_0", showAnalog[0])
		imgui.SameLine()
		showAnalog[1], _ = imgui.Toggle("analog_0", showAnalog[1])
		t += float64(imgui.GetIoDeltaTime())
		if showDigital[0] {
			dataDigitalX[0].AddValue(t)
			dataDigitalY[0].AddValue(ternary(math.Sin(2*t) > 0.45, 0.0, 1.0))
		}
		if showDigital[1] {
			dataDigitalX[1].AddValue(t)
			dataDigitalY[1].AddValue(ternary(math.Sin(2*t) < 0.45, 0.0, 1.0))
		}
		if showAnalog[0] {
			dataAnalogX[0].AddValue(t)
			dataAnalogY[0].AddValue(math.Sin(2 * t))
		}
		if showAnalog[1] {
			dataAnalogX[1].AddValue(t)
			dataAnalogY[1].AddValue(math.Cos(2 * t))
		}

		if implot.BeginPlot("##Digital") {
			implot.SetupAxisLimits(implot.ImAxis_X1, t-10.0, t, ternary[implot.ImPlotCond](paused, implot.ImPlotCond_Once, implot.ImPlotCond_Always))
			implot.SetupAxisLimits(implot.ImAxis_Y1, -1, 1, implot.ImPlotCond_Once)
			for i := 0; i < 2; i++ {
				if showDigital[i] && dataDigitalX[i].Pos > 0 {
					implot.PlotDigitalFloat64(digitalLabels[i], dataDigitalX[i].Ring, dataDigitalY[i].Ring)
				}
			}
			for i := 0; i < 2; i++ {
				if showAnalog[i] && dataAnalogX[i].Pos > 0 {
					implot.PlotLineXYFloat64(analogLabels[i], dataAnalogX[i].Ring, dataAnalogY[i].Ring)
				}
			}
			implot.EndPlot()
		}
	}
	return
}
