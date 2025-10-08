package readaccess

import (
	"fmt"
	"io"
	"iter"
)

type ColumnIndexCodeGenerator struct {
	indices    []uint32
	fieldNames []string
}

func NewColumnIndexCodeGenerator() *ColumnIndexCodeGenerator {
	return &ColumnIndexCodeGenerator{
		indices:    make([]uint32, 0, 128),
		fieldNames: make([]string, 0, 128),
	}
}
func (inst *ColumnIndexCodeGenerator) IterateAll() iter.Seq2[uint32, string] {
	return func(yield func(uint32, string) bool) {
		for i, idx := range inst.indices {
			if !yield(idx, inst.fieldNames[i]) {
				break
			}
		}
	}
}
func (inst *ColumnIndexCodeGenerator) AddField(name string, columnIndex uint32) {
	inst.fieldNames = append(inst.fieldNames, name)
	inst.indices = append(inst.indices, columnIndex)
}
func (inst *ColumnIndexCodeGenerator) GenerateInstInit(w io.Writer) (err error) {
	for j, columnIndex := range inst.indices {
		_, err = fmt.Fprintf(w, "\tinst.%s = %d\n", inst.fieldNames[j], columnIndex)
		if err != nil {
			return
		}
	}
	return
}
func (inst *ColumnIndexCodeGenerator) Length() int {
	return len(inst.fieldNames)
}
func (inst *ColumnIndexCodeGenerator) GenerateCommon(w io.Writer, instClassType string) (err error) {
	{
		_, err = fmt.Fprintf(w, `func (inst *%s%s) GetColumnIndices() (columnIndices []uint32) {
	columnIndices = []uint32{
`, instClassType, genericTypeParamsUse)
		if err != nil {
			return
		}
		for _, fieldName := range inst.fieldNames {
			_, err = fmt.Fprintf(w, "\t\tinst.%s,\n", fieldName)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(w, "}\n\treturn\n}\n\n")
		if err != nil {
			return
		}
	}
	{
		_, err = fmt.Fprintf(w, `func (inst *%s%s) GetColumnIndexFieldNames() (fieldNames []string) {
	fieldNames = []string{
`, instClassType, genericTypeParamsUse)
		if err != nil {
			return
		}
		for _, fieldName := range inst.fieldNames {
			_, err = fmt.Fprintf(w, "\t\t%q,\n", instClassType+"."+fieldName)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(w, "}\n\treturn\n}\n\n")
		if err != nil {
			return
		}
	}
	{
		_, err = fmt.Fprintf(w, `func (inst *%s%s) SetColumnIndices(indices []uint32) (rest []uint32) {
`, instClassType, genericTypeParamsUse)
		if err != nil {
			return
		}
		for i, fieldName := range inst.fieldNames {
			_, err = fmt.Fprintf(w, "\t\tinst.%s = indices[%d]\n", fieldName, i)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprintf(w, "\nrest = indices[%d:]\n\treturn}\n\n", len(inst.fieldNames))
		if err != nil {
			return
		}
	}

	_, err = fmt.Fprintf(w, "var _ runtime.ColumnIndexHandlingI = (*%s%s)(nil)\n", instClassType, genericInstantiation)
	return
}
func (inst *ColumnIndexCodeGenerator) Reset() {
	inst.indices = inst.indices[:0]
	inst.fieldNames = inst.fieldNames[:0]
}
