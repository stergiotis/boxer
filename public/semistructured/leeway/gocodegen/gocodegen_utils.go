package gocodegen

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

func PrettyCaller(skip int, rootFolder string, defaultFuncName string) (funcName string, filePath string, lineNumber int) {
	var ok bool
	var pc uintptr
	pc, filePath, lineNumber, ok = runtime.Caller(skip)
	if ok {
		f := runtime.FuncForPC(pc)
		funcName = defaultFuncName
		if f != nil {
			funcName = f.Name()
			idx := strings.LastIndex(funcName, string(filepath.Separator))
			if idx > 0 && idx+1 < len(funcName) {
				funcName = funcName[idx+1:]
			}
		}
		_, after, found := strings.Cut(filePath, rootFolder)
		if found {
			filePath = "." + rootFolder + after
		}
	}
	return
}
func EmitGeneratingCodeLocation(w io.Writer) {
	funcName, filePath, lineNumber := PrettyCaller(2, "/public/", "")
	if filePath != "" {
		_, _ = fmt.Fprintf(w, "\n///////////////////////////////////////////////////////////////////\n// code generator\n// %s\n// %s:%d\n\n", funcName, filePath, lineNumber)
	}
}
