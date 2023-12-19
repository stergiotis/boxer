## Features
* FFFI preserves ...
    - ... comments;
    - ... prolog code;
    - ... epilog code;
    - ... receiver variable names and types;
    - ... multiple objects in one file.
* Error Handling
    - Implicitly: Method `receiver.handleError(_err_)`.
    - Explicitly (last result type is an interface containing `error`): Return values are left untouched.
* Reserved Names in go:
    - Variable `_err_` is used for implicitly handled errors.
    - Variable `_f` is used for the fffi runtime handle.
    - Receiver name `foreignptr` is used to detect foreign pointers/instances to enable object-oriented interfaces.
* Reserved Names in C++:
    - Goto label `skipAfterError` is reserved.
* Object-oriented interfaces
    - Define a custom type as an alias over a pointer-wide unsigned integer (consider architecture (e.g. ILP32, LLP64) of foreign code, not go code);
    - Name the receiver `foreignptr` (needed to detect oo use case);
    - Go method receiver is sent to foreign code and can be cast to a regular foreign code pointer (e.g. `((my_ptr_type*)foreignptr)->myMethod(...)`).

## Interfaces
FFFI object needs to implement the following interface
```go
type FffiI interface {
	handleError(err error)
	getFffi() *fffi2.Fffi2
}
```
## Example
Regular go package defining type aliases and objects implementing the `FffiI` interface:
```go
package mypackage

type MyEnumE uint32
type MyBool bool
type MyError interface {
  F() uint32
  error
}
type MyStruct struct {
    fffi *fffi2.Fffi2	
}
func (inst *MyStruct) handleError(err error) {
	log.Fatal().Err(err).Msg("fffi error")
}
func (inst *MyStruct) getFffi() *fffi2.Fffi2 {
    return inst.fffi	
}
```
For the use of IDL function stubs instead of method stubs the following variables need to be defined:
```go
var currentFffiVar *runtime.Fffi2
var currentFffiErrorHandler func(err error)
```
Interface definition language file (e.g. .idl.go):
```go
package mypackage

// MyExportedFunction a comment
func (inst *MyStruct) MyExportedFunctionSimple(a uint32,b uint32) (res uint32) {
  _ = `res = myU32AdditionInCpp(a,b)`
}

// MyExportedFunction a comment
func (inst *MyStruct) MyExportedFunction(a uint32,b MyEnumE) (success MyBool) {
	{ // prolog code
      myvar0 := 0
	  _ = myvar
    }
    _ = `success = cppFunc(a,b)`
	{ // epilog code
	    mvar1 := 1
		_ = myvar1
    }
}
func (inst *MyStruct) MyExportedFunction2(a uint32,b MyEnumE) (success MyBool, err MyError) {
    _ = `if(!cppFunc(a,b)) {
            sendString("my error string");
            goto skipAfterError;
         }`
}
```

Use of methods with a foreign pointer type as receiver:
```go
type MyStruct uintptr
func (foreignptr *MyStruct) MyExportedMethod(t uint32) (r bool) {
	_ = `r = ((MyStruct)foreignptr)->MyExportedMethod(t)`
}
```

Use of generics in IDL stubs:
* Type needs to be constrained with a resolvable (in the sense of fffi) type. 
* Currently only one constraint is supported.
```go
func MyExportedFunction3[T ~int](data []T, t bool) (r uint32) {
	_ = `myExportedFunc3(data,t)`
}
```

## Build Flags
IDL Code: 
```go
//go:build fffi_idl_code
```
Application Code:
```go
//go:build !bootstrap
```

## Imports

## Limitations
Multi-line imports are not supported in IDL code files
Example:
Supported:
```go
import "foo"
import "bar"
```
Not supported:
```go
import (
	"foo"
    "bar"
)
```
For parametric type params (i.e. generics) currently only one type constraint is supported. The type needs to be resolvable by the fffi naming resolver.
```go
// Okay
func MyExportedFunction3[T ~int](data []T, t bool) (r uint32) {
_ = `myExportedFunc3(data,t)`
}
// Not supported: multiple type constraints
func MyExportedFunction3[T ~int|~uint](data []T, t bool) (r uint32) {
_ = `myExportedFunc3(data,t)`
}
// Not supported: non-resolvable type
func MyExportedFunction3[T ~MyStruct](data []T, t bool) (r uint32) {
_ = `myExportedFunc3(data,t)`
}
```
For cross-package sharing of types either user dot imports (not recommended) or type aliases:
```go
import . "my/package/with/fffi/relevant/types"
```

```go
import "my/package/with/fffi/relevant/types"
type myimportedtype = types.TypeA
type myimportedtype = types.TypeB
```
