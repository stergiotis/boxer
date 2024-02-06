//go:build !bootstrap

package demo

import (
	"fmt"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"math/rand"
	"sort"
	"strings"
)

func RenderSimpleTable() {
	imgui.TextUnformatted("Basic")
	if imgui.BeginTableV("mytable", 4, imgui.ImGuiTableFlags_None, 0, 0.0) {
		for row := 0; row < 8; row++ {
			imgui.TableNextRow()
			for col := 0; col < 4; col++ {
				imgui.TableNextColumn()
				imgui.TextUnformatted(fmt.Sprintf("row %d, col %d", row, col))
			}
		}
		imgui.EndTable()
	}
}

type ColumnSortableHomogenous[T any] struct {
	Data         [][]T
	indices      []int
	compareFuncs []func(a T, b T) int
}

func NewColumnSortableHomogenous[T any](nColumns int, nRows int, compareFuncs []func(a T, b T) int) *ColumnSortableHomogenous[T] {
	data := make([][]T, 0, nColumns)
	for col := 0; col < nColumns; col++ {
		data = append(data, make([]T, nRows, nRows))
	}
	indices := make([]int, 0, nRows)
	for i := 0; i < nRows; i++ {
		indices = append(indices, i)
	}
	return &ColumnSortableHomogenous[T]{
		Data:         data,
		indices:      indices,
		compareFuncs: compareFuncs,
	}
}
func (inst *ColumnSortableHomogenous[T]) Set(col int, row int, val T) {
	inst.Data[col][inst.indices[row]] = val
}
func (inst *ColumnSortableHomogenous[T]) Get(col int, row int) T {
	return inst.Data[col][inst.indices[row]]
}
func (inst *ColumnSortableHomogenous[T]) Sort(columnIndices []int16, directions []imgui.ImGuiSortDirection) {
	indices := inst.indices
	comparseFuncts := inst.compareFuncs
	data := inst.Data
	sort.Slice(inst.indices, func(i, j int) bool {
		r1 := indices[i]
		r2 := indices[j]
		for v, col := range columnIndices {
			dir := directions[v]
			cmp := comparseFuncts[col]
			c := cmp(data[col][r1], data[col][r2])

			if c < 0 {
				return dir == imgui.ImGuiSortDirection_Ascending
			} else if c > 0 {
				return dir != imgui.ImGuiSortDirection_Ascending
			}
		}
		// all columns are equal
		return false
	})
}

func MakeRenderInteractiveTable() func() {
	var data *ColumnSortableHomogenous[string]
	return func() {
		const columnCount = 4
		const rowCount = 12
		if data == nil {
			compareFuncs := make([]func(a string, b string) int, 0, columnCount)
			for i := 0; i < columnCount; i++ {
				compareFuncs = append(compareFuncs, strings.Compare)
			}
			data = NewColumnSortableHomogenous[string](columnCount, rowCount, compareFuncs)
			for row := 0; row < rowCount; row++ {
				data.Set(0, row, fmt.Sprintf("row %02d, col %d", row, 0))
			}
			for row := 0; row < rowCount; row++ {
				data.Set(1, row, "abcdefghijklmnopqrstuvwxyz"[row:row+1])
			}
			for row := 0; row < rowCount; row++ {
				data.Set(2, row, fmt.Sprintf("%02d", row))
			}
			for row := 0; row < rowCount; row++ {
				data.Set(3, row, fmt.Sprintf("%03d", rand.Int31()%100))
			}
		}

		imgui.TextUnformatted("With setup columns:")
		flags := imgui.ImGuiTableFlags_Resizable | imgui.ImGuiTableFlags_Reorderable | imgui.ImGuiTableFlags_Hideable | imgui.ImGuiTableFlags_Sortable |
			imgui.ImGuiTableFlags_SortMulti | imgui.ImGuiTableFlags_RowBg | imgui.ImGuiTableFlags_BordersOuter | imgui.ImGuiTableFlags_BordersV |
			imgui.ImGuiTableFlags_NoBordersInBody | imgui.ImGuiTableFlags_ScrollY | imgui.ImGuiTableFlags_SizingStretchProp | imgui.ImGuiTableFlags_NoHostExtendY
		h := 12.0 * imgui.GetTextLineHeightWithSpacing()
		if imgui.BeginTableV("mytable2", 4, flags, imgui.ImVec2(complex(0.0, h)), 0.0) {
			imgui.TableSetupColumnV("column a", imgui.ImGuiTableColumnFlags_None, 0, 0)
			imgui.TableSetupColumnV("column b", imgui.ImGuiTableColumnFlags_None, 0, 1)
			imgui.TableSetupColumnV("column c", imgui.ImGuiTableColumnFlags_None, 0, 2)
			imgui.TableSetupColumnV("column d", imgui.ImGuiTableColumnFlags_None, 0, 3)
			imgui.TableSetupScrollFreeze(0, 1)
			imgui.TableHeadersRow()
			sortActive, dirty, userIds, columnIndices, directions := imgui.TableGetSortSpecs()
			for row := 0; row < rowCount; row++ {
				imgui.TableNextRow()
				for col := 0; col < columnCount; col++ {
					imgui.TableNextColumn()
					s := data.Get(col, row)
					imgui.PushIDInt(col*1000 + row)
					imgui.TextUnformatted(s)
					imgui.PopID()
				}
			}
			imgui.EndTable()
			if sortActive {
				imgui.TextUnformatted(fmt.Sprintf("dirty=%v,userids=%q,columnIndices=%q,directions=%q", dirty, userIds, columnIndices, directions))
			}
			if sortActive && dirty {
				data.Sort(columnIndices, directions)
			}
		}
	}
}
