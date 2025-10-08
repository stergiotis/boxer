//go:build leeway_generic

package readaccess

const genericTypeParamsDecl = "[C runtime.ColumnI[D], D runtime.ArrayDataI]"
const genericInstantiation = "[runtime.ColumnI[runtime.ArrayDataI], runtime.ArrayDataI]"
const genericTypeParamsUse = "[C,D]"
