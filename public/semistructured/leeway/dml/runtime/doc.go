// Package runtime is the support code leeway-generated write sides import:
// the interfaces a generated attribute and membership builder implement
// (one per membership channel), the error-accumulating contract
// ([ErrorHandlingI]), helpers such as [AppendFixedText], and
// [DeferredSectionBuffer], which lets several typed contributions to one
// entity be emitted section by section in a fixed order.
//
// It is not meant to be called directly by application code; the generated
// table types wrap it. A [DeferredSectionBuffer] accepts one contribution
// per kind per entity and refuses mixing typed contributions with raw DML
// use; call Reset between entities.
package runtime

// GeneratedABI1 is the generated-code ABI this runtime serves. Every file the
// DML generator writes references it (`const _ = runtime.GeneratedABI1`), so
// a file generated against an older runtime fails to compile at that line
// instead of deep inside a method; regenerate it. A change to what generated
// code may call renames the constant (GeneratedABI2, …) in the same commit
// as the generator change.
const GeneratedABI1 = true
