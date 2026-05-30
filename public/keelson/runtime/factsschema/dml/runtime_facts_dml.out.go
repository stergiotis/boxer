// Code generated; Leeway DML (github.com/stergiotis/boxer/public/app) DO NOT EDIT.

package dml

import (
	"errors"
	"github.com/apache/arrow-go/v18/arrow"
	array "github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"slices"
	"time"
)

///////////////////////////////////////////////////////////////////
// code generator
// gocodegen.GenerateArrowSchemaFactory
// ./public/semistructured/leeway/gocodegen/gocodegen_common.go:26

func CreateSchemaFacts() (schema *arrow.Schema) {
	schema = arrow.NewSchema([]arrow.Field{
		/* 000 */ arrow.Field{Name: "id:id:u64:2k:0:0:", Nullable: false, Type: arrow.PrimitiveTypes.Uint64},
		/* 001 */ arrow.Field{Name: "id:naturalKey:y:g:0:0:", Nullable: false, Type: &arrow.BinaryType{}},
		/* 002 */ arrow.Field{Name: "ts:ts:z64:2k:0:0:", Nullable: false, Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
		/* 003 */ arrow.Field{Name: "lc:expiresAt:z64:g:0:0:", Nullable: false, Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
		/* 004 */ arrow.Field{Name: "tv:foreignKey:value:val:u64:g:1d0DV72:0:0::foreignKey", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 005 */ arrow.Field{Name: "tv:foreignKey:lr:lr:u64:2q:1d0DV72:0:0::foreignKey", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 006 */ arrow.Field{Name: "tv:foreignKey:lrcard:lrcard:u64:4gw:1d0DV72:0:0::foreignKey", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 007 */ arrow.Field{Name: "tv:textArray:value:val:sh:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 008 */ arrow.Field{Name: "tv:textArray:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 009 */ arrow.Field{Name: "tv:textArray:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 010 */ arrow.Field{Name: "tv:textArray:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 011 */ arrow.Field{Name: "tv:textArray:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 012 */ arrow.Field{Name: "tv:textArray:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 013 */ arrow.Field{Name: "tv:textArray:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 014 */ arrow.Field{Name: "tv:textArray:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 015 */ arrow.Field{Name: "tv:textArray:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 016 */ arrow.Field{Name: "tv:stringArray:value:val:sh:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 017 */ arrow.Field{Name: "tv:stringArray:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 018 */ arrow.Field{Name: "tv:stringArray:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 019 */ arrow.Field{Name: "tv:stringArray:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 020 */ arrow.Field{Name: "tv:stringArray:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 021 */ arrow.Field{Name: "tv:stringArray:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 022 */ arrow.Field{Name: "tv:stringArray:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 023 */ arrow.Field{Name: "tv:stringArray:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 024 */ arrow.Field{Name: "tv:stringArray:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 025 */ arrow.Field{Name: "tv:symbol:value:val:s:m:0:24:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 026 */ arrow.Field{Name: "tv:symbol:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 027 */ arrow.Field{Name: "tv:symbol:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 028 */ arrow.Field{Name: "tv:symbol:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 029 */ arrow.Field{Name: "tv:symbol:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 030 */ arrow.Field{Name: "tv:symbol:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 031 */ arrow.Field{Name: "tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 032 */ arrow.Field{Name: "tv:symbol:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 033 */ arrow.Field{Name: "tv:symbolArray:value:val:sh:m:0:24:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.StringType{})},
		/* 034 */ arrow.Field{Name: "tv:symbolArray:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 035 */ arrow.Field{Name: "tv:symbolArray:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 036 */ arrow.Field{Name: "tv:symbolArray:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 037 */ arrow.Field{Name: "tv:symbolArray:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 038 */ arrow.Field{Name: "tv:symbolArray:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 039 */ arrow.Field{Name: "tv:symbolArray:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 040 */ arrow.Field{Name: "tv:symbolArray:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 041 */ arrow.Field{Name: "tv:symbolArray:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 042 */ arrow.Field{Name: "tv:blobArray:value:val:yh:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 043 */ arrow.Field{Name: "tv:blobArray:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 044 */ arrow.Field{Name: "tv:blobArray:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 045 */ arrow.Field{Name: "tv:blobArray:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 046 */ arrow.Field{Name: "tv:blobArray:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 047 */ arrow.Field{Name: "tv:blobArray:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 048 */ arrow.Field{Name: "tv:blobArray:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 049 */ arrow.Field{Name: "tv:blobArray:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 050 */ arrow.Field{Name: "tv:blobArray:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 051 */ arrow.Field{Name: "tv:u8Array:value:val:u8h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint8)},
		/* 052 */ arrow.Field{Name: "tv:u8Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 053 */ arrow.Field{Name: "tv:u8Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 054 */ arrow.Field{Name: "tv:u8Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 055 */ arrow.Field{Name: "tv:u8Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 056 */ arrow.Field{Name: "tv:u8Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 057 */ arrow.Field{Name: "tv:u8Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 058 */ arrow.Field{Name: "tv:u8Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 059 */ arrow.Field{Name: "tv:u8Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 060 */ arrow.Field{Name: "tv:u16Array:value:val:u16h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint16)},
		/* 061 */ arrow.Field{Name: "tv:u16Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 062 */ arrow.Field{Name: "tv:u16Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 063 */ arrow.Field{Name: "tv:u16Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 064 */ arrow.Field{Name: "tv:u16Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 065 */ arrow.Field{Name: "tv:u16Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 066 */ arrow.Field{Name: "tv:u16Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 067 */ arrow.Field{Name: "tv:u16Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 068 */ arrow.Field{Name: "tv:u16Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 069 */ arrow.Field{Name: "tv:u32Array:value:val:u32h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint32)},
		/* 070 */ arrow.Field{Name: "tv:u32Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 071 */ arrow.Field{Name: "tv:u32Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 072 */ arrow.Field{Name: "tv:u32Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 073 */ arrow.Field{Name: "tv:u32Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 074 */ arrow.Field{Name: "tv:u32Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 075 */ arrow.Field{Name: "tv:u32Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 076 */ arrow.Field{Name: "tv:u32Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 077 */ arrow.Field{Name: "tv:u32Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 078 */ arrow.Field{Name: "tv:u32Set:value:val:u32m:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint32)},
		/* 079 */ arrow.Field{Name: "tv:u32Set:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 080 */ arrow.Field{Name: "tv:u32Set:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 081 */ arrow.Field{Name: "tv:u32Set:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 082 */ arrow.Field{Name: "tv:u32Set:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 083 */ arrow.Field{Name: "tv:u32Set:card:card:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 084 */ arrow.Field{Name: "tv:u32Set:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 085 */ arrow.Field{Name: "tv:u32Set:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 086 */ arrow.Field{Name: "tv:u32Set:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 087 */ arrow.Field{Name: "tv:u64Array:value:val:u64h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 088 */ arrow.Field{Name: "tv:u64Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 089 */ arrow.Field{Name: "tv:u64Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 090 */ arrow.Field{Name: "tv:u64Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 091 */ arrow.Field{Name: "tv:u64Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 092 */ arrow.Field{Name: "tv:u64Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 093 */ arrow.Field{Name: "tv:u64Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 094 */ arrow.Field{Name: "tv:u64Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 095 */ arrow.Field{Name: "tv:u64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 096 */ arrow.Field{Name: "tv:u64Set:value:val:u64m:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 097 */ arrow.Field{Name: "tv:u64Set:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 098 */ arrow.Field{Name: "tv:u64Set:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 099 */ arrow.Field{Name: "tv:u64Set:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 100 */ arrow.Field{Name: "tv:u64Set:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 101 */ arrow.Field{Name: "tv:u64Set:card:card:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 102 */ arrow.Field{Name: "tv:u64Set:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 103 */ arrow.Field{Name: "tv:u64Set:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 104 */ arrow.Field{Name: "tv:u64Set:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 105 */ arrow.Field{Name: "tv:i8Array:value:val:i8h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Int8)},
		/* 106 */ arrow.Field{Name: "tv:i8Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 107 */ arrow.Field{Name: "tv:i8Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 108 */ arrow.Field{Name: "tv:i8Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 109 */ arrow.Field{Name: "tv:i8Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 110 */ arrow.Field{Name: "tv:i8Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 111 */ arrow.Field{Name: "tv:i8Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 112 */ arrow.Field{Name: "tv:i8Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 113 */ arrow.Field{Name: "tv:i8Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 114 */ arrow.Field{Name: "tv:i16Array:value:val:i16h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Int16)},
		/* 115 */ arrow.Field{Name: "tv:i16Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 116 */ arrow.Field{Name: "tv:i16Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 117 */ arrow.Field{Name: "tv:i16Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 118 */ arrow.Field{Name: "tv:i16Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 119 */ arrow.Field{Name: "tv:i16Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 120 */ arrow.Field{Name: "tv:i16Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 121 */ arrow.Field{Name: "tv:i16Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 122 */ arrow.Field{Name: "tv:i16Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 123 */ arrow.Field{Name: "tv:i32Array:value:val:i32h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Int32)},
		/* 124 */ arrow.Field{Name: "tv:i32Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 125 */ arrow.Field{Name: "tv:i32Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 126 */ arrow.Field{Name: "tv:i32Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 127 */ arrow.Field{Name: "tv:i32Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 128 */ arrow.Field{Name: "tv:i32Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 129 */ arrow.Field{Name: "tv:i32Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 130 */ arrow.Field{Name: "tv:i32Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 131 */ arrow.Field{Name: "tv:i32Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 132 */ arrow.Field{Name: "tv:i64Array:value:val:i64h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Int64)},
		/* 133 */ arrow.Field{Name: "tv:i64Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 134 */ arrow.Field{Name: "tv:i64Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 135 */ arrow.Field{Name: "tv:i64Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 136 */ arrow.Field{Name: "tv:i64Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 137 */ arrow.Field{Name: "tv:i64Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 138 */ arrow.Field{Name: "tv:i64Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 139 */ arrow.Field{Name: "tv:i64Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 140 */ arrow.Field{Name: "tv:i64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 141 */ arrow.Field{Name: "tv:f32Array:value:val:f32h:gM:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Float32)},
		/* 142 */ arrow.Field{Name: "tv:f32Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 143 */ arrow.Field{Name: "tv:f32Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 144 */ arrow.Field{Name: "tv:f32Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 145 */ arrow.Field{Name: "tv:f32Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 146 */ arrow.Field{Name: "tv:f32Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 147 */ arrow.Field{Name: "tv:f32Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 148 */ arrow.Field{Name: "tv:f32Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 149 */ arrow.Field{Name: "tv:f32Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 150 */ arrow.Field{Name: "tv:f64Array:value:val:f64h:gM:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Float64)},
		/* 151 */ arrow.Field{Name: "tv:f64Array:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 152 */ arrow.Field{Name: "tv:f64Array:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 153 */ arrow.Field{Name: "tv:f64Array:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 154 */ arrow.Field{Name: "tv:f64Array:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 155 */ arrow.Field{Name: "tv:f64Array:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 156 */ arrow.Field{Name: "tv:f64Array:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 157 */ arrow.Field{Name: "tv:f64Array:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 158 */ arrow.Field{Name: "tv:f64Array:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 159 */ arrow.Field{Name: "tv:u32Range:beginIncl:val:u32:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint32)},
		/* 160 */ arrow.Field{Name: "tv:u32Range:endExcl:val:u32:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint32)},
		/* 161 */ arrow.Field{Name: "tv:u32Range:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 162 */ arrow.Field{Name: "tv:u32Range:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 163 */ arrow.Field{Name: "tv:u32Range:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 164 */ arrow.Field{Name: "tv:u32Range:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 165 */ arrow.Field{Name: "tv:u32Range:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 166 */ arrow.Field{Name: "tv:u32Range:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 167 */ arrow.Field{Name: "tv:u32Range:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 168 */ arrow.Field{Name: "tv:timeArray:value:val:z64h:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.TimestampType{Unit: arrow.Nanosecond})},
		/* 169 */ arrow.Field{Name: "tv:timeArray:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 170 */ arrow.Field{Name: "tv:timeArray:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 171 */ arrow.Field{Name: "tv:timeArray:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 172 */ arrow.Field{Name: "tv:timeArray:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 173 */ arrow.Field{Name: "tv:timeArray:len:len:u64:28o:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 174 */ arrow.Field{Name: "tv:timeArray:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 175 */ arrow.Field{Name: "tv:timeArray:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 176 */ arrow.Field{Name: "tv:timeArray:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 177 */ arrow.Field{Name: "tv:bool:value:val:b:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BooleanType{})},
		/* 178 */ arrow.Field{Name: "tv:bool:hr:hr:u64:2k:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 179 */ arrow.Field{Name: "tv:bool:lr:lr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 180 */ arrow.Field{Name: "tv:bool:lmr:lmr:u64:2q:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 181 */ arrow.Field{Name: "tv:bool:mrhp:mrhp:y:g:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(&arrow.BinaryType{})},
		/* 182 */ arrow.Field{Name: "tv:bool:hrcard:hrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 183 */ arrow.Field{Name: "tv:bool:lrcard:lrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
		/* 184 */ arrow.Field{Name: "tv:bool:lmrcard:lmrcard:u64:4gw:0:0:0::data", Nullable: false, Type: arrow.ListOfNonNullable(arrow.PrimitiveTypes.Uint64)},
	}, nil)
	return
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityClassAndFactoryCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1257

type InEntityFacts struct {
	errs           []error
	state          runtime.EntityStateE
	allocator      memory.Allocator
	builder        *array.RecordBuilder
	records        []arrow.RecordBatch
	section00Inst  *InEntityFactsSectionBlobArray
	section00State runtime.EntityStateE
	section01Inst  *InEntityFactsSectionBool
	section01State runtime.EntityStateE
	section02Inst  *InEntityFactsSectionF32Array
	section02State runtime.EntityStateE
	section03Inst  *InEntityFactsSectionF64Array
	section03State runtime.EntityStateE
	section04Inst  *InEntityFactsSectionForeignKey
	section04State runtime.EntityStateE
	section05Inst  *InEntityFactsSectionI16Array
	section05State runtime.EntityStateE
	section06Inst  *InEntityFactsSectionI32Array
	section06State runtime.EntityStateE
	section07Inst  *InEntityFactsSectionI64Array
	section07State runtime.EntityStateE
	section08Inst  *InEntityFactsSectionI8Array
	section08State runtime.EntityStateE
	section09Inst  *InEntityFactsSectionStringArray
	section09State runtime.EntityStateE
	section10Inst  *InEntityFactsSectionSymbol
	section10State runtime.EntityStateE
	section11Inst  *InEntityFactsSectionSymbolArray
	section11State runtime.EntityStateE
	section12Inst  *InEntityFactsSectionTextArray
	section12State runtime.EntityStateE
	section13Inst  *InEntityFactsSectionTimeArray
	section13State runtime.EntityStateE
	section14Inst  *InEntityFactsSectionU16Array
	section14State runtime.EntityStateE
	section15Inst  *InEntityFactsSectionU32Array
	section15State runtime.EntityStateE
	section16Inst  *InEntityFactsSectionU32Range
	section16State runtime.EntityStateE
	section17Inst  *InEntityFactsSectionU32Set
	section17State runtime.EntityStateE
	section18Inst  *InEntityFactsSectionU64Array
	section18State runtime.EntityStateE
	section19Inst  *InEntityFactsSectionU64Set
	section19State runtime.EntityStateE
	section20Inst  *InEntityFactsSectionU8Array
	section20State runtime.EntityStateE
	activeSections *[21]bool
	plainId0       uint64

	plainNaturalKey1 []byte

	plainTs2 time.Time

	plainExpiresAt3       time.Time
	scalarFieldBuilder000 *array.Uint64Builder

	scalarFieldBuilder001 *array.BinaryBuilder

	scalarFieldBuilder002 *array.TimestampBuilder

	scalarFieldBuilder003 *array.TimestampBuilder
}

func NewInEntityFacts(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntityFacts) {
	inst = &InEntityFacts{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.allocator = allocator
	inst.records = make([]arrow.RecordBatch, 0, estimatedNumberOfRecords)
	schema := CreateSchemaFacts()
	builder := array.NewRecordBuilder(allocator, schema)
	inst.builder = builder
	inst.initSections(builder)
	inst.scalarFieldBuilder000 = builder.Field(0).(*array.Uint64Builder)
	inst.scalarFieldBuilder001 = builder.Field(1).(*array.BinaryBuilder)
	inst.scalarFieldBuilder002 = builder.Field(2).(*array.TimestampBuilder)
	inst.scalarFieldBuilder003 = builder.Field(3).(*array.TimestampBuilder)

	return inst
}

// SetActiveSections marks which section indices BeginEntity should
// initialise (skipping beginSection for the rest). Pass nil to clear.
// The hint is a performance optimisation; sending BeginAttribute to
// an unmarked section produces empty-list bytes at TransferRecords.
func (inst *InEntityFacts) SetActiveSections(idxs []int) {
	if idxs == nil {
		inst.activeSections = nil
		return
	}
	var mask [21]bool
	for _, i := range idxs {
		if i >= 0 && i < len(mask) {
			mask[i] = true
		}
	}
	inst.activeSections = &mask
}

// Builder exposes the underlying RecordBuilder so callers can apply
// shim-level hints (e.g. SetActiveFields on the arrowrowbinary /
// arrowsparserb / arrowrowcbor backends).
func (inst *InEntityFacts) Builder() *array.RecordBuilder { return inst.builder }

// InEntityFactsSectionIndices maps each section name to its section%02dInst slot in
// the generated entity. Useful for callers that need to compute
// SetActiveSections inputs from section names — for example, the
// marshallgen-driven keelson codec wrappers.
var InEntityFactsSectionIndices = map[string]int{
	"blobArray":   0,
	"bool":        1,
	"f32Array":    2,
	"f64Array":    3,
	"foreignKey":  4,
	"i16Array":    5,
	"i32Array":    6,
	"i64Array":    7,
	"i8Array":     8,
	"stringArray": 9,
	"symbol":      10,
	"symbolArray": 11,
	"textArray":   12,
	"timeArray":   13,
	"u16Array":    14,
	"u32Array":    15,
	"u32Range":    16,
	"u32Set":      17,
	"u64Array":    18,
	"u64Set":      19,
	"u8Array":     20,
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1434

func (inst *InEntityFacts) SetId(id0 uint64, naturalKey1 []byte) *InEntityFacts {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainId0 = id0
	inst.plainNaturalKey1 = naturalKey1

	return inst
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1434

func (inst *InEntityFacts) SetTimestamp(ts2 time.Time) *InEntityFacts {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainTs2 = ts2

	return inst
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1434

func (inst *InEntityFacts) SetLifecycle(expiresAt3 time.Time) *InEntityFacts {
	if inst.state != runtime.EntityStateInEntity {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.plainExpiresAt3 = expiresAt3

	return inst
}
func (inst *InEntityFacts) appendPlainValues() {
	inst.scalarFieldBuilder000.Append(inst.plainId0)

	inst.scalarFieldBuilder001.Append(inst.plainNaturalKey1)

	inst.scalarFieldBuilder002.Append(arrow.Timestamp(inst.plainTs2.UnixNano()))

	inst.scalarFieldBuilder003.Append(arrow.Timestamp(inst.plainExpiresAt3.UnixNano()))
}
func (inst *InEntityFacts) resetPlainValues() {
	inst.plainId0 = uint64(0)

	inst.plainNaturalKey1 = []byte(nil)

	inst.plainTs2 = time.Time{}

	inst.plainExpiresAt3 = time.Time{}
}
func (inst *InEntityFacts) initSections(builder *array.RecordBuilder) {
	inst.section00Inst = NewInEntityFactsSectionBlobArray(builder, inst)
	inst.section01Inst = NewInEntityFactsSectionBool(builder, inst)
	inst.section02Inst = NewInEntityFactsSectionF32Array(builder, inst)
	inst.section03Inst = NewInEntityFactsSectionF64Array(builder, inst)
	inst.section04Inst = NewInEntityFactsSectionForeignKey(builder, inst)
	inst.section05Inst = NewInEntityFactsSectionI16Array(builder, inst)
	inst.section06Inst = NewInEntityFactsSectionI32Array(builder, inst)
	inst.section07Inst = NewInEntityFactsSectionI64Array(builder, inst)
	inst.section08Inst = NewInEntityFactsSectionI8Array(builder, inst)
	inst.section09Inst = NewInEntityFactsSectionStringArray(builder, inst)
	inst.section10Inst = NewInEntityFactsSectionSymbol(builder, inst)
	inst.section11Inst = NewInEntityFactsSectionSymbolArray(builder, inst)
	inst.section12Inst = NewInEntityFactsSectionTextArray(builder, inst)
	inst.section13Inst = NewInEntityFactsSectionTimeArray(builder, inst)
	inst.section14Inst = NewInEntityFactsSectionU16Array(builder, inst)
	inst.section15Inst = NewInEntityFactsSectionU32Array(builder, inst)
	inst.section16Inst = NewInEntityFactsSectionU32Range(builder, inst)
	inst.section17Inst = NewInEntityFactsSectionU32Set(builder, inst)
	inst.section18Inst = NewInEntityFactsSectionU64Array(builder, inst)
	inst.section19Inst = NewInEntityFactsSectionU64Set(builder, inst)
	inst.section20Inst = NewInEntityFactsSectionU8Array(builder, inst)
}
func (inst *InEntityFacts) beginSections() {
	if mask := inst.activeSections; mask != nil {
		if mask[0] {
			inst.section00Inst.beginSection()
		}
		if mask[1] {
			inst.section01Inst.beginSection()
		}
		if mask[2] {
			inst.section02Inst.beginSection()
		}
		if mask[3] {
			inst.section03Inst.beginSection()
		}
		if mask[4] {
			inst.section04Inst.beginSection()
		}
		if mask[5] {
			inst.section05Inst.beginSection()
		}
		if mask[6] {
			inst.section06Inst.beginSection()
		}
		if mask[7] {
			inst.section07Inst.beginSection()
		}
		if mask[8] {
			inst.section08Inst.beginSection()
		}
		if mask[9] {
			inst.section09Inst.beginSection()
		}
		if mask[10] {
			inst.section10Inst.beginSection()
		}
		if mask[11] {
			inst.section11Inst.beginSection()
		}
		if mask[12] {
			inst.section12Inst.beginSection()
		}
		if mask[13] {
			inst.section13Inst.beginSection()
		}
		if mask[14] {
			inst.section14Inst.beginSection()
		}
		if mask[15] {
			inst.section15Inst.beginSection()
		}
		if mask[16] {
			inst.section16Inst.beginSection()
		}
		if mask[17] {
			inst.section17Inst.beginSection()
		}
		if mask[18] {
			inst.section18Inst.beginSection()
		}
		if mask[19] {
			inst.section19Inst.beginSection()
		}
		if mask[20] {
			inst.section20Inst.beginSection()
		}
		return
	}
	inst.section00Inst.beginSection()
	inst.section01Inst.beginSection()
	inst.section02Inst.beginSection()
	inst.section03Inst.beginSection()
	inst.section04Inst.beginSection()
	inst.section05Inst.beginSection()
	inst.section06Inst.beginSection()
	inst.section07Inst.beginSection()
	inst.section08Inst.beginSection()
	inst.section09Inst.beginSection()
	inst.section10Inst.beginSection()
	inst.section11Inst.beginSection()
	inst.section12Inst.beginSection()
	inst.section13Inst.beginSection()
	inst.section14Inst.beginSection()
	inst.section15Inst.beginSection()
	inst.section16Inst.beginSection()
	inst.section17Inst.beginSection()
	inst.section18Inst.beginSection()
	inst.section19Inst.beginSection()
	inst.section20Inst.beginSection()
}
func (inst *InEntityFacts) resetSections() {
	inst.section00Inst.resetSection()
	inst.section01Inst.resetSection()
	inst.section02Inst.resetSection()
	inst.section03Inst.resetSection()
	inst.section04Inst.resetSection()
	inst.section05Inst.resetSection()
	inst.section06Inst.resetSection()
	inst.section07Inst.resetSection()
	inst.section08Inst.resetSection()
	inst.section09Inst.resetSection()
	inst.section10Inst.resetSection()
	inst.section11Inst.resetSection()
	inst.section12Inst.resetSection()
	inst.section13Inst.resetSection()
	inst.section14Inst.resetSection()
	inst.section15Inst.resetSection()
	inst.section16Inst.resetSection()
	inst.section17Inst.resetSection()
	inst.section18Inst.resetSection()
	inst.section19Inst.resetSection()
	inst.section20Inst.resetSection()
}
func (inst *InEntityFacts) CheckErrors() (err error) {
	err = eh.CheckErrors(inst.errs)
	err = errors.Join(err, inst.section00Inst.CheckErrors())
	err = errors.Join(err, inst.section01Inst.CheckErrors())
	err = errors.Join(err, inst.section02Inst.CheckErrors())
	err = errors.Join(err, inst.section03Inst.CheckErrors())
	err = errors.Join(err, inst.section04Inst.CheckErrors())
	err = errors.Join(err, inst.section05Inst.CheckErrors())
	err = errors.Join(err, inst.section06Inst.CheckErrors())
	err = errors.Join(err, inst.section07Inst.CheckErrors())
	err = errors.Join(err, inst.section08Inst.CheckErrors())
	err = errors.Join(err, inst.section09Inst.CheckErrors())
	err = errors.Join(err, inst.section10Inst.CheckErrors())
	err = errors.Join(err, inst.section11Inst.CheckErrors())
	err = errors.Join(err, inst.section12Inst.CheckErrors())
	err = errors.Join(err, inst.section13Inst.CheckErrors())
	err = errors.Join(err, inst.section14Inst.CheckErrors())
	err = errors.Join(err, inst.section15Inst.CheckErrors())
	err = errors.Join(err, inst.section16Inst.CheckErrors())
	err = errors.Join(err, inst.section17Inst.CheckErrors())
	err = errors.Join(err, inst.section18Inst.CheckErrors())
	err = errors.Join(err, inst.section19Inst.CheckErrors())
	err = errors.Join(err, inst.section20Inst.CheckErrors())

	return
}
func (inst *InEntityFacts) GetSectionBlobArray() *InEntityFactsSectionBlobArray {
	return inst.section00Inst
}
func (inst *InEntityFacts) GetSectionBool() *InEntityFactsSectionBool {
	return inst.section01Inst
}
func (inst *InEntityFacts) GetSectionF32Array() *InEntityFactsSectionF32Array {
	return inst.section02Inst
}
func (inst *InEntityFacts) GetSectionF64Array() *InEntityFactsSectionF64Array {
	return inst.section03Inst
}
func (inst *InEntityFacts) GetSectionForeignKey() *InEntityFactsSectionForeignKey {
	return inst.section04Inst
}
func (inst *InEntityFacts) GetSectionI16Array() *InEntityFactsSectionI16Array {
	return inst.section05Inst
}
func (inst *InEntityFacts) GetSectionI32Array() *InEntityFactsSectionI32Array {
	return inst.section06Inst
}
func (inst *InEntityFacts) GetSectionI64Array() *InEntityFactsSectionI64Array {
	return inst.section07Inst
}
func (inst *InEntityFacts) GetSectionI8Array() *InEntityFactsSectionI8Array {
	return inst.section08Inst
}
func (inst *InEntityFacts) GetSectionStringArray() *InEntityFactsSectionStringArray {
	return inst.section09Inst
}
func (inst *InEntityFacts) GetSectionSymbol() *InEntityFactsSectionSymbol {
	return inst.section10Inst
}
func (inst *InEntityFacts) GetSectionSymbolArray() *InEntityFactsSectionSymbolArray {
	return inst.section11Inst
}
func (inst *InEntityFacts) GetSectionTextArray() *InEntityFactsSectionTextArray {
	return inst.section12Inst
}
func (inst *InEntityFacts) GetSectionTimeArray() *InEntityFactsSectionTimeArray {
	return inst.section13Inst
}
func (inst *InEntityFacts) GetSectionU16Array() *InEntityFactsSectionU16Array {
	return inst.section14Inst
}
func (inst *InEntityFacts) GetSectionU32Array() *InEntityFactsSectionU32Array {
	return inst.section15Inst
}
func (inst *InEntityFacts) GetSectionU32Range() *InEntityFactsSectionU32Range {
	return inst.section16Inst
}
func (inst *InEntityFacts) GetSectionU32Set() *InEntityFactsSectionU32Set {
	return inst.section17Inst
}
func (inst *InEntityFacts) GetSectionU64Array() *InEntityFactsSectionU64Array {
	return inst.section18Inst
}
func (inst *InEntityFacts) GetSectionU64Set() *InEntityFactsSectionU64Set {
	return inst.section19Inst
}
func (inst *InEntityFacts) GetSectionU8Array() *InEntityFactsSectionU8Array {
	return inst.section20Inst
}
func (inst *InEntityFacts) BeginEntity() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInitial:
		inst.state = runtime.EntityStateInEntity
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}

	inst.beginSections()
	return inst
}
func (inst *InEntityFacts) validateEntity() {
	{
		state := inst.section00Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "blobArray").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section01Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "bool").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section02Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "f32Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section03Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "f64Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section04Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "foreignKey").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section05Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "i16Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section06Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "i32Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section07Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "i64Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section08Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "i8Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section09Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "stringArray").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section10Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "symbol").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section11Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "symbolArray").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section12Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "textArray").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section13Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "timeArray").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section14Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u16Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section15Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u32Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section16Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u32Range").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section17Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u32Set").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section18Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u64Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section19Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u64Set").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}
	{
		state := inst.section20Inst.state
		switch state {
		case runtime.EntityStateInAttribute:
			inst.AppendError(eb.Build().Str("section", "u8Array").Stringer("state", state).Errorf("wrong state: Check that .BeginAttribute() is followed by .EndAttribute()"))
			break
		}
	}

	// FIXME check coSectionGroup consistency
	return
}
func (inst *InEntityFacts) CommitEntity() (err error) {
	inst.validateEntity()
	err = inst.CheckErrors()
	if err != nil {
		err = eh.Errorf("unable to commit entity, found errors: %w", err)
		return
	}
	switch inst.state {
	case runtime.EntityStateInEntity:
		inst.state = runtime.EntityStateInitial
		break
	default:
		err = runtime.ErrInvalidStateTransition
		return
	}

	inst.appendPlainValues()
	inst.resetPlainValues()
	inst.resetSections()
	return
}
func (inst *InEntityFacts) RollbackEntity() (err error) {
	switch inst.state {
	case runtime.EntityStateInEntity:
		inst.state = runtime.EntityStateInitial
		break
	default:
		err = runtime.ErrInvalidStateTransition
		return
	}

	inst.appendPlainValues() // arrow fields must all have one row
	inst.resetPlainValues()
	inst.resetSections()
	rec := inst.builder.NewRecord()
	if rec.NumRows() > 1 {
		inst.records = append(inst.records, rec.NewSlice(0, rec.NumRows()-1))
	} else {
		// FIXME find better way to truncate builder
		inst.builder.NewRecord().Release()
	}
	rec.Release()
	return
}

// TransferRecords The returned Records must be Release()'d after use.
func (inst *InEntityFacts) TransferRecords(recordsIn []arrow.RecordBatch) (recordsOut []arrow.RecordBatch, err error) {
	if inst.state != runtime.EntityStateInitial {
		err = runtime.ErrInvalidStateTransition
		return
	}

	recordsOut = slices.Grow(recordsIn, len(inst.records)+1)
	copy(recordsOut, inst.records)
	clear(inst.records)
	inst.records = inst.records[:0]
	rec := inst.builder.NewRecord()
	if rec.NumRows() > 0 {
		recordsOut = append(recordsOut, rec)
	}
	return
}

func (inst *InEntityFacts) GetSchema() (schema *arrow.Schema) {
	return inst.builder.Schema()
}

func (inst *InEntityFacts) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFacts) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionBlobArray struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionBlobArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder042 *array.BinaryBuilder
	homogenousArrayListBuilder042  *array.ListBuilder
}

func NewInEntityFactsSectionBlobArray(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionBlobArray) {
	inst = &InEntityFactsSectionBlobArray{}
	inAttr := NewInEntityFactsSectionBlobArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder042 = builder.Field(42).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.homogenousArrayListBuilder042 = builder.Field(42).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionBlobArray) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionBlobArray) BeginAttribute() *InEntityFactsSectionBlobArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionBlobArray) BeginAttributeSingle(value42 []byte) *InEntityFactsSectionBlobArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value42)
}
func (inst *InEntityFactsSectionBlobArray) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionBlobArray) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionBlobArray) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionBlobArray) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionBlobArray) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionBlobArray) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionBlobArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionBlobArray
	homogenousArrayFieldBuilder042        *array.BinaryBuilder
	homogenousArrayListBuilder042         *array.ListBuilder
	membershipFieldBuilder043             *array.Uint64Builder
	membershipListBuilder043              *array.ListBuilder
	membershipFieldBuilder044             *array.Uint64Builder
	membershipListBuilder044              *array.ListBuilder
	membershipFieldBuilder045             *array.Uint64Builder
	membershipListBuilder045              *array.ListBuilder
	membershipFieldBuilder046             *array.BinaryBuilder
	membershipListBuilder046              *array.ListBuilder
	homogenousArraySupportFieldBuilder047 *array.Uint64Builder
	homogenousArraySupportListBuilder047  *array.ListBuilder
	membershipSupportFieldBuilder048      *array.Uint64Builder
	membershipSupportListBuilder048       *array.ListBuilder
	membershipSupportFieldBuilder049      *array.Uint64Builder
	membershipSupportListBuilder049       *array.ListBuilder
	membershipSupportFieldBuilder050      *array.Uint64Builder
	membershipSupportListBuilder050       *array.ListBuilder

	membershipContainerLength043 int

	membershipContainerLength044 int

	membershipContainerLength045 int

	membershipContainerLength046 int

	homogenousArrayContainerLength042 int
}

func NewInEntityFactsSectionBlobArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionBlobArray) (inst *InEntityFactsSectionBlobArrayInAttr) {
	inst = &InEntityFactsSectionBlobArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder042 = builder.Field(42).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.homogenousArrayListBuilder042 = builder.Field(42).(*array.ListBuilder)
	inst.membershipFieldBuilder043 = builder.Field(43).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder043 = builder.Field(43).(*array.ListBuilder)
	inst.membershipFieldBuilder044 = builder.Field(44).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder044 = builder.Field(44).(*array.ListBuilder)
	inst.membershipFieldBuilder045 = builder.Field(45).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder045 = builder.Field(45).(*array.ListBuilder)
	inst.membershipFieldBuilder046 = builder.Field(46).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder046 = builder.Field(46).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder047 = builder.Field(47).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder047 = builder.Field(47).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder048 = builder.Field(48).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder048 = builder.Field(48).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder049 = builder.Field(49).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder049 = builder.Field(49).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder050 = builder.Field(50).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder050 = builder.Field(50).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionBlobArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder042.Append(true)
	inst.membershipListBuilder043.Append(true)
	inst.membershipListBuilder044.Append(true)
	inst.membershipListBuilder045.Append(true)
	inst.membershipListBuilder046.Append(true)
	inst.homogenousArrayContainerLength042 = 0
	inst.membershipContainerLength043 = 0
	inst.membershipContainerLength044 = 0
	inst.membershipContainerLength045 = 0
	inst.membershipContainerLength046 = 0
	inst.homogenousArraySupportListBuilder047.Append(true)
	inst.membershipSupportListBuilder048.Append(true)
	inst.membershipSupportListBuilder049.Append(true)
	inst.membershipSupportListBuilder050.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddToContainer(value42 []byte) *InEntityFactsSectionBlobArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder042.Append(value42)
	inst.homogenousArrayContainerLength042++
	return inst
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddToContainerP(value42 []byte) {
	inst.AddToContainer(value42)
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddMembershipHighCardRef(hr43 uint64) *InEntityFactsSectionBlobArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder043.Append(hr43)
	inst.membershipContainerLength043++
	return inst
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddMembershipHighCardRefP(hr43 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder043.Append(hr43)
	inst.membershipContainerLength043++
	return
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddMembershipLowCardRef(lr44 uint64) *InEntityFactsSectionBlobArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder044.Append(lr44)
	inst.membershipContainerLength044++
	return inst
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddMembershipLowCardRefP(lr44 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder044.Append(lr44)
	inst.membershipContainerLength044++
	return
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddMembershipMixedLowCardRef(lmr45 uint64, mrhp46 []byte) *InEntityFactsSectionBlobArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder045.Append(lmr45)
	inst.membershipFieldBuilder046.Append(mrhp46)
	inst.membershipContainerLength045++
	inst.membershipContainerLength046++
	return inst
}
func (inst *InEntityFactsSectionBlobArrayInAttr) AddMembershipMixedLowCardRefP(lmr45 uint64, mrhp46 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder045.Append(lmr45)
	inst.membershipFieldBuilder046.Append(mrhp46)
	inst.membershipContainerLength045++
	inst.membershipContainerLength046++
	return
}
func (inst *InEntityFactsSectionBlobArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength043
	inst.membershipContainerLength043 = 0
	inst.membershipSupportFieldBuilder048.Append(uint64(l))
	l = inst.membershipContainerLength044
	inst.membershipContainerLength044 = 0
	inst.membershipSupportFieldBuilder049.Append(uint64(l))
	l = inst.membershipContainerLength045
	inst.membershipContainerLength045 = 0
	inst.membershipSupportFieldBuilder050.Append(uint64(l))
}
func (inst *InEntityFactsSectionBlobArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength042
	inst.homogenousArrayContainerLength042 = 0
	inst.homogenousArraySupportFieldBuilder047.Append(uint64(l))
}
func (inst *InEntityFactsSectionBlobArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionBlobArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionBlobArrayInAttr) EndAttribute() *InEntityFactsSectionBlobArray {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionBlobArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionBlobArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionBlobArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionBool struct {
	errs                  []error
	inAttr                *InEntityFactsSectionBoolInAttr
	state                 runtime.EntityStateE
	parent                *InEntityFacts
	scalarFieldBuilder177 *array.BooleanBuilder
	scalarListBuilder177  *array.ListBuilder
}

func NewInEntityFactsSectionBool(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionBool) {
	inst = &InEntityFactsSectionBool{}
	inAttr := NewInEntityFactsSectionBoolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder177 = builder.Field(177).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder177 = builder.Field(177).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionBool) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionBool) BeginAttribute(value177 bool) *InEntityFactsSectionBoolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder177.Append(value177)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionBool) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionBool) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionBool) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionBool) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionBool) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionBool) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionBoolInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityFactsSectionBool
	scalarFieldBuilder177            *array.BooleanBuilder
	scalarListBuilder177             *array.ListBuilder
	membershipFieldBuilder178        *array.Uint64Builder
	membershipListBuilder178         *array.ListBuilder
	membershipFieldBuilder179        *array.Uint64Builder
	membershipListBuilder179         *array.ListBuilder
	membershipFieldBuilder180        *array.Uint64Builder
	membershipListBuilder180         *array.ListBuilder
	membershipFieldBuilder181        *array.BinaryBuilder
	membershipListBuilder181         *array.ListBuilder
	membershipSupportFieldBuilder182 *array.Uint64Builder
	membershipSupportListBuilder182  *array.ListBuilder
	membershipSupportFieldBuilder183 *array.Uint64Builder
	membershipSupportListBuilder183  *array.ListBuilder
	membershipSupportFieldBuilder184 *array.Uint64Builder
	membershipSupportListBuilder184  *array.ListBuilder

	membershipContainerLength178 int

	membershipContainerLength179 int

	membershipContainerLength180 int

	membershipContainerLength181 int
}

func NewInEntityFactsSectionBoolInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionBool) (inst *InEntityFactsSectionBoolInAttr) {
	inst = &InEntityFactsSectionBoolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder177 = builder.Field(177).(*array.ListBuilder).ValueBuilder().(*array.BooleanBuilder)
	inst.scalarListBuilder177 = builder.Field(177).(*array.ListBuilder)
	inst.membershipFieldBuilder178 = builder.Field(178).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder178 = builder.Field(178).(*array.ListBuilder)
	inst.membershipFieldBuilder179 = builder.Field(179).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder179 = builder.Field(179).(*array.ListBuilder)
	inst.membershipFieldBuilder180 = builder.Field(180).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder180 = builder.Field(180).(*array.ListBuilder)
	inst.membershipFieldBuilder181 = builder.Field(181).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder181 = builder.Field(181).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder182 = builder.Field(182).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder182 = builder.Field(182).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder183 = builder.Field(183).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder183 = builder.Field(183).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder184 = builder.Field(184).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder184 = builder.Field(184).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionBoolInAttr) beginAttribute() {
	inst.membershipListBuilder178.Append(true)
	inst.membershipListBuilder179.Append(true)
	inst.membershipListBuilder180.Append(true)
	inst.membershipListBuilder181.Append(true)
	inst.membershipContainerLength178 = 0
	inst.membershipContainerLength179 = 0
	inst.membershipContainerLength180 = 0
	inst.membershipContainerLength181 = 0
	inst.scalarListBuilder177.Append(true)
	inst.membershipSupportListBuilder182.Append(true)
	inst.membershipSupportListBuilder183.Append(true)
	inst.membershipSupportListBuilder184.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionBoolInAttr) AddMembershipHighCardRef(hr178 uint64) *InEntityFactsSectionBoolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder178.Append(hr178)
	inst.membershipContainerLength178++
	return inst
}
func (inst *InEntityFactsSectionBoolInAttr) AddMembershipHighCardRefP(hr178 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder178.Append(hr178)
	inst.membershipContainerLength178++
	return
}
func (inst *InEntityFactsSectionBoolInAttr) AddMembershipLowCardRef(lr179 uint64) *InEntityFactsSectionBoolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder179.Append(lr179)
	inst.membershipContainerLength179++
	return inst
}
func (inst *InEntityFactsSectionBoolInAttr) AddMembershipLowCardRefP(lr179 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder179.Append(lr179)
	inst.membershipContainerLength179++
	return
}
func (inst *InEntityFactsSectionBoolInAttr) AddMembershipMixedLowCardRef(lmr180 uint64, mrhp181 []byte) *InEntityFactsSectionBoolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder180.Append(lmr180)
	inst.membershipFieldBuilder181.Append(mrhp181)
	inst.membershipContainerLength180++
	inst.membershipContainerLength181++
	return inst
}
func (inst *InEntityFactsSectionBoolInAttr) AddMembershipMixedLowCardRefP(lmr180 uint64, mrhp181 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder180.Append(lmr180)
	inst.membershipFieldBuilder181.Append(mrhp181)
	inst.membershipContainerLength180++
	inst.membershipContainerLength181++
	return
}
func (inst *InEntityFactsSectionBoolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength178
	inst.membershipContainerLength178 = 0
	inst.membershipSupportFieldBuilder182.Append(uint64(l))
	l = inst.membershipContainerLength179
	inst.membershipContainerLength179 = 0
	inst.membershipSupportFieldBuilder183.Append(uint64(l))
	l = inst.membershipContainerLength180
	inst.membershipContainerLength180 = 0
	inst.membershipSupportFieldBuilder184.Append(uint64(l))
}
func (inst *InEntityFactsSectionBoolInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityFactsSectionBoolInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionBoolInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionBoolInAttr) EndAttribute() *InEntityFactsSectionBool {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionBoolInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionBoolInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionBoolInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionF32Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionF32ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder141 *array.Float32Builder
	homogenousArrayListBuilder141  *array.ListBuilder
}

func NewInEntityFactsSectionF32Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionF32Array) {
	inst = &InEntityFactsSectionF32Array{}
	inAttr := NewInEntityFactsSectionF32ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder141 = builder.Field(141).(*array.ListBuilder).ValueBuilder().(*array.Float32Builder)
	inst.homogenousArrayListBuilder141 = builder.Field(141).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionF32Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionF32Array) BeginAttribute() *InEntityFactsSectionF32ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionF32Array) BeginAttributeSingle(value141 float32) *InEntityFactsSectionF32ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value141)
}
func (inst *InEntityFactsSectionF32Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionF32Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionF32Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionF32Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionF32Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionF32Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionF32ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionF32Array
	homogenousArrayFieldBuilder141        *array.Float32Builder
	homogenousArrayListBuilder141         *array.ListBuilder
	membershipFieldBuilder142             *array.Uint64Builder
	membershipListBuilder142              *array.ListBuilder
	membershipFieldBuilder143             *array.Uint64Builder
	membershipListBuilder143              *array.ListBuilder
	membershipFieldBuilder144             *array.Uint64Builder
	membershipListBuilder144              *array.ListBuilder
	membershipFieldBuilder145             *array.BinaryBuilder
	membershipListBuilder145              *array.ListBuilder
	homogenousArraySupportFieldBuilder146 *array.Uint64Builder
	homogenousArraySupportListBuilder146  *array.ListBuilder
	membershipSupportFieldBuilder147      *array.Uint64Builder
	membershipSupportListBuilder147       *array.ListBuilder
	membershipSupportFieldBuilder148      *array.Uint64Builder
	membershipSupportListBuilder148       *array.ListBuilder
	membershipSupportFieldBuilder149      *array.Uint64Builder
	membershipSupportListBuilder149       *array.ListBuilder

	membershipContainerLength142 int

	membershipContainerLength143 int

	membershipContainerLength144 int

	membershipContainerLength145 int

	homogenousArrayContainerLength141 int
}

func NewInEntityFactsSectionF32ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionF32Array) (inst *InEntityFactsSectionF32ArrayInAttr) {
	inst = &InEntityFactsSectionF32ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder141 = builder.Field(141).(*array.ListBuilder).ValueBuilder().(*array.Float32Builder)
	inst.homogenousArrayListBuilder141 = builder.Field(141).(*array.ListBuilder)
	inst.membershipFieldBuilder142 = builder.Field(142).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder142 = builder.Field(142).(*array.ListBuilder)
	inst.membershipFieldBuilder143 = builder.Field(143).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder143 = builder.Field(143).(*array.ListBuilder)
	inst.membershipFieldBuilder144 = builder.Field(144).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder144 = builder.Field(144).(*array.ListBuilder)
	inst.membershipFieldBuilder145 = builder.Field(145).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder145 = builder.Field(145).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder146 = builder.Field(146).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder146 = builder.Field(146).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder147 = builder.Field(147).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder147 = builder.Field(147).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder148 = builder.Field(148).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder148 = builder.Field(148).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder149 = builder.Field(149).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder149 = builder.Field(149).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionF32ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder141.Append(true)
	inst.membershipListBuilder142.Append(true)
	inst.membershipListBuilder143.Append(true)
	inst.membershipListBuilder144.Append(true)
	inst.membershipListBuilder145.Append(true)
	inst.homogenousArrayContainerLength141 = 0
	inst.membershipContainerLength142 = 0
	inst.membershipContainerLength143 = 0
	inst.membershipContainerLength144 = 0
	inst.membershipContainerLength145 = 0
	inst.homogenousArraySupportListBuilder146.Append(true)
	inst.membershipSupportListBuilder147.Append(true)
	inst.membershipSupportListBuilder148.Append(true)
	inst.membershipSupportListBuilder149.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddToContainer(value141 float32) *InEntityFactsSectionF32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder141.Append(value141)
	inst.homogenousArrayContainerLength141++
	return inst
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddToContainerP(value141 float32) {
	inst.AddToContainer(value141)
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddMembershipHighCardRef(hr142 uint64) *InEntityFactsSectionF32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder142.Append(hr142)
	inst.membershipContainerLength142++
	return inst
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddMembershipHighCardRefP(hr142 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder142.Append(hr142)
	inst.membershipContainerLength142++
	return
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddMembershipLowCardRef(lr143 uint64) *InEntityFactsSectionF32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder143.Append(lr143)
	inst.membershipContainerLength143++
	return inst
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddMembershipLowCardRefP(lr143 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder143.Append(lr143)
	inst.membershipContainerLength143++
	return
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddMembershipMixedLowCardRef(lmr144 uint64, mrhp145 []byte) *InEntityFactsSectionF32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder144.Append(lmr144)
	inst.membershipFieldBuilder145.Append(mrhp145)
	inst.membershipContainerLength144++
	inst.membershipContainerLength145++
	return inst
}
func (inst *InEntityFactsSectionF32ArrayInAttr) AddMembershipMixedLowCardRefP(lmr144 uint64, mrhp145 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder144.Append(lmr144)
	inst.membershipFieldBuilder145.Append(mrhp145)
	inst.membershipContainerLength144++
	inst.membershipContainerLength145++
	return
}
func (inst *InEntityFactsSectionF32ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength142
	inst.membershipContainerLength142 = 0
	inst.membershipSupportFieldBuilder147.Append(uint64(l))
	l = inst.membershipContainerLength143
	inst.membershipContainerLength143 = 0
	inst.membershipSupportFieldBuilder148.Append(uint64(l))
	l = inst.membershipContainerLength144
	inst.membershipContainerLength144 = 0
	inst.membershipSupportFieldBuilder149.Append(uint64(l))
}
func (inst *InEntityFactsSectionF32ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength141
	inst.homogenousArrayContainerLength141 = 0
	inst.homogenousArraySupportFieldBuilder146.Append(uint64(l))
}
func (inst *InEntityFactsSectionF32ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionF32ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionF32ArrayInAttr) EndAttribute() *InEntityFactsSectionF32Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionF32ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionF32ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionF32ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionF64Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionF64ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder150 *array.Float64Builder
	homogenousArrayListBuilder150  *array.ListBuilder
}

func NewInEntityFactsSectionF64Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionF64Array) {
	inst = &InEntityFactsSectionF64Array{}
	inAttr := NewInEntityFactsSectionF64ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder150 = builder.Field(150).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.homogenousArrayListBuilder150 = builder.Field(150).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionF64Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionF64Array) BeginAttribute() *InEntityFactsSectionF64ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionF64Array) BeginAttributeSingle(value150 float64) *InEntityFactsSectionF64ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value150)
}
func (inst *InEntityFactsSectionF64Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionF64Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionF64Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionF64Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionF64Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionF64Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionF64ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionF64Array
	homogenousArrayFieldBuilder150        *array.Float64Builder
	homogenousArrayListBuilder150         *array.ListBuilder
	membershipFieldBuilder151             *array.Uint64Builder
	membershipListBuilder151              *array.ListBuilder
	membershipFieldBuilder152             *array.Uint64Builder
	membershipListBuilder152              *array.ListBuilder
	membershipFieldBuilder153             *array.Uint64Builder
	membershipListBuilder153              *array.ListBuilder
	membershipFieldBuilder154             *array.BinaryBuilder
	membershipListBuilder154              *array.ListBuilder
	homogenousArraySupportFieldBuilder155 *array.Uint64Builder
	homogenousArraySupportListBuilder155  *array.ListBuilder
	membershipSupportFieldBuilder156      *array.Uint64Builder
	membershipSupportListBuilder156       *array.ListBuilder
	membershipSupportFieldBuilder157      *array.Uint64Builder
	membershipSupportListBuilder157       *array.ListBuilder
	membershipSupportFieldBuilder158      *array.Uint64Builder
	membershipSupportListBuilder158       *array.ListBuilder

	membershipContainerLength151 int

	membershipContainerLength152 int

	membershipContainerLength153 int

	membershipContainerLength154 int

	homogenousArrayContainerLength150 int
}

func NewInEntityFactsSectionF64ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionF64Array) (inst *InEntityFactsSectionF64ArrayInAttr) {
	inst = &InEntityFactsSectionF64ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder150 = builder.Field(150).(*array.ListBuilder).ValueBuilder().(*array.Float64Builder)
	inst.homogenousArrayListBuilder150 = builder.Field(150).(*array.ListBuilder)
	inst.membershipFieldBuilder151 = builder.Field(151).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder151 = builder.Field(151).(*array.ListBuilder)
	inst.membershipFieldBuilder152 = builder.Field(152).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder152 = builder.Field(152).(*array.ListBuilder)
	inst.membershipFieldBuilder153 = builder.Field(153).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder153 = builder.Field(153).(*array.ListBuilder)
	inst.membershipFieldBuilder154 = builder.Field(154).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder154 = builder.Field(154).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder155 = builder.Field(155).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder155 = builder.Field(155).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder156 = builder.Field(156).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder156 = builder.Field(156).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder157 = builder.Field(157).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder157 = builder.Field(157).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder158 = builder.Field(158).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder158 = builder.Field(158).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionF64ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder150.Append(true)
	inst.membershipListBuilder151.Append(true)
	inst.membershipListBuilder152.Append(true)
	inst.membershipListBuilder153.Append(true)
	inst.membershipListBuilder154.Append(true)
	inst.homogenousArrayContainerLength150 = 0
	inst.membershipContainerLength151 = 0
	inst.membershipContainerLength152 = 0
	inst.membershipContainerLength153 = 0
	inst.membershipContainerLength154 = 0
	inst.homogenousArraySupportListBuilder155.Append(true)
	inst.membershipSupportListBuilder156.Append(true)
	inst.membershipSupportListBuilder157.Append(true)
	inst.membershipSupportListBuilder158.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddToContainer(value150 float64) *InEntityFactsSectionF64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder150.Append(value150)
	inst.homogenousArrayContainerLength150++
	return inst
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddToContainerP(value150 float64) {
	inst.AddToContainer(value150)
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddMembershipHighCardRef(hr151 uint64) *InEntityFactsSectionF64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder151.Append(hr151)
	inst.membershipContainerLength151++
	return inst
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddMembershipHighCardRefP(hr151 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder151.Append(hr151)
	inst.membershipContainerLength151++
	return
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddMembershipLowCardRef(lr152 uint64) *InEntityFactsSectionF64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder152.Append(lr152)
	inst.membershipContainerLength152++
	return inst
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddMembershipLowCardRefP(lr152 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder152.Append(lr152)
	inst.membershipContainerLength152++
	return
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddMembershipMixedLowCardRef(lmr153 uint64, mrhp154 []byte) *InEntityFactsSectionF64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder153.Append(lmr153)
	inst.membershipFieldBuilder154.Append(mrhp154)
	inst.membershipContainerLength153++
	inst.membershipContainerLength154++
	return inst
}
func (inst *InEntityFactsSectionF64ArrayInAttr) AddMembershipMixedLowCardRefP(lmr153 uint64, mrhp154 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder153.Append(lmr153)
	inst.membershipFieldBuilder154.Append(mrhp154)
	inst.membershipContainerLength153++
	inst.membershipContainerLength154++
	return
}
func (inst *InEntityFactsSectionF64ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength151
	inst.membershipContainerLength151 = 0
	inst.membershipSupportFieldBuilder156.Append(uint64(l))
	l = inst.membershipContainerLength152
	inst.membershipContainerLength152 = 0
	inst.membershipSupportFieldBuilder157.Append(uint64(l))
	l = inst.membershipContainerLength153
	inst.membershipContainerLength153 = 0
	inst.membershipSupportFieldBuilder158.Append(uint64(l))
}
func (inst *InEntityFactsSectionF64ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength150
	inst.homogenousArrayContainerLength150 = 0
	inst.homogenousArraySupportFieldBuilder155.Append(uint64(l))
}
func (inst *InEntityFactsSectionF64ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionF64ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionF64ArrayInAttr) EndAttribute() *InEntityFactsSectionF64Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionF64ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionF64ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionF64ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionForeignKey struct {
	errs                  []error
	inAttr                *InEntityFactsSectionForeignKeyInAttr
	state                 runtime.EntityStateE
	parent                *InEntityFacts
	scalarFieldBuilder004 *array.Uint64Builder
	scalarListBuilder004  *array.ListBuilder
}

func NewInEntityFactsSectionForeignKey(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionForeignKey) {
	inst = &InEntityFactsSectionForeignKey{}
	inAttr := NewInEntityFactsSectionForeignKeyInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder004 = builder.Field(4).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionForeignKey) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionForeignKey) BeginAttribute(value4 uint64) *InEntityFactsSectionForeignKeyInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder004.Append(value4)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionForeignKey) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionForeignKey) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionForeignKey) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionForeignKey) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionForeignKey) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionForeignKey) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionForeignKeyInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityFactsSectionForeignKey
	scalarFieldBuilder004            *array.Uint64Builder
	scalarListBuilder004             *array.ListBuilder
	membershipFieldBuilder005        *array.Uint64Builder
	membershipListBuilder005         *array.ListBuilder
	membershipSupportFieldBuilder006 *array.Uint64Builder
	membershipSupportListBuilder006  *array.ListBuilder

	membershipContainerLength005 int
}

func NewInEntityFactsSectionForeignKeyInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionForeignKey) (inst *InEntityFactsSectionForeignKeyInAttr) {
	inst = &InEntityFactsSectionForeignKeyInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder004 = builder.Field(4).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.scalarListBuilder004 = builder.Field(4).(*array.ListBuilder)
	inst.membershipFieldBuilder005 = builder.Field(5).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder005 = builder.Field(5).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder006 = builder.Field(6).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder006 = builder.Field(6).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionForeignKeyInAttr) beginAttribute() {
	inst.membershipListBuilder005.Append(true)
	inst.membershipContainerLength005 = 0
	inst.scalarListBuilder004.Append(true)
	inst.membershipSupportListBuilder006.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionForeignKeyInAttr) AddMembershipLowCardRef(lr5 uint64) *InEntityFactsSectionForeignKeyInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder005.Append(lr5)
	inst.membershipContainerLength005++
	return inst
}
func (inst *InEntityFactsSectionForeignKeyInAttr) AddMembershipLowCardRefP(lr5 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder005.Append(lr5)
	inst.membershipContainerLength005++
	return
}
func (inst *InEntityFactsSectionForeignKeyInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength005
	inst.membershipContainerLength005 = 0
	inst.membershipSupportFieldBuilder006.Append(uint64(l))
}
func (inst *InEntityFactsSectionForeignKeyInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityFactsSectionForeignKeyInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionForeignKeyInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionForeignKeyInAttr) EndAttribute() *InEntityFactsSectionForeignKey {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionForeignKeyInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionForeignKeyInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionForeignKeyInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI16Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionI16ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder114 *array.Int16Builder
	homogenousArrayListBuilder114  *array.ListBuilder
}

func NewInEntityFactsSectionI16Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionI16Array) {
	inst = &InEntityFactsSectionI16Array{}
	inAttr := NewInEntityFactsSectionI16ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder114 = builder.Field(114).(*array.ListBuilder).ValueBuilder().(*array.Int16Builder)
	inst.homogenousArrayListBuilder114 = builder.Field(114).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI16Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionI16Array) BeginAttribute() *InEntityFactsSectionI16ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionI16Array) BeginAttributeSingle(value114 int16) *InEntityFactsSectionI16ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value114)
}
func (inst *InEntityFactsSectionI16Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionI16Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionI16Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionI16Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionI16Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI16Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI16ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionI16Array
	homogenousArrayFieldBuilder114        *array.Int16Builder
	homogenousArrayListBuilder114         *array.ListBuilder
	membershipFieldBuilder115             *array.Uint64Builder
	membershipListBuilder115              *array.ListBuilder
	membershipFieldBuilder116             *array.Uint64Builder
	membershipListBuilder116              *array.ListBuilder
	membershipFieldBuilder117             *array.Uint64Builder
	membershipListBuilder117              *array.ListBuilder
	membershipFieldBuilder118             *array.BinaryBuilder
	membershipListBuilder118              *array.ListBuilder
	homogenousArraySupportFieldBuilder119 *array.Uint64Builder
	homogenousArraySupportListBuilder119  *array.ListBuilder
	membershipSupportFieldBuilder120      *array.Uint64Builder
	membershipSupportListBuilder120       *array.ListBuilder
	membershipSupportFieldBuilder121      *array.Uint64Builder
	membershipSupportListBuilder121       *array.ListBuilder
	membershipSupportFieldBuilder122      *array.Uint64Builder
	membershipSupportListBuilder122       *array.ListBuilder

	membershipContainerLength115 int

	membershipContainerLength116 int

	membershipContainerLength117 int

	membershipContainerLength118 int

	homogenousArrayContainerLength114 int
}

func NewInEntityFactsSectionI16ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionI16Array) (inst *InEntityFactsSectionI16ArrayInAttr) {
	inst = &InEntityFactsSectionI16ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder114 = builder.Field(114).(*array.ListBuilder).ValueBuilder().(*array.Int16Builder)
	inst.homogenousArrayListBuilder114 = builder.Field(114).(*array.ListBuilder)
	inst.membershipFieldBuilder115 = builder.Field(115).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder115 = builder.Field(115).(*array.ListBuilder)
	inst.membershipFieldBuilder116 = builder.Field(116).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder116 = builder.Field(116).(*array.ListBuilder)
	inst.membershipFieldBuilder117 = builder.Field(117).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder117 = builder.Field(117).(*array.ListBuilder)
	inst.membershipFieldBuilder118 = builder.Field(118).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder118 = builder.Field(118).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder119 = builder.Field(119).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder119 = builder.Field(119).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder120 = builder.Field(120).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder120 = builder.Field(120).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder121 = builder.Field(121).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder121 = builder.Field(121).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder122 = builder.Field(122).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder122 = builder.Field(122).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI16ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder114.Append(true)
	inst.membershipListBuilder115.Append(true)
	inst.membershipListBuilder116.Append(true)
	inst.membershipListBuilder117.Append(true)
	inst.membershipListBuilder118.Append(true)
	inst.homogenousArrayContainerLength114 = 0
	inst.membershipContainerLength115 = 0
	inst.membershipContainerLength116 = 0
	inst.membershipContainerLength117 = 0
	inst.membershipContainerLength118 = 0
	inst.homogenousArraySupportListBuilder119.Append(true)
	inst.membershipSupportListBuilder120.Append(true)
	inst.membershipSupportListBuilder121.Append(true)
	inst.membershipSupportListBuilder122.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddToContainer(value114 int16) *InEntityFactsSectionI16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder114.Append(value114)
	inst.homogenousArrayContainerLength114++
	return inst
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddToContainerP(value114 int16) {
	inst.AddToContainer(value114)
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddMembershipHighCardRef(hr115 uint64) *InEntityFactsSectionI16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder115.Append(hr115)
	inst.membershipContainerLength115++
	return inst
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddMembershipHighCardRefP(hr115 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder115.Append(hr115)
	inst.membershipContainerLength115++
	return
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddMembershipLowCardRef(lr116 uint64) *InEntityFactsSectionI16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder116.Append(lr116)
	inst.membershipContainerLength116++
	return inst
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddMembershipLowCardRefP(lr116 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder116.Append(lr116)
	inst.membershipContainerLength116++
	return
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddMembershipMixedLowCardRef(lmr117 uint64, mrhp118 []byte) *InEntityFactsSectionI16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder117.Append(lmr117)
	inst.membershipFieldBuilder118.Append(mrhp118)
	inst.membershipContainerLength117++
	inst.membershipContainerLength118++
	return inst
}
func (inst *InEntityFactsSectionI16ArrayInAttr) AddMembershipMixedLowCardRefP(lmr117 uint64, mrhp118 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder117.Append(lmr117)
	inst.membershipFieldBuilder118.Append(mrhp118)
	inst.membershipContainerLength117++
	inst.membershipContainerLength118++
	return
}
func (inst *InEntityFactsSectionI16ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength115
	inst.membershipContainerLength115 = 0
	inst.membershipSupportFieldBuilder120.Append(uint64(l))
	l = inst.membershipContainerLength116
	inst.membershipContainerLength116 = 0
	inst.membershipSupportFieldBuilder121.Append(uint64(l))
	l = inst.membershipContainerLength117
	inst.membershipContainerLength117 = 0
	inst.membershipSupportFieldBuilder122.Append(uint64(l))
}
func (inst *InEntityFactsSectionI16ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength114
	inst.homogenousArrayContainerLength114 = 0
	inst.homogenousArraySupportFieldBuilder119.Append(uint64(l))
}
func (inst *InEntityFactsSectionI16ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionI16ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionI16ArrayInAttr) EndAttribute() *InEntityFactsSectionI16Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionI16ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionI16ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI16ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI32Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionI32ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder123 *array.Int32Builder
	homogenousArrayListBuilder123  *array.ListBuilder
}

func NewInEntityFactsSectionI32Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionI32Array) {
	inst = &InEntityFactsSectionI32Array{}
	inAttr := NewInEntityFactsSectionI32ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder123 = builder.Field(123).(*array.ListBuilder).ValueBuilder().(*array.Int32Builder)
	inst.homogenousArrayListBuilder123 = builder.Field(123).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI32Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionI32Array) BeginAttribute() *InEntityFactsSectionI32ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionI32Array) BeginAttributeSingle(value123 int32) *InEntityFactsSectionI32ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value123)
}
func (inst *InEntityFactsSectionI32Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionI32Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionI32Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionI32Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionI32Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI32Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI32ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionI32Array
	homogenousArrayFieldBuilder123        *array.Int32Builder
	homogenousArrayListBuilder123         *array.ListBuilder
	membershipFieldBuilder124             *array.Uint64Builder
	membershipListBuilder124              *array.ListBuilder
	membershipFieldBuilder125             *array.Uint64Builder
	membershipListBuilder125              *array.ListBuilder
	membershipFieldBuilder126             *array.Uint64Builder
	membershipListBuilder126              *array.ListBuilder
	membershipFieldBuilder127             *array.BinaryBuilder
	membershipListBuilder127              *array.ListBuilder
	homogenousArraySupportFieldBuilder128 *array.Uint64Builder
	homogenousArraySupportListBuilder128  *array.ListBuilder
	membershipSupportFieldBuilder129      *array.Uint64Builder
	membershipSupportListBuilder129       *array.ListBuilder
	membershipSupportFieldBuilder130      *array.Uint64Builder
	membershipSupportListBuilder130       *array.ListBuilder
	membershipSupportFieldBuilder131      *array.Uint64Builder
	membershipSupportListBuilder131       *array.ListBuilder

	membershipContainerLength124 int

	membershipContainerLength125 int

	membershipContainerLength126 int

	membershipContainerLength127 int

	homogenousArrayContainerLength123 int
}

func NewInEntityFactsSectionI32ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionI32Array) (inst *InEntityFactsSectionI32ArrayInAttr) {
	inst = &InEntityFactsSectionI32ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder123 = builder.Field(123).(*array.ListBuilder).ValueBuilder().(*array.Int32Builder)
	inst.homogenousArrayListBuilder123 = builder.Field(123).(*array.ListBuilder)
	inst.membershipFieldBuilder124 = builder.Field(124).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder124 = builder.Field(124).(*array.ListBuilder)
	inst.membershipFieldBuilder125 = builder.Field(125).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder125 = builder.Field(125).(*array.ListBuilder)
	inst.membershipFieldBuilder126 = builder.Field(126).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder126 = builder.Field(126).(*array.ListBuilder)
	inst.membershipFieldBuilder127 = builder.Field(127).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder127 = builder.Field(127).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder128 = builder.Field(128).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder128 = builder.Field(128).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder129 = builder.Field(129).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder129 = builder.Field(129).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder130 = builder.Field(130).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder130 = builder.Field(130).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder131 = builder.Field(131).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder131 = builder.Field(131).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI32ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder123.Append(true)
	inst.membershipListBuilder124.Append(true)
	inst.membershipListBuilder125.Append(true)
	inst.membershipListBuilder126.Append(true)
	inst.membershipListBuilder127.Append(true)
	inst.homogenousArrayContainerLength123 = 0
	inst.membershipContainerLength124 = 0
	inst.membershipContainerLength125 = 0
	inst.membershipContainerLength126 = 0
	inst.membershipContainerLength127 = 0
	inst.homogenousArraySupportListBuilder128.Append(true)
	inst.membershipSupportListBuilder129.Append(true)
	inst.membershipSupportListBuilder130.Append(true)
	inst.membershipSupportListBuilder131.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddToContainer(value123 int32) *InEntityFactsSectionI32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder123.Append(value123)
	inst.homogenousArrayContainerLength123++
	return inst
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddToContainerP(value123 int32) {
	inst.AddToContainer(value123)
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddMembershipHighCardRef(hr124 uint64) *InEntityFactsSectionI32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder124.Append(hr124)
	inst.membershipContainerLength124++
	return inst
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddMembershipHighCardRefP(hr124 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder124.Append(hr124)
	inst.membershipContainerLength124++
	return
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddMembershipLowCardRef(lr125 uint64) *InEntityFactsSectionI32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder125.Append(lr125)
	inst.membershipContainerLength125++
	return inst
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddMembershipLowCardRefP(lr125 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder125.Append(lr125)
	inst.membershipContainerLength125++
	return
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddMembershipMixedLowCardRef(lmr126 uint64, mrhp127 []byte) *InEntityFactsSectionI32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder126.Append(lmr126)
	inst.membershipFieldBuilder127.Append(mrhp127)
	inst.membershipContainerLength126++
	inst.membershipContainerLength127++
	return inst
}
func (inst *InEntityFactsSectionI32ArrayInAttr) AddMembershipMixedLowCardRefP(lmr126 uint64, mrhp127 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder126.Append(lmr126)
	inst.membershipFieldBuilder127.Append(mrhp127)
	inst.membershipContainerLength126++
	inst.membershipContainerLength127++
	return
}
func (inst *InEntityFactsSectionI32ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength124
	inst.membershipContainerLength124 = 0
	inst.membershipSupportFieldBuilder129.Append(uint64(l))
	l = inst.membershipContainerLength125
	inst.membershipContainerLength125 = 0
	inst.membershipSupportFieldBuilder130.Append(uint64(l))
	l = inst.membershipContainerLength126
	inst.membershipContainerLength126 = 0
	inst.membershipSupportFieldBuilder131.Append(uint64(l))
}
func (inst *InEntityFactsSectionI32ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength123
	inst.homogenousArrayContainerLength123 = 0
	inst.homogenousArraySupportFieldBuilder128.Append(uint64(l))
}
func (inst *InEntityFactsSectionI32ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionI32ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionI32ArrayInAttr) EndAttribute() *InEntityFactsSectionI32Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionI32ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionI32ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI32ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI64Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionI64ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder132 *array.Int64Builder
	homogenousArrayListBuilder132  *array.ListBuilder
}

func NewInEntityFactsSectionI64Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionI64Array) {
	inst = &InEntityFactsSectionI64Array{}
	inAttr := NewInEntityFactsSectionI64ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder132 = builder.Field(132).(*array.ListBuilder).ValueBuilder().(*array.Int64Builder)
	inst.homogenousArrayListBuilder132 = builder.Field(132).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI64Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionI64Array) BeginAttribute() *InEntityFactsSectionI64ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionI64Array) BeginAttributeSingle(value132 int64) *InEntityFactsSectionI64ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value132)
}
func (inst *InEntityFactsSectionI64Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionI64Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionI64Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionI64Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionI64Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI64Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI64ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionI64Array
	homogenousArrayFieldBuilder132        *array.Int64Builder
	homogenousArrayListBuilder132         *array.ListBuilder
	membershipFieldBuilder133             *array.Uint64Builder
	membershipListBuilder133              *array.ListBuilder
	membershipFieldBuilder134             *array.Uint64Builder
	membershipListBuilder134              *array.ListBuilder
	membershipFieldBuilder135             *array.Uint64Builder
	membershipListBuilder135              *array.ListBuilder
	membershipFieldBuilder136             *array.BinaryBuilder
	membershipListBuilder136              *array.ListBuilder
	homogenousArraySupportFieldBuilder137 *array.Uint64Builder
	homogenousArraySupportListBuilder137  *array.ListBuilder
	membershipSupportFieldBuilder138      *array.Uint64Builder
	membershipSupportListBuilder138       *array.ListBuilder
	membershipSupportFieldBuilder139      *array.Uint64Builder
	membershipSupportListBuilder139       *array.ListBuilder
	membershipSupportFieldBuilder140      *array.Uint64Builder
	membershipSupportListBuilder140       *array.ListBuilder

	membershipContainerLength133 int

	membershipContainerLength134 int

	membershipContainerLength135 int

	membershipContainerLength136 int

	homogenousArrayContainerLength132 int
}

func NewInEntityFactsSectionI64ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionI64Array) (inst *InEntityFactsSectionI64ArrayInAttr) {
	inst = &InEntityFactsSectionI64ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder132 = builder.Field(132).(*array.ListBuilder).ValueBuilder().(*array.Int64Builder)
	inst.homogenousArrayListBuilder132 = builder.Field(132).(*array.ListBuilder)
	inst.membershipFieldBuilder133 = builder.Field(133).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder133 = builder.Field(133).(*array.ListBuilder)
	inst.membershipFieldBuilder134 = builder.Field(134).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder134 = builder.Field(134).(*array.ListBuilder)
	inst.membershipFieldBuilder135 = builder.Field(135).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder135 = builder.Field(135).(*array.ListBuilder)
	inst.membershipFieldBuilder136 = builder.Field(136).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder136 = builder.Field(136).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder137 = builder.Field(137).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder137 = builder.Field(137).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder138 = builder.Field(138).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder138 = builder.Field(138).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder139 = builder.Field(139).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder139 = builder.Field(139).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder140 = builder.Field(140).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder140 = builder.Field(140).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI64ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder132.Append(true)
	inst.membershipListBuilder133.Append(true)
	inst.membershipListBuilder134.Append(true)
	inst.membershipListBuilder135.Append(true)
	inst.membershipListBuilder136.Append(true)
	inst.homogenousArrayContainerLength132 = 0
	inst.membershipContainerLength133 = 0
	inst.membershipContainerLength134 = 0
	inst.membershipContainerLength135 = 0
	inst.membershipContainerLength136 = 0
	inst.homogenousArraySupportListBuilder137.Append(true)
	inst.membershipSupportListBuilder138.Append(true)
	inst.membershipSupportListBuilder139.Append(true)
	inst.membershipSupportListBuilder140.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddToContainer(value132 int64) *InEntityFactsSectionI64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder132.Append(value132)
	inst.homogenousArrayContainerLength132++
	return inst
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddToContainerP(value132 int64) {
	inst.AddToContainer(value132)
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddMembershipHighCardRef(hr133 uint64) *InEntityFactsSectionI64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder133.Append(hr133)
	inst.membershipContainerLength133++
	return inst
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddMembershipHighCardRefP(hr133 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder133.Append(hr133)
	inst.membershipContainerLength133++
	return
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddMembershipLowCardRef(lr134 uint64) *InEntityFactsSectionI64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder134.Append(lr134)
	inst.membershipContainerLength134++
	return inst
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddMembershipLowCardRefP(lr134 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder134.Append(lr134)
	inst.membershipContainerLength134++
	return
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddMembershipMixedLowCardRef(lmr135 uint64, mrhp136 []byte) *InEntityFactsSectionI64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder135.Append(lmr135)
	inst.membershipFieldBuilder136.Append(mrhp136)
	inst.membershipContainerLength135++
	inst.membershipContainerLength136++
	return inst
}
func (inst *InEntityFactsSectionI64ArrayInAttr) AddMembershipMixedLowCardRefP(lmr135 uint64, mrhp136 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder135.Append(lmr135)
	inst.membershipFieldBuilder136.Append(mrhp136)
	inst.membershipContainerLength135++
	inst.membershipContainerLength136++
	return
}
func (inst *InEntityFactsSectionI64ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength133
	inst.membershipContainerLength133 = 0
	inst.membershipSupportFieldBuilder138.Append(uint64(l))
	l = inst.membershipContainerLength134
	inst.membershipContainerLength134 = 0
	inst.membershipSupportFieldBuilder139.Append(uint64(l))
	l = inst.membershipContainerLength135
	inst.membershipContainerLength135 = 0
	inst.membershipSupportFieldBuilder140.Append(uint64(l))
}
func (inst *InEntityFactsSectionI64ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength132
	inst.homogenousArrayContainerLength132 = 0
	inst.homogenousArraySupportFieldBuilder137.Append(uint64(l))
}
func (inst *InEntityFactsSectionI64ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionI64ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionI64ArrayInAttr) EndAttribute() *InEntityFactsSectionI64Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionI64ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionI64ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI64ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI8Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionI8ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder105 *array.Int8Builder
	homogenousArrayListBuilder105  *array.ListBuilder
}

func NewInEntityFactsSectionI8Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionI8Array) {
	inst = &InEntityFactsSectionI8Array{}
	inAttr := NewInEntityFactsSectionI8ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder105 = builder.Field(105).(*array.ListBuilder).ValueBuilder().(*array.Int8Builder)
	inst.homogenousArrayListBuilder105 = builder.Field(105).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI8Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionI8Array) BeginAttribute() *InEntityFactsSectionI8ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionI8Array) BeginAttributeSingle(value105 int8) *InEntityFactsSectionI8ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value105)
}
func (inst *InEntityFactsSectionI8Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionI8Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionI8Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionI8Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionI8Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI8Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionI8ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionI8Array
	homogenousArrayFieldBuilder105        *array.Int8Builder
	homogenousArrayListBuilder105         *array.ListBuilder
	membershipFieldBuilder106             *array.Uint64Builder
	membershipListBuilder106              *array.ListBuilder
	membershipFieldBuilder107             *array.Uint64Builder
	membershipListBuilder107              *array.ListBuilder
	membershipFieldBuilder108             *array.Uint64Builder
	membershipListBuilder108              *array.ListBuilder
	membershipFieldBuilder109             *array.BinaryBuilder
	membershipListBuilder109              *array.ListBuilder
	homogenousArraySupportFieldBuilder110 *array.Uint64Builder
	homogenousArraySupportListBuilder110  *array.ListBuilder
	membershipSupportFieldBuilder111      *array.Uint64Builder
	membershipSupportListBuilder111       *array.ListBuilder
	membershipSupportFieldBuilder112      *array.Uint64Builder
	membershipSupportListBuilder112       *array.ListBuilder
	membershipSupportFieldBuilder113      *array.Uint64Builder
	membershipSupportListBuilder113       *array.ListBuilder

	membershipContainerLength106 int

	membershipContainerLength107 int

	membershipContainerLength108 int

	membershipContainerLength109 int

	homogenousArrayContainerLength105 int
}

func NewInEntityFactsSectionI8ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionI8Array) (inst *InEntityFactsSectionI8ArrayInAttr) {
	inst = &InEntityFactsSectionI8ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder105 = builder.Field(105).(*array.ListBuilder).ValueBuilder().(*array.Int8Builder)
	inst.homogenousArrayListBuilder105 = builder.Field(105).(*array.ListBuilder)
	inst.membershipFieldBuilder106 = builder.Field(106).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder106 = builder.Field(106).(*array.ListBuilder)
	inst.membershipFieldBuilder107 = builder.Field(107).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder107 = builder.Field(107).(*array.ListBuilder)
	inst.membershipFieldBuilder108 = builder.Field(108).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder108 = builder.Field(108).(*array.ListBuilder)
	inst.membershipFieldBuilder109 = builder.Field(109).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder109 = builder.Field(109).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder110 = builder.Field(110).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder110 = builder.Field(110).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder111 = builder.Field(111).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder111 = builder.Field(111).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder112 = builder.Field(112).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder112 = builder.Field(112).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder113 = builder.Field(113).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder113 = builder.Field(113).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionI8ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder105.Append(true)
	inst.membershipListBuilder106.Append(true)
	inst.membershipListBuilder107.Append(true)
	inst.membershipListBuilder108.Append(true)
	inst.membershipListBuilder109.Append(true)
	inst.homogenousArrayContainerLength105 = 0
	inst.membershipContainerLength106 = 0
	inst.membershipContainerLength107 = 0
	inst.membershipContainerLength108 = 0
	inst.membershipContainerLength109 = 0
	inst.homogenousArraySupportListBuilder110.Append(true)
	inst.membershipSupportListBuilder111.Append(true)
	inst.membershipSupportListBuilder112.Append(true)
	inst.membershipSupportListBuilder113.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddToContainer(value105 int8) *InEntityFactsSectionI8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder105.Append(value105)
	inst.homogenousArrayContainerLength105++
	return inst
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddToContainerP(value105 int8) {
	inst.AddToContainer(value105)
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddMembershipHighCardRef(hr106 uint64) *InEntityFactsSectionI8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder106.Append(hr106)
	inst.membershipContainerLength106++
	return inst
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddMembershipHighCardRefP(hr106 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder106.Append(hr106)
	inst.membershipContainerLength106++
	return
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddMembershipLowCardRef(lr107 uint64) *InEntityFactsSectionI8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder107.Append(lr107)
	inst.membershipContainerLength107++
	return inst
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddMembershipLowCardRefP(lr107 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder107.Append(lr107)
	inst.membershipContainerLength107++
	return
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddMembershipMixedLowCardRef(lmr108 uint64, mrhp109 []byte) *InEntityFactsSectionI8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder108.Append(lmr108)
	inst.membershipFieldBuilder109.Append(mrhp109)
	inst.membershipContainerLength108++
	inst.membershipContainerLength109++
	return inst
}
func (inst *InEntityFactsSectionI8ArrayInAttr) AddMembershipMixedLowCardRefP(lmr108 uint64, mrhp109 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder108.Append(lmr108)
	inst.membershipFieldBuilder109.Append(mrhp109)
	inst.membershipContainerLength108++
	inst.membershipContainerLength109++
	return
}
func (inst *InEntityFactsSectionI8ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength106
	inst.membershipContainerLength106 = 0
	inst.membershipSupportFieldBuilder111.Append(uint64(l))
	l = inst.membershipContainerLength107
	inst.membershipContainerLength107 = 0
	inst.membershipSupportFieldBuilder112.Append(uint64(l))
	l = inst.membershipContainerLength108
	inst.membershipContainerLength108 = 0
	inst.membershipSupportFieldBuilder113.Append(uint64(l))
}
func (inst *InEntityFactsSectionI8ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength105
	inst.homogenousArrayContainerLength105 = 0
	inst.homogenousArraySupportFieldBuilder110.Append(uint64(l))
}
func (inst *InEntityFactsSectionI8ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionI8ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionI8ArrayInAttr) EndAttribute() *InEntityFactsSectionI8Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionI8ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionI8ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionI8ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionStringArray struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionStringArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder016 *array.StringBuilder
	homogenousArrayListBuilder016  *array.ListBuilder
}

func NewInEntityFactsSectionStringArray(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionStringArray) {
	inst = &InEntityFactsSectionStringArray{}
	inAttr := NewInEntityFactsSectionStringArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder016 = builder.Field(16).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionStringArray) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionStringArray) BeginAttribute() *InEntityFactsSectionStringArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionStringArray) BeginAttributeSingle(value16 string) *InEntityFactsSectionStringArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value16)
}
func (inst *InEntityFactsSectionStringArray) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionStringArray) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionStringArray) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionStringArray) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionStringArray) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionStringArray) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionStringArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionStringArray
	homogenousArrayFieldBuilder016        *array.StringBuilder
	homogenousArrayListBuilder016         *array.ListBuilder
	membershipFieldBuilder017             *array.Uint64Builder
	membershipListBuilder017              *array.ListBuilder
	membershipFieldBuilder018             *array.Uint64Builder
	membershipListBuilder018              *array.ListBuilder
	membershipFieldBuilder019             *array.Uint64Builder
	membershipListBuilder019              *array.ListBuilder
	membershipFieldBuilder020             *array.BinaryBuilder
	membershipListBuilder020              *array.ListBuilder
	homogenousArraySupportFieldBuilder021 *array.Uint64Builder
	homogenousArraySupportListBuilder021  *array.ListBuilder
	membershipSupportFieldBuilder022      *array.Uint64Builder
	membershipSupportListBuilder022       *array.ListBuilder
	membershipSupportFieldBuilder023      *array.Uint64Builder
	membershipSupportListBuilder023       *array.ListBuilder
	membershipSupportFieldBuilder024      *array.Uint64Builder
	membershipSupportListBuilder024       *array.ListBuilder

	membershipContainerLength017 int

	membershipContainerLength018 int

	membershipContainerLength019 int

	membershipContainerLength020 int

	homogenousArrayContainerLength016 int
}

func NewInEntityFactsSectionStringArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionStringArray) (inst *InEntityFactsSectionStringArrayInAttr) {
	inst = &InEntityFactsSectionStringArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder016 = builder.Field(16).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder016 = builder.Field(16).(*array.ListBuilder)
	inst.membershipFieldBuilder017 = builder.Field(17).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder017 = builder.Field(17).(*array.ListBuilder)
	inst.membershipFieldBuilder018 = builder.Field(18).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder018 = builder.Field(18).(*array.ListBuilder)
	inst.membershipFieldBuilder019 = builder.Field(19).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder019 = builder.Field(19).(*array.ListBuilder)
	inst.membershipFieldBuilder020 = builder.Field(20).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder020 = builder.Field(20).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder021 = builder.Field(21).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder021 = builder.Field(21).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder022 = builder.Field(22).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder022 = builder.Field(22).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder023 = builder.Field(23).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder023 = builder.Field(23).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder024 = builder.Field(24).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder024 = builder.Field(24).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionStringArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder016.Append(true)
	inst.membershipListBuilder017.Append(true)
	inst.membershipListBuilder018.Append(true)
	inst.membershipListBuilder019.Append(true)
	inst.membershipListBuilder020.Append(true)
	inst.homogenousArrayContainerLength016 = 0
	inst.membershipContainerLength017 = 0
	inst.membershipContainerLength018 = 0
	inst.membershipContainerLength019 = 0
	inst.membershipContainerLength020 = 0
	inst.homogenousArraySupportListBuilder021.Append(true)
	inst.membershipSupportListBuilder022.Append(true)
	inst.membershipSupportListBuilder023.Append(true)
	inst.membershipSupportListBuilder024.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddToContainer(value16 string) *InEntityFactsSectionStringArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder016.Append(value16)
	inst.homogenousArrayContainerLength016++
	return inst
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddToContainerP(value16 string) {
	inst.AddToContainer(value16)
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddMembershipHighCardRef(hr17 uint64) *InEntityFactsSectionStringArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder017.Append(hr17)
	inst.membershipContainerLength017++
	return inst
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddMembershipHighCardRefP(hr17 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder017.Append(hr17)
	inst.membershipContainerLength017++
	return
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddMembershipLowCardRef(lr18 uint64) *InEntityFactsSectionStringArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder018.Append(lr18)
	inst.membershipContainerLength018++
	return inst
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddMembershipLowCardRefP(lr18 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder018.Append(lr18)
	inst.membershipContainerLength018++
	return
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddMembershipMixedLowCardRef(lmr19 uint64, mrhp20 []byte) *InEntityFactsSectionStringArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder019.Append(lmr19)
	inst.membershipFieldBuilder020.Append(mrhp20)
	inst.membershipContainerLength019++
	inst.membershipContainerLength020++
	return inst
}
func (inst *InEntityFactsSectionStringArrayInAttr) AddMembershipMixedLowCardRefP(lmr19 uint64, mrhp20 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder019.Append(lmr19)
	inst.membershipFieldBuilder020.Append(mrhp20)
	inst.membershipContainerLength019++
	inst.membershipContainerLength020++
	return
}
func (inst *InEntityFactsSectionStringArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength017
	inst.membershipContainerLength017 = 0
	inst.membershipSupportFieldBuilder022.Append(uint64(l))
	l = inst.membershipContainerLength018
	inst.membershipContainerLength018 = 0
	inst.membershipSupportFieldBuilder023.Append(uint64(l))
	l = inst.membershipContainerLength019
	inst.membershipContainerLength019 = 0
	inst.membershipSupportFieldBuilder024.Append(uint64(l))
}
func (inst *InEntityFactsSectionStringArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength016
	inst.homogenousArrayContainerLength016 = 0
	inst.homogenousArraySupportFieldBuilder021.Append(uint64(l))
}
func (inst *InEntityFactsSectionStringArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionStringArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionStringArrayInAttr) EndAttribute() *InEntityFactsSectionStringArray {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionStringArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionStringArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionStringArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionSymbol struct {
	errs                  []error
	inAttr                *InEntityFactsSectionSymbolInAttr
	state                 runtime.EntityStateE
	parent                *InEntityFacts
	scalarFieldBuilder025 *array.StringBuilder
	scalarListBuilder025  *array.ListBuilder
}

func NewInEntityFactsSectionSymbol(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionSymbol) {
	inst = &InEntityFactsSectionSymbol{}
	inAttr := NewInEntityFactsSectionSymbolInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder025 = builder.Field(25).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder025 = builder.Field(25).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionSymbol) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionSymbol) BeginAttribute(value25 string) *InEntityFactsSectionSymbolInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder025.Append(value25)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionSymbol) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionSymbol) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionSymbol) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionSymbol) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionSymbol) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionSymbol) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionSymbolInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityFactsSectionSymbol
	scalarFieldBuilder025            *array.StringBuilder
	scalarListBuilder025             *array.ListBuilder
	membershipFieldBuilder026        *array.Uint64Builder
	membershipListBuilder026         *array.ListBuilder
	membershipFieldBuilder027        *array.Uint64Builder
	membershipListBuilder027         *array.ListBuilder
	membershipFieldBuilder028        *array.Uint64Builder
	membershipListBuilder028         *array.ListBuilder
	membershipFieldBuilder029        *array.BinaryBuilder
	membershipListBuilder029         *array.ListBuilder
	membershipSupportFieldBuilder030 *array.Uint64Builder
	membershipSupportListBuilder030  *array.ListBuilder
	membershipSupportFieldBuilder031 *array.Uint64Builder
	membershipSupportListBuilder031  *array.ListBuilder
	membershipSupportFieldBuilder032 *array.Uint64Builder
	membershipSupportListBuilder032  *array.ListBuilder

	membershipContainerLength026 int

	membershipContainerLength027 int

	membershipContainerLength028 int

	membershipContainerLength029 int
}

func NewInEntityFactsSectionSymbolInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionSymbol) (inst *InEntityFactsSectionSymbolInAttr) {
	inst = &InEntityFactsSectionSymbolInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder025 = builder.Field(25).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.scalarListBuilder025 = builder.Field(25).(*array.ListBuilder)
	inst.membershipFieldBuilder026 = builder.Field(26).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder026 = builder.Field(26).(*array.ListBuilder)
	inst.membershipFieldBuilder027 = builder.Field(27).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder027 = builder.Field(27).(*array.ListBuilder)
	inst.membershipFieldBuilder028 = builder.Field(28).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder028 = builder.Field(28).(*array.ListBuilder)
	inst.membershipFieldBuilder029 = builder.Field(29).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder029 = builder.Field(29).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder030 = builder.Field(30).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder030 = builder.Field(30).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder031 = builder.Field(31).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder031 = builder.Field(31).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder032 = builder.Field(32).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder032 = builder.Field(32).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionSymbolInAttr) beginAttribute() {
	inst.membershipListBuilder026.Append(true)
	inst.membershipListBuilder027.Append(true)
	inst.membershipListBuilder028.Append(true)
	inst.membershipListBuilder029.Append(true)
	inst.membershipContainerLength026 = 0
	inst.membershipContainerLength027 = 0
	inst.membershipContainerLength028 = 0
	inst.membershipContainerLength029 = 0
	inst.scalarListBuilder025.Append(true)
	inst.membershipSupportListBuilder030.Append(true)
	inst.membershipSupportListBuilder031.Append(true)
	inst.membershipSupportListBuilder032.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionSymbolInAttr) AddMembershipHighCardRef(hr26 uint64) *InEntityFactsSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder026.Append(hr26)
	inst.membershipContainerLength026++
	return inst
}
func (inst *InEntityFactsSectionSymbolInAttr) AddMembershipHighCardRefP(hr26 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder026.Append(hr26)
	inst.membershipContainerLength026++
	return
}
func (inst *InEntityFactsSectionSymbolInAttr) AddMembershipLowCardRef(lr27 uint64) *InEntityFactsSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder027.Append(lr27)
	inst.membershipContainerLength027++
	return inst
}
func (inst *InEntityFactsSectionSymbolInAttr) AddMembershipLowCardRefP(lr27 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder027.Append(lr27)
	inst.membershipContainerLength027++
	return
}
func (inst *InEntityFactsSectionSymbolInAttr) AddMembershipMixedLowCardRef(lmr28 uint64, mrhp29 []byte) *InEntityFactsSectionSymbolInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder028.Append(lmr28)
	inst.membershipFieldBuilder029.Append(mrhp29)
	inst.membershipContainerLength028++
	inst.membershipContainerLength029++
	return inst
}
func (inst *InEntityFactsSectionSymbolInAttr) AddMembershipMixedLowCardRefP(lmr28 uint64, mrhp29 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder028.Append(lmr28)
	inst.membershipFieldBuilder029.Append(mrhp29)
	inst.membershipContainerLength028++
	inst.membershipContainerLength029++
	return
}
func (inst *InEntityFactsSectionSymbolInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength026
	inst.membershipContainerLength026 = 0
	inst.membershipSupportFieldBuilder030.Append(uint64(l))
	l = inst.membershipContainerLength027
	inst.membershipContainerLength027 = 0
	inst.membershipSupportFieldBuilder031.Append(uint64(l))
	l = inst.membershipContainerLength028
	inst.membershipContainerLength028 = 0
	inst.membershipSupportFieldBuilder032.Append(uint64(l))
}
func (inst *InEntityFactsSectionSymbolInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityFactsSectionSymbolInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionSymbolInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionSymbolInAttr) EndAttribute() *InEntityFactsSectionSymbol {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionSymbolInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionSymbolInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionSymbolInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionSymbolArray struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionSymbolArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder033 *array.StringBuilder
	homogenousArrayListBuilder033  *array.ListBuilder
}

func NewInEntityFactsSectionSymbolArray(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionSymbolArray) {
	inst = &InEntityFactsSectionSymbolArray{}
	inAttr := NewInEntityFactsSectionSymbolArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder033 = builder.Field(33).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder033 = builder.Field(33).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionSymbolArray) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionSymbolArray) BeginAttribute() *InEntityFactsSectionSymbolArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionSymbolArray) BeginAttributeSingle(value33 string) *InEntityFactsSectionSymbolArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value33)
}
func (inst *InEntityFactsSectionSymbolArray) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionSymbolArray) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionSymbolArray) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionSymbolArray) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionSymbolArray) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionSymbolArray) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionSymbolArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionSymbolArray
	homogenousArrayFieldBuilder033        *array.StringBuilder
	homogenousArrayListBuilder033         *array.ListBuilder
	membershipFieldBuilder034             *array.Uint64Builder
	membershipListBuilder034              *array.ListBuilder
	membershipFieldBuilder035             *array.Uint64Builder
	membershipListBuilder035              *array.ListBuilder
	membershipFieldBuilder036             *array.Uint64Builder
	membershipListBuilder036              *array.ListBuilder
	membershipFieldBuilder037             *array.BinaryBuilder
	membershipListBuilder037              *array.ListBuilder
	homogenousArraySupportFieldBuilder038 *array.Uint64Builder
	homogenousArraySupportListBuilder038  *array.ListBuilder
	membershipSupportFieldBuilder039      *array.Uint64Builder
	membershipSupportListBuilder039       *array.ListBuilder
	membershipSupportFieldBuilder040      *array.Uint64Builder
	membershipSupportListBuilder040       *array.ListBuilder
	membershipSupportFieldBuilder041      *array.Uint64Builder
	membershipSupportListBuilder041       *array.ListBuilder

	membershipContainerLength034 int

	membershipContainerLength035 int

	membershipContainerLength036 int

	membershipContainerLength037 int

	homogenousArrayContainerLength033 int
}

func NewInEntityFactsSectionSymbolArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionSymbolArray) (inst *InEntityFactsSectionSymbolArrayInAttr) {
	inst = &InEntityFactsSectionSymbolArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder033 = builder.Field(33).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder033 = builder.Field(33).(*array.ListBuilder)
	inst.membershipFieldBuilder034 = builder.Field(34).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder034 = builder.Field(34).(*array.ListBuilder)
	inst.membershipFieldBuilder035 = builder.Field(35).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder035 = builder.Field(35).(*array.ListBuilder)
	inst.membershipFieldBuilder036 = builder.Field(36).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder036 = builder.Field(36).(*array.ListBuilder)
	inst.membershipFieldBuilder037 = builder.Field(37).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder037 = builder.Field(37).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder038 = builder.Field(38).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder038 = builder.Field(38).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder039 = builder.Field(39).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder039 = builder.Field(39).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder040 = builder.Field(40).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder040 = builder.Field(40).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder041 = builder.Field(41).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder041 = builder.Field(41).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder033.Append(true)
	inst.membershipListBuilder034.Append(true)
	inst.membershipListBuilder035.Append(true)
	inst.membershipListBuilder036.Append(true)
	inst.membershipListBuilder037.Append(true)
	inst.homogenousArrayContainerLength033 = 0
	inst.membershipContainerLength034 = 0
	inst.membershipContainerLength035 = 0
	inst.membershipContainerLength036 = 0
	inst.membershipContainerLength037 = 0
	inst.homogenousArraySupportListBuilder038.Append(true)
	inst.membershipSupportListBuilder039.Append(true)
	inst.membershipSupportListBuilder040.Append(true)
	inst.membershipSupportListBuilder041.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddToContainer(value33 string) *InEntityFactsSectionSymbolArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder033.Append(value33)
	inst.homogenousArrayContainerLength033++
	return inst
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddToContainerP(value33 string) {
	inst.AddToContainer(value33)
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddMembershipHighCardRef(hr34 uint64) *InEntityFactsSectionSymbolArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder034.Append(hr34)
	inst.membershipContainerLength034++
	return inst
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddMembershipHighCardRefP(hr34 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder034.Append(hr34)
	inst.membershipContainerLength034++
	return
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddMembershipLowCardRef(lr35 uint64) *InEntityFactsSectionSymbolArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder035.Append(lr35)
	inst.membershipContainerLength035++
	return inst
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddMembershipLowCardRefP(lr35 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder035.Append(lr35)
	inst.membershipContainerLength035++
	return
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddMembershipMixedLowCardRef(lmr36 uint64, mrhp37 []byte) *InEntityFactsSectionSymbolArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder036.Append(lmr36)
	inst.membershipFieldBuilder037.Append(mrhp37)
	inst.membershipContainerLength036++
	inst.membershipContainerLength037++
	return inst
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) AddMembershipMixedLowCardRefP(lmr36 uint64, mrhp37 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder036.Append(lmr36)
	inst.membershipFieldBuilder037.Append(mrhp37)
	inst.membershipContainerLength036++
	inst.membershipContainerLength037++
	return
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength034
	inst.membershipContainerLength034 = 0
	inst.membershipSupportFieldBuilder039.Append(uint64(l))
	l = inst.membershipContainerLength035
	inst.membershipContainerLength035 = 0
	inst.membershipSupportFieldBuilder040.Append(uint64(l))
	l = inst.membershipContainerLength036
	inst.membershipContainerLength036 = 0
	inst.membershipSupportFieldBuilder041.Append(uint64(l))
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength033
	inst.homogenousArrayContainerLength033 = 0
	inst.homogenousArraySupportFieldBuilder038.Append(uint64(l))
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) EndAttribute() *InEntityFactsSectionSymbolArray {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionSymbolArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionSymbolArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionTextArray struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionTextArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder007 *array.StringBuilder
	homogenousArrayListBuilder007  *array.ListBuilder
}

func NewInEntityFactsSectionTextArray(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionTextArray) {
	inst = &InEntityFactsSectionTextArray{}
	inAttr := NewInEntityFactsSectionTextArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder007 = builder.Field(7).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionTextArray) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionTextArray) BeginAttribute() *InEntityFactsSectionTextArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionTextArray) BeginAttributeSingle(value7 string) *InEntityFactsSectionTextArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value7)
}
func (inst *InEntityFactsSectionTextArray) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionTextArray) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionTextArray) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionTextArray) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionTextArray) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionTextArray) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionTextArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionTextArray
	homogenousArrayFieldBuilder007        *array.StringBuilder
	homogenousArrayListBuilder007         *array.ListBuilder
	membershipFieldBuilder008             *array.Uint64Builder
	membershipListBuilder008              *array.ListBuilder
	membershipFieldBuilder009             *array.Uint64Builder
	membershipListBuilder009              *array.ListBuilder
	membershipFieldBuilder010             *array.Uint64Builder
	membershipListBuilder010              *array.ListBuilder
	membershipFieldBuilder011             *array.BinaryBuilder
	membershipListBuilder011              *array.ListBuilder
	homogenousArraySupportFieldBuilder012 *array.Uint64Builder
	homogenousArraySupportListBuilder012  *array.ListBuilder
	membershipSupportFieldBuilder013      *array.Uint64Builder
	membershipSupportListBuilder013       *array.ListBuilder
	membershipSupportFieldBuilder014      *array.Uint64Builder
	membershipSupportListBuilder014       *array.ListBuilder
	membershipSupportFieldBuilder015      *array.Uint64Builder
	membershipSupportListBuilder015       *array.ListBuilder

	membershipContainerLength008 int

	membershipContainerLength009 int

	membershipContainerLength010 int

	membershipContainerLength011 int

	homogenousArrayContainerLength007 int
}

func NewInEntityFactsSectionTextArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionTextArray) (inst *InEntityFactsSectionTextArrayInAttr) {
	inst = &InEntityFactsSectionTextArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder007 = builder.Field(7).(*array.ListBuilder).ValueBuilder().(*array.StringBuilder)
	inst.homogenousArrayListBuilder007 = builder.Field(7).(*array.ListBuilder)
	inst.membershipFieldBuilder008 = builder.Field(8).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder008 = builder.Field(8).(*array.ListBuilder)
	inst.membershipFieldBuilder009 = builder.Field(9).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder009 = builder.Field(9).(*array.ListBuilder)
	inst.membershipFieldBuilder010 = builder.Field(10).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder010 = builder.Field(10).(*array.ListBuilder)
	inst.membershipFieldBuilder011 = builder.Field(11).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder011 = builder.Field(11).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder012 = builder.Field(12).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder012 = builder.Field(12).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder013 = builder.Field(13).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder013 = builder.Field(13).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder014 = builder.Field(14).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder014 = builder.Field(14).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder015 = builder.Field(15).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder015 = builder.Field(15).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionTextArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder007.Append(true)
	inst.membershipListBuilder008.Append(true)
	inst.membershipListBuilder009.Append(true)
	inst.membershipListBuilder010.Append(true)
	inst.membershipListBuilder011.Append(true)
	inst.homogenousArrayContainerLength007 = 0
	inst.membershipContainerLength008 = 0
	inst.membershipContainerLength009 = 0
	inst.membershipContainerLength010 = 0
	inst.membershipContainerLength011 = 0
	inst.homogenousArraySupportListBuilder012.Append(true)
	inst.membershipSupportListBuilder013.Append(true)
	inst.membershipSupportListBuilder014.Append(true)
	inst.membershipSupportListBuilder015.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddToContainer(value7 string) *InEntityFactsSectionTextArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder007.Append(value7)
	inst.homogenousArrayContainerLength007++
	return inst
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddToContainerP(value7 string) {
	inst.AddToContainer(value7)
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddMembershipHighCardRef(hr8 uint64) *InEntityFactsSectionTextArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder008.Append(hr8)
	inst.membershipContainerLength008++
	return inst
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddMembershipHighCardRefP(hr8 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder008.Append(hr8)
	inst.membershipContainerLength008++
	return
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddMembershipLowCardRef(lr9 uint64) *InEntityFactsSectionTextArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder009.Append(lr9)
	inst.membershipContainerLength009++
	return inst
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddMembershipLowCardRefP(lr9 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder009.Append(lr9)
	inst.membershipContainerLength009++
	return
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddMembershipMixedLowCardRef(lmr10 uint64, mrhp11 []byte) *InEntityFactsSectionTextArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder010.Append(lmr10)
	inst.membershipFieldBuilder011.Append(mrhp11)
	inst.membershipContainerLength010++
	inst.membershipContainerLength011++
	return inst
}
func (inst *InEntityFactsSectionTextArrayInAttr) AddMembershipMixedLowCardRefP(lmr10 uint64, mrhp11 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder010.Append(lmr10)
	inst.membershipFieldBuilder011.Append(mrhp11)
	inst.membershipContainerLength010++
	inst.membershipContainerLength011++
	return
}
func (inst *InEntityFactsSectionTextArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength008
	inst.membershipContainerLength008 = 0
	inst.membershipSupportFieldBuilder013.Append(uint64(l))
	l = inst.membershipContainerLength009
	inst.membershipContainerLength009 = 0
	inst.membershipSupportFieldBuilder014.Append(uint64(l))
	l = inst.membershipContainerLength010
	inst.membershipContainerLength010 = 0
	inst.membershipSupportFieldBuilder015.Append(uint64(l))
}
func (inst *InEntityFactsSectionTextArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength007
	inst.homogenousArrayContainerLength007 = 0
	inst.homogenousArraySupportFieldBuilder012.Append(uint64(l))
}
func (inst *InEntityFactsSectionTextArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionTextArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionTextArrayInAttr) EndAttribute() *InEntityFactsSectionTextArray {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionTextArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionTextArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionTextArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionTimeArray struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionTimeArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder168 *array.TimestampBuilder
	homogenousArrayListBuilder168  *array.ListBuilder
}

func NewInEntityFactsSectionTimeArray(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionTimeArray) {
	inst = &InEntityFactsSectionTimeArray{}
	inAttr := NewInEntityFactsSectionTimeArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder168 = builder.Field(168).(*array.ListBuilder).ValueBuilder().(*array.TimestampBuilder)
	inst.homogenousArrayListBuilder168 = builder.Field(168).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionTimeArray) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionTimeArray) BeginAttribute() *InEntityFactsSectionTimeArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionTimeArray) BeginAttributeSingle(value168 time.Time) *InEntityFactsSectionTimeArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value168)
}
func (inst *InEntityFactsSectionTimeArray) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionTimeArray) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionTimeArray) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionTimeArray) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionTimeArray) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionTimeArray) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionTimeArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionTimeArray
	homogenousArrayFieldBuilder168        *array.TimestampBuilder
	homogenousArrayListBuilder168         *array.ListBuilder
	membershipFieldBuilder169             *array.Uint64Builder
	membershipListBuilder169              *array.ListBuilder
	membershipFieldBuilder170             *array.Uint64Builder
	membershipListBuilder170              *array.ListBuilder
	membershipFieldBuilder171             *array.Uint64Builder
	membershipListBuilder171              *array.ListBuilder
	membershipFieldBuilder172             *array.BinaryBuilder
	membershipListBuilder172              *array.ListBuilder
	homogenousArraySupportFieldBuilder173 *array.Uint64Builder
	homogenousArraySupportListBuilder173  *array.ListBuilder
	membershipSupportFieldBuilder174      *array.Uint64Builder
	membershipSupportListBuilder174       *array.ListBuilder
	membershipSupportFieldBuilder175      *array.Uint64Builder
	membershipSupportListBuilder175       *array.ListBuilder
	membershipSupportFieldBuilder176      *array.Uint64Builder
	membershipSupportListBuilder176       *array.ListBuilder

	membershipContainerLength169 int

	membershipContainerLength170 int

	membershipContainerLength171 int

	membershipContainerLength172 int

	homogenousArrayContainerLength168 int
}

func NewInEntityFactsSectionTimeArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionTimeArray) (inst *InEntityFactsSectionTimeArrayInAttr) {
	inst = &InEntityFactsSectionTimeArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder168 = builder.Field(168).(*array.ListBuilder).ValueBuilder().(*array.TimestampBuilder)
	inst.homogenousArrayListBuilder168 = builder.Field(168).(*array.ListBuilder)
	inst.membershipFieldBuilder169 = builder.Field(169).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder169 = builder.Field(169).(*array.ListBuilder)
	inst.membershipFieldBuilder170 = builder.Field(170).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder170 = builder.Field(170).(*array.ListBuilder)
	inst.membershipFieldBuilder171 = builder.Field(171).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder171 = builder.Field(171).(*array.ListBuilder)
	inst.membershipFieldBuilder172 = builder.Field(172).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder172 = builder.Field(172).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder173 = builder.Field(173).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder173 = builder.Field(173).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder174 = builder.Field(174).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder174 = builder.Field(174).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder175 = builder.Field(175).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder175 = builder.Field(175).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder176 = builder.Field(176).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder176 = builder.Field(176).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionTimeArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder168.Append(true)
	inst.membershipListBuilder169.Append(true)
	inst.membershipListBuilder170.Append(true)
	inst.membershipListBuilder171.Append(true)
	inst.membershipListBuilder172.Append(true)
	inst.homogenousArrayContainerLength168 = 0
	inst.membershipContainerLength169 = 0
	inst.membershipContainerLength170 = 0
	inst.membershipContainerLength171 = 0
	inst.membershipContainerLength172 = 0
	inst.homogenousArraySupportListBuilder173.Append(true)
	inst.membershipSupportListBuilder174.Append(true)
	inst.membershipSupportListBuilder175.Append(true)
	inst.membershipSupportListBuilder176.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddToContainer(value168 time.Time) *InEntityFactsSectionTimeArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder168.Append(arrow.Timestamp(value168.UnixNano()))
	inst.homogenousArrayContainerLength168++
	return inst
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddToContainerP(value168 time.Time) {
	inst.AddToContainer(value168)
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddMembershipHighCardRef(hr169 uint64) *InEntityFactsSectionTimeArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder169.Append(hr169)
	inst.membershipContainerLength169++
	return inst
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddMembershipHighCardRefP(hr169 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder169.Append(hr169)
	inst.membershipContainerLength169++
	return
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddMembershipLowCardRef(lr170 uint64) *InEntityFactsSectionTimeArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder170.Append(lr170)
	inst.membershipContainerLength170++
	return inst
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddMembershipLowCardRefP(lr170 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder170.Append(lr170)
	inst.membershipContainerLength170++
	return
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddMembershipMixedLowCardRef(lmr171 uint64, mrhp172 []byte) *InEntityFactsSectionTimeArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder171.Append(lmr171)
	inst.membershipFieldBuilder172.Append(mrhp172)
	inst.membershipContainerLength171++
	inst.membershipContainerLength172++
	return inst
}
func (inst *InEntityFactsSectionTimeArrayInAttr) AddMembershipMixedLowCardRefP(lmr171 uint64, mrhp172 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder171.Append(lmr171)
	inst.membershipFieldBuilder172.Append(mrhp172)
	inst.membershipContainerLength171++
	inst.membershipContainerLength172++
	return
}
func (inst *InEntityFactsSectionTimeArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength169
	inst.membershipContainerLength169 = 0
	inst.membershipSupportFieldBuilder174.Append(uint64(l))
	l = inst.membershipContainerLength170
	inst.membershipContainerLength170 = 0
	inst.membershipSupportFieldBuilder175.Append(uint64(l))
	l = inst.membershipContainerLength171
	inst.membershipContainerLength171 = 0
	inst.membershipSupportFieldBuilder176.Append(uint64(l))
}
func (inst *InEntityFactsSectionTimeArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength168
	inst.homogenousArrayContainerLength168 = 0
	inst.homogenousArraySupportFieldBuilder173.Append(uint64(l))
}
func (inst *InEntityFactsSectionTimeArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionTimeArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionTimeArrayInAttr) EndAttribute() *InEntityFactsSectionTimeArray {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionTimeArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionTimeArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionTimeArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU16Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionU16ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder060 *array.Uint16Builder
	homogenousArrayListBuilder060  *array.ListBuilder
}

func NewInEntityFactsSectionU16Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionU16Array) {
	inst = &InEntityFactsSectionU16Array{}
	inAttr := NewInEntityFactsSectionU16ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder060 = builder.Field(60).(*array.ListBuilder).ValueBuilder().(*array.Uint16Builder)
	inst.homogenousArrayListBuilder060 = builder.Field(60).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU16Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionU16Array) BeginAttribute() *InEntityFactsSectionU16ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionU16Array) BeginAttributeSingle(value60 uint16) *InEntityFactsSectionU16ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value60)
}
func (inst *InEntityFactsSectionU16Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionU16Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionU16Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionU16Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionU16Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU16Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU16ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionU16Array
	homogenousArrayFieldBuilder060        *array.Uint16Builder
	homogenousArrayListBuilder060         *array.ListBuilder
	membershipFieldBuilder061             *array.Uint64Builder
	membershipListBuilder061              *array.ListBuilder
	membershipFieldBuilder062             *array.Uint64Builder
	membershipListBuilder062              *array.ListBuilder
	membershipFieldBuilder063             *array.Uint64Builder
	membershipListBuilder063              *array.ListBuilder
	membershipFieldBuilder064             *array.BinaryBuilder
	membershipListBuilder064              *array.ListBuilder
	homogenousArraySupportFieldBuilder065 *array.Uint64Builder
	homogenousArraySupportListBuilder065  *array.ListBuilder
	membershipSupportFieldBuilder066      *array.Uint64Builder
	membershipSupportListBuilder066       *array.ListBuilder
	membershipSupportFieldBuilder067      *array.Uint64Builder
	membershipSupportListBuilder067       *array.ListBuilder
	membershipSupportFieldBuilder068      *array.Uint64Builder
	membershipSupportListBuilder068       *array.ListBuilder

	membershipContainerLength061 int

	membershipContainerLength062 int

	membershipContainerLength063 int

	membershipContainerLength064 int

	homogenousArrayContainerLength060 int
}

func NewInEntityFactsSectionU16ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionU16Array) (inst *InEntityFactsSectionU16ArrayInAttr) {
	inst = &InEntityFactsSectionU16ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder060 = builder.Field(60).(*array.ListBuilder).ValueBuilder().(*array.Uint16Builder)
	inst.homogenousArrayListBuilder060 = builder.Field(60).(*array.ListBuilder)
	inst.membershipFieldBuilder061 = builder.Field(61).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder061 = builder.Field(61).(*array.ListBuilder)
	inst.membershipFieldBuilder062 = builder.Field(62).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder062 = builder.Field(62).(*array.ListBuilder)
	inst.membershipFieldBuilder063 = builder.Field(63).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder063 = builder.Field(63).(*array.ListBuilder)
	inst.membershipFieldBuilder064 = builder.Field(64).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder064 = builder.Field(64).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder065 = builder.Field(65).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder065 = builder.Field(65).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder066 = builder.Field(66).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder066 = builder.Field(66).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder067 = builder.Field(67).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder067 = builder.Field(67).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder068 = builder.Field(68).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder068 = builder.Field(68).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU16ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder060.Append(true)
	inst.membershipListBuilder061.Append(true)
	inst.membershipListBuilder062.Append(true)
	inst.membershipListBuilder063.Append(true)
	inst.membershipListBuilder064.Append(true)
	inst.homogenousArrayContainerLength060 = 0
	inst.membershipContainerLength061 = 0
	inst.membershipContainerLength062 = 0
	inst.membershipContainerLength063 = 0
	inst.membershipContainerLength064 = 0
	inst.homogenousArraySupportListBuilder065.Append(true)
	inst.membershipSupportListBuilder066.Append(true)
	inst.membershipSupportListBuilder067.Append(true)
	inst.membershipSupportListBuilder068.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddToContainer(value60 uint16) *InEntityFactsSectionU16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder060.Append(value60)
	inst.homogenousArrayContainerLength060++
	return inst
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddToContainerP(value60 uint16) {
	inst.AddToContainer(value60)
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddMembershipHighCardRef(hr61 uint64) *InEntityFactsSectionU16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder061.Append(hr61)
	inst.membershipContainerLength061++
	return inst
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddMembershipHighCardRefP(hr61 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder061.Append(hr61)
	inst.membershipContainerLength061++
	return
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddMembershipLowCardRef(lr62 uint64) *InEntityFactsSectionU16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder062.Append(lr62)
	inst.membershipContainerLength062++
	return inst
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddMembershipLowCardRefP(lr62 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder062.Append(lr62)
	inst.membershipContainerLength062++
	return
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddMembershipMixedLowCardRef(lmr63 uint64, mrhp64 []byte) *InEntityFactsSectionU16ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder063.Append(lmr63)
	inst.membershipFieldBuilder064.Append(mrhp64)
	inst.membershipContainerLength063++
	inst.membershipContainerLength064++
	return inst
}
func (inst *InEntityFactsSectionU16ArrayInAttr) AddMembershipMixedLowCardRefP(lmr63 uint64, mrhp64 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder063.Append(lmr63)
	inst.membershipFieldBuilder064.Append(mrhp64)
	inst.membershipContainerLength063++
	inst.membershipContainerLength064++
	return
}
func (inst *InEntityFactsSectionU16ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength061
	inst.membershipContainerLength061 = 0
	inst.membershipSupportFieldBuilder066.Append(uint64(l))
	l = inst.membershipContainerLength062
	inst.membershipContainerLength062 = 0
	inst.membershipSupportFieldBuilder067.Append(uint64(l))
	l = inst.membershipContainerLength063
	inst.membershipContainerLength063 = 0
	inst.membershipSupportFieldBuilder068.Append(uint64(l))
}
func (inst *InEntityFactsSectionU16ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength060
	inst.homogenousArrayContainerLength060 = 0
	inst.homogenousArraySupportFieldBuilder065.Append(uint64(l))
}
func (inst *InEntityFactsSectionU16ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionU16ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionU16ArrayInAttr) EndAttribute() *InEntityFactsSectionU16Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionU16ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionU16ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU16ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU32Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionU32ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder069 *array.Uint32Builder
	homogenousArrayListBuilder069  *array.ListBuilder
}

func NewInEntityFactsSectionU32Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionU32Array) {
	inst = &InEntityFactsSectionU32Array{}
	inAttr := NewInEntityFactsSectionU32ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder069 = builder.Field(69).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder069 = builder.Field(69).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU32Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionU32Array) BeginAttribute() *InEntityFactsSectionU32ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionU32Array) BeginAttributeSingle(value69 uint32) *InEntityFactsSectionU32ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value69)
}
func (inst *InEntityFactsSectionU32Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionU32Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionU32Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionU32Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionU32Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU32Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU32ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionU32Array
	homogenousArrayFieldBuilder069        *array.Uint32Builder
	homogenousArrayListBuilder069         *array.ListBuilder
	membershipFieldBuilder070             *array.Uint64Builder
	membershipListBuilder070              *array.ListBuilder
	membershipFieldBuilder071             *array.Uint64Builder
	membershipListBuilder071              *array.ListBuilder
	membershipFieldBuilder072             *array.Uint64Builder
	membershipListBuilder072              *array.ListBuilder
	membershipFieldBuilder073             *array.BinaryBuilder
	membershipListBuilder073              *array.ListBuilder
	homogenousArraySupportFieldBuilder074 *array.Uint64Builder
	homogenousArraySupportListBuilder074  *array.ListBuilder
	membershipSupportFieldBuilder075      *array.Uint64Builder
	membershipSupportListBuilder075       *array.ListBuilder
	membershipSupportFieldBuilder076      *array.Uint64Builder
	membershipSupportListBuilder076       *array.ListBuilder
	membershipSupportFieldBuilder077      *array.Uint64Builder
	membershipSupportListBuilder077       *array.ListBuilder

	membershipContainerLength070 int

	membershipContainerLength071 int

	membershipContainerLength072 int

	membershipContainerLength073 int

	homogenousArrayContainerLength069 int
}

func NewInEntityFactsSectionU32ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionU32Array) (inst *InEntityFactsSectionU32ArrayInAttr) {
	inst = &InEntityFactsSectionU32ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder069 = builder.Field(69).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.homogenousArrayListBuilder069 = builder.Field(69).(*array.ListBuilder)
	inst.membershipFieldBuilder070 = builder.Field(70).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder070 = builder.Field(70).(*array.ListBuilder)
	inst.membershipFieldBuilder071 = builder.Field(71).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder071 = builder.Field(71).(*array.ListBuilder)
	inst.membershipFieldBuilder072 = builder.Field(72).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder072 = builder.Field(72).(*array.ListBuilder)
	inst.membershipFieldBuilder073 = builder.Field(73).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder073 = builder.Field(73).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder074 = builder.Field(74).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder074 = builder.Field(74).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder075 = builder.Field(75).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder075 = builder.Field(75).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder076 = builder.Field(76).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder076 = builder.Field(76).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder077 = builder.Field(77).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder077 = builder.Field(77).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU32ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder069.Append(true)
	inst.membershipListBuilder070.Append(true)
	inst.membershipListBuilder071.Append(true)
	inst.membershipListBuilder072.Append(true)
	inst.membershipListBuilder073.Append(true)
	inst.homogenousArrayContainerLength069 = 0
	inst.membershipContainerLength070 = 0
	inst.membershipContainerLength071 = 0
	inst.membershipContainerLength072 = 0
	inst.membershipContainerLength073 = 0
	inst.homogenousArraySupportListBuilder074.Append(true)
	inst.membershipSupportListBuilder075.Append(true)
	inst.membershipSupportListBuilder076.Append(true)
	inst.membershipSupportListBuilder077.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddToContainer(value69 uint32) *InEntityFactsSectionU32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder069.Append(value69)
	inst.homogenousArrayContainerLength069++
	return inst
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddToContainerP(value69 uint32) {
	inst.AddToContainer(value69)
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddMembershipHighCardRef(hr70 uint64) *InEntityFactsSectionU32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder070.Append(hr70)
	inst.membershipContainerLength070++
	return inst
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddMembershipHighCardRefP(hr70 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder070.Append(hr70)
	inst.membershipContainerLength070++
	return
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddMembershipLowCardRef(lr71 uint64) *InEntityFactsSectionU32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder071.Append(lr71)
	inst.membershipContainerLength071++
	return inst
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddMembershipLowCardRefP(lr71 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder071.Append(lr71)
	inst.membershipContainerLength071++
	return
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddMembershipMixedLowCardRef(lmr72 uint64, mrhp73 []byte) *InEntityFactsSectionU32ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder072.Append(lmr72)
	inst.membershipFieldBuilder073.Append(mrhp73)
	inst.membershipContainerLength072++
	inst.membershipContainerLength073++
	return inst
}
func (inst *InEntityFactsSectionU32ArrayInAttr) AddMembershipMixedLowCardRefP(lmr72 uint64, mrhp73 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder072.Append(lmr72)
	inst.membershipFieldBuilder073.Append(mrhp73)
	inst.membershipContainerLength072++
	inst.membershipContainerLength073++
	return
}
func (inst *InEntityFactsSectionU32ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength070
	inst.membershipContainerLength070 = 0
	inst.membershipSupportFieldBuilder075.Append(uint64(l))
	l = inst.membershipContainerLength071
	inst.membershipContainerLength071 = 0
	inst.membershipSupportFieldBuilder076.Append(uint64(l))
	l = inst.membershipContainerLength072
	inst.membershipContainerLength072 = 0
	inst.membershipSupportFieldBuilder077.Append(uint64(l))
}
func (inst *InEntityFactsSectionU32ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength069
	inst.homogenousArrayContainerLength069 = 0
	inst.homogenousArraySupportFieldBuilder074.Append(uint64(l))
}
func (inst *InEntityFactsSectionU32ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionU32ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionU32ArrayInAttr) EndAttribute() *InEntityFactsSectionU32Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionU32ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionU32ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU32ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU32Range struct {
	errs                  []error
	inAttr                *InEntityFactsSectionU32RangeInAttr
	state                 runtime.EntityStateE
	parent                *InEntityFacts
	scalarFieldBuilder159 *array.Uint32Builder
	scalarListBuilder159  *array.ListBuilder
	scalarFieldBuilder160 *array.Uint32Builder
	scalarListBuilder160  *array.ListBuilder
}

func NewInEntityFactsSectionU32Range(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionU32Range) {
	inst = &InEntityFactsSectionU32Range{}
	inAttr := NewInEntityFactsSectionU32RangeInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.scalarFieldBuilder159 = builder.Field(159).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.scalarListBuilder159 = builder.Field(159).(*array.ListBuilder)
	inst.scalarFieldBuilder160 = builder.Field(160).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.scalarListBuilder160 = builder.Field(160).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU32Range) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionU32Range) BeginAttribute(beginIncl159 uint32, endExcl160 uint32) *InEntityFactsSectionU32RangeInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}
	inst.scalarFieldBuilder159.Append(beginIncl159)
	inst.scalarFieldBuilder160.Append(endExcl160)

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionU32Range) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionU32Range) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionU32Range) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionU32Range) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionU32Range) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU32Range) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU32RangeInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityFactsSectionU32Range
	scalarFieldBuilder159            *array.Uint32Builder
	scalarListBuilder159             *array.ListBuilder
	scalarFieldBuilder160            *array.Uint32Builder
	scalarListBuilder160             *array.ListBuilder
	membershipFieldBuilder161        *array.Uint64Builder
	membershipListBuilder161         *array.ListBuilder
	membershipFieldBuilder162        *array.Uint64Builder
	membershipListBuilder162         *array.ListBuilder
	membershipFieldBuilder163        *array.Uint64Builder
	membershipListBuilder163         *array.ListBuilder
	membershipFieldBuilder164        *array.BinaryBuilder
	membershipListBuilder164         *array.ListBuilder
	membershipSupportFieldBuilder165 *array.Uint64Builder
	membershipSupportListBuilder165  *array.ListBuilder
	membershipSupportFieldBuilder166 *array.Uint64Builder
	membershipSupportListBuilder166  *array.ListBuilder
	membershipSupportFieldBuilder167 *array.Uint64Builder
	membershipSupportListBuilder167  *array.ListBuilder

	membershipContainerLength161 int

	membershipContainerLength162 int

	membershipContainerLength163 int

	membershipContainerLength164 int
}

func NewInEntityFactsSectionU32RangeInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionU32Range) (inst *InEntityFactsSectionU32RangeInAttr) {
	inst = &InEntityFactsSectionU32RangeInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.scalarFieldBuilder159 = builder.Field(159).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.scalarListBuilder159 = builder.Field(159).(*array.ListBuilder)
	inst.scalarFieldBuilder160 = builder.Field(160).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.scalarListBuilder160 = builder.Field(160).(*array.ListBuilder)
	inst.membershipFieldBuilder161 = builder.Field(161).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder161 = builder.Field(161).(*array.ListBuilder)
	inst.membershipFieldBuilder162 = builder.Field(162).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder162 = builder.Field(162).(*array.ListBuilder)
	inst.membershipFieldBuilder163 = builder.Field(163).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder163 = builder.Field(163).(*array.ListBuilder)
	inst.membershipFieldBuilder164 = builder.Field(164).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder164 = builder.Field(164).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder165 = builder.Field(165).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder165 = builder.Field(165).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder166 = builder.Field(166).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder166 = builder.Field(166).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder167 = builder.Field(167).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder167 = builder.Field(167).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU32RangeInAttr) beginAttribute() {
	inst.membershipListBuilder161.Append(true)
	inst.membershipListBuilder162.Append(true)
	inst.membershipListBuilder163.Append(true)
	inst.membershipListBuilder164.Append(true)
	inst.membershipContainerLength161 = 0
	inst.membershipContainerLength162 = 0
	inst.membershipContainerLength163 = 0
	inst.membershipContainerLength164 = 0
	inst.scalarListBuilder159.Append(true)
	inst.scalarListBuilder160.Append(true)
	inst.membershipSupportListBuilder165.Append(true)
	inst.membershipSupportListBuilder166.Append(true)
	inst.membershipSupportListBuilder167.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionU32RangeInAttr) AddMembershipHighCardRef(hr161 uint64) *InEntityFactsSectionU32RangeInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder161.Append(hr161)
	inst.membershipContainerLength161++
	return inst
}
func (inst *InEntityFactsSectionU32RangeInAttr) AddMembershipHighCardRefP(hr161 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder161.Append(hr161)
	inst.membershipContainerLength161++
	return
}
func (inst *InEntityFactsSectionU32RangeInAttr) AddMembershipLowCardRef(lr162 uint64) *InEntityFactsSectionU32RangeInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder162.Append(lr162)
	inst.membershipContainerLength162++
	return inst
}
func (inst *InEntityFactsSectionU32RangeInAttr) AddMembershipLowCardRefP(lr162 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder162.Append(lr162)
	inst.membershipContainerLength162++
	return
}
func (inst *InEntityFactsSectionU32RangeInAttr) AddMembershipMixedLowCardRef(lmr163 uint64, mrhp164 []byte) *InEntityFactsSectionU32RangeInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder163.Append(lmr163)
	inst.membershipFieldBuilder164.Append(mrhp164)
	inst.membershipContainerLength163++
	inst.membershipContainerLength164++
	return inst
}
func (inst *InEntityFactsSectionU32RangeInAttr) AddMembershipMixedLowCardRefP(lmr163 uint64, mrhp164 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder163.Append(lmr163)
	inst.membershipFieldBuilder164.Append(mrhp164)
	inst.membershipContainerLength163++
	inst.membershipContainerLength164++
	return
}
func (inst *InEntityFactsSectionU32RangeInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength161
	inst.membershipContainerLength161 = 0
	inst.membershipSupportFieldBuilder165.Append(uint64(l))
	l = inst.membershipContainerLength162
	inst.membershipContainerLength162 = 0
	inst.membershipSupportFieldBuilder166.Append(uint64(l))
	l = inst.membershipContainerLength163
	inst.membershipContainerLength163 = 0
	inst.membershipSupportFieldBuilder167.Append(uint64(l))
}
func (inst *InEntityFactsSectionU32RangeInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
}
func (inst *InEntityFactsSectionU32RangeInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionU32RangeInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionU32RangeInAttr) EndAttribute() *InEntityFactsSectionU32Range {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionU32RangeInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionU32RangeInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU32RangeInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU32Set struct {
	errs               []error
	inAttr             *InEntityFactsSectionU32SetInAttr
	state              runtime.EntityStateE
	parent             *InEntityFacts
	setFieldBuilder078 *array.Uint32Builder
	setListBuilder078  *array.ListBuilder
}

func NewInEntityFactsSectionU32Set(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionU32Set) {
	inst = &InEntityFactsSectionU32Set{}
	inAttr := NewInEntityFactsSectionU32SetInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.setFieldBuilder078 = builder.Field(78).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.setListBuilder078 = builder.Field(78).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU32Set) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionU32Set) BeginAttribute() *InEntityFactsSectionU32SetInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionU32Set) BeginAttributeSingle(value78 uint32) *InEntityFactsSectionU32SetInAttr {
	return inst.BeginAttribute().AddToContainer(value78)
}
func (inst *InEntityFactsSectionU32Set) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionU32Set) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionU32Set) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionU32Set) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionU32Set) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU32Set) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU32SetInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityFactsSectionU32Set
	setFieldBuilder078               *array.Uint32Builder
	setListBuilder078                *array.ListBuilder
	membershipFieldBuilder079        *array.Uint64Builder
	membershipListBuilder079         *array.ListBuilder
	membershipFieldBuilder080        *array.Uint64Builder
	membershipListBuilder080         *array.ListBuilder
	membershipFieldBuilder081        *array.Uint64Builder
	membershipListBuilder081         *array.ListBuilder
	membershipFieldBuilder082        *array.BinaryBuilder
	membershipListBuilder082         *array.ListBuilder
	setSupportFieldBuilder083        *array.Uint64Builder
	setSupportListBuilder083         *array.ListBuilder
	membershipSupportFieldBuilder084 *array.Uint64Builder
	membershipSupportListBuilder084  *array.ListBuilder
	membershipSupportFieldBuilder085 *array.Uint64Builder
	membershipSupportListBuilder085  *array.ListBuilder
	membershipSupportFieldBuilder086 *array.Uint64Builder
	membershipSupportListBuilder086  *array.ListBuilder

	membershipContainerLength079 int

	membershipContainerLength080 int

	membershipContainerLength081 int

	membershipContainerLength082 int

	setContainerLength078 int
}

func NewInEntityFactsSectionU32SetInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionU32Set) (inst *InEntityFactsSectionU32SetInAttr) {
	inst = &InEntityFactsSectionU32SetInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.setFieldBuilder078 = builder.Field(78).(*array.ListBuilder).ValueBuilder().(*array.Uint32Builder)
	inst.setListBuilder078 = builder.Field(78).(*array.ListBuilder)
	inst.membershipFieldBuilder079 = builder.Field(79).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder079 = builder.Field(79).(*array.ListBuilder)
	inst.membershipFieldBuilder080 = builder.Field(80).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder080 = builder.Field(80).(*array.ListBuilder)
	inst.membershipFieldBuilder081 = builder.Field(81).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder081 = builder.Field(81).(*array.ListBuilder)
	inst.membershipFieldBuilder082 = builder.Field(82).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder082 = builder.Field(82).(*array.ListBuilder)
	inst.setSupportFieldBuilder083 = builder.Field(83).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.setSupportListBuilder083 = builder.Field(83).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder084 = builder.Field(84).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder084 = builder.Field(84).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder085 = builder.Field(85).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder085 = builder.Field(85).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder086 = builder.Field(86).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder086 = builder.Field(86).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU32SetInAttr) beginAttribute() {
	inst.setListBuilder078.Append(true)
	inst.membershipListBuilder079.Append(true)
	inst.membershipListBuilder080.Append(true)
	inst.membershipListBuilder081.Append(true)
	inst.membershipListBuilder082.Append(true)
	inst.setContainerLength078 = 0
	inst.membershipContainerLength079 = 0
	inst.membershipContainerLength080 = 0
	inst.membershipContainerLength081 = 0
	inst.membershipContainerLength082 = 0
	inst.setSupportListBuilder083.Append(true)
	inst.membershipSupportListBuilder084.Append(true)
	inst.membershipSupportListBuilder085.Append(true)
	inst.membershipSupportListBuilder086.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionU32SetInAttr) AddToContainer(value78 uint32) *InEntityFactsSectionU32SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.setFieldBuilder078.Append(value78)
	inst.setContainerLength078++
	return inst
}
func (inst *InEntityFactsSectionU32SetInAttr) AddToContainerP(value78 uint32) {
	inst.AddToContainer(value78)
}
func (inst *InEntityFactsSectionU32SetInAttr) AddMembershipHighCardRef(hr79 uint64) *InEntityFactsSectionU32SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder079.Append(hr79)
	inst.membershipContainerLength079++
	return inst
}
func (inst *InEntityFactsSectionU32SetInAttr) AddMembershipHighCardRefP(hr79 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder079.Append(hr79)
	inst.membershipContainerLength079++
	return
}
func (inst *InEntityFactsSectionU32SetInAttr) AddMembershipLowCardRef(lr80 uint64) *InEntityFactsSectionU32SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder080.Append(lr80)
	inst.membershipContainerLength080++
	return inst
}
func (inst *InEntityFactsSectionU32SetInAttr) AddMembershipLowCardRefP(lr80 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder080.Append(lr80)
	inst.membershipContainerLength080++
	return
}
func (inst *InEntityFactsSectionU32SetInAttr) AddMembershipMixedLowCardRef(lmr81 uint64, mrhp82 []byte) *InEntityFactsSectionU32SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder081.Append(lmr81)
	inst.membershipFieldBuilder082.Append(mrhp82)
	inst.membershipContainerLength081++
	inst.membershipContainerLength082++
	return inst
}
func (inst *InEntityFactsSectionU32SetInAttr) AddMembershipMixedLowCardRefP(lmr81 uint64, mrhp82 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder081.Append(lmr81)
	inst.membershipFieldBuilder082.Append(mrhp82)
	inst.membershipContainerLength081++
	inst.membershipContainerLength082++
	return
}
func (inst *InEntityFactsSectionU32SetInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength079
	inst.membershipContainerLength079 = 0
	inst.membershipSupportFieldBuilder084.Append(uint64(l))
	l = inst.membershipContainerLength080
	inst.membershipContainerLength080 = 0
	inst.membershipSupportFieldBuilder085.Append(uint64(l))
	l = inst.membershipContainerLength081
	inst.membershipContainerLength081 = 0
	inst.membershipSupportFieldBuilder086.Append(uint64(l))
}
func (inst *InEntityFactsSectionU32SetInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.setContainerLength078
	inst.setContainerLength078 = 0
	inst.setSupportFieldBuilder083.Append(uint64(l))
}
func (inst *InEntityFactsSectionU32SetInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionU32SetInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionU32SetInAttr) EndAttribute() *InEntityFactsSectionU32Set {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionU32SetInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionU32SetInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU32SetInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU64Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionU64ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder087 *array.Uint64Builder
	homogenousArrayListBuilder087  *array.ListBuilder
}

func NewInEntityFactsSectionU64Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionU64Array) {
	inst = &InEntityFactsSectionU64Array{}
	inAttr := NewInEntityFactsSectionU64ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder087 = builder.Field(87).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArrayListBuilder087 = builder.Field(87).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU64Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionU64Array) BeginAttribute() *InEntityFactsSectionU64ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionU64Array) BeginAttributeSingle(value87 uint64) *InEntityFactsSectionU64ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value87)
}
func (inst *InEntityFactsSectionU64Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionU64Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionU64Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionU64Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionU64Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU64Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU64ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionU64Array
	homogenousArrayFieldBuilder087        *array.Uint64Builder
	homogenousArrayListBuilder087         *array.ListBuilder
	membershipFieldBuilder088             *array.Uint64Builder
	membershipListBuilder088              *array.ListBuilder
	membershipFieldBuilder089             *array.Uint64Builder
	membershipListBuilder089              *array.ListBuilder
	membershipFieldBuilder090             *array.Uint64Builder
	membershipListBuilder090              *array.ListBuilder
	membershipFieldBuilder091             *array.BinaryBuilder
	membershipListBuilder091              *array.ListBuilder
	homogenousArraySupportFieldBuilder092 *array.Uint64Builder
	homogenousArraySupportListBuilder092  *array.ListBuilder
	membershipSupportFieldBuilder093      *array.Uint64Builder
	membershipSupportListBuilder093       *array.ListBuilder
	membershipSupportFieldBuilder094      *array.Uint64Builder
	membershipSupportListBuilder094       *array.ListBuilder
	membershipSupportFieldBuilder095      *array.Uint64Builder
	membershipSupportListBuilder095       *array.ListBuilder

	membershipContainerLength088 int

	membershipContainerLength089 int

	membershipContainerLength090 int

	membershipContainerLength091 int

	homogenousArrayContainerLength087 int
}

func NewInEntityFactsSectionU64ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionU64Array) (inst *InEntityFactsSectionU64ArrayInAttr) {
	inst = &InEntityFactsSectionU64ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder087 = builder.Field(87).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArrayListBuilder087 = builder.Field(87).(*array.ListBuilder)
	inst.membershipFieldBuilder088 = builder.Field(88).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder088 = builder.Field(88).(*array.ListBuilder)
	inst.membershipFieldBuilder089 = builder.Field(89).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder089 = builder.Field(89).(*array.ListBuilder)
	inst.membershipFieldBuilder090 = builder.Field(90).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder090 = builder.Field(90).(*array.ListBuilder)
	inst.membershipFieldBuilder091 = builder.Field(91).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder091 = builder.Field(91).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder092 = builder.Field(92).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder092 = builder.Field(92).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder093 = builder.Field(93).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder093 = builder.Field(93).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder094 = builder.Field(94).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder094 = builder.Field(94).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder095 = builder.Field(95).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder095 = builder.Field(95).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU64ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder087.Append(true)
	inst.membershipListBuilder088.Append(true)
	inst.membershipListBuilder089.Append(true)
	inst.membershipListBuilder090.Append(true)
	inst.membershipListBuilder091.Append(true)
	inst.homogenousArrayContainerLength087 = 0
	inst.membershipContainerLength088 = 0
	inst.membershipContainerLength089 = 0
	inst.membershipContainerLength090 = 0
	inst.membershipContainerLength091 = 0
	inst.homogenousArraySupportListBuilder092.Append(true)
	inst.membershipSupportListBuilder093.Append(true)
	inst.membershipSupportListBuilder094.Append(true)
	inst.membershipSupportListBuilder095.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddToContainer(value87 uint64) *InEntityFactsSectionU64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder087.Append(value87)
	inst.homogenousArrayContainerLength087++
	return inst
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddToContainerP(value87 uint64) {
	inst.AddToContainer(value87)
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddMembershipHighCardRef(hr88 uint64) *InEntityFactsSectionU64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder088.Append(hr88)
	inst.membershipContainerLength088++
	return inst
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddMembershipHighCardRefP(hr88 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder088.Append(hr88)
	inst.membershipContainerLength088++
	return
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddMembershipLowCardRef(lr89 uint64) *InEntityFactsSectionU64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder089.Append(lr89)
	inst.membershipContainerLength089++
	return inst
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddMembershipLowCardRefP(lr89 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder089.Append(lr89)
	inst.membershipContainerLength089++
	return
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddMembershipMixedLowCardRef(lmr90 uint64, mrhp91 []byte) *InEntityFactsSectionU64ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder090.Append(lmr90)
	inst.membershipFieldBuilder091.Append(mrhp91)
	inst.membershipContainerLength090++
	inst.membershipContainerLength091++
	return inst
}
func (inst *InEntityFactsSectionU64ArrayInAttr) AddMembershipMixedLowCardRefP(lmr90 uint64, mrhp91 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder090.Append(lmr90)
	inst.membershipFieldBuilder091.Append(mrhp91)
	inst.membershipContainerLength090++
	inst.membershipContainerLength091++
	return
}
func (inst *InEntityFactsSectionU64ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength088
	inst.membershipContainerLength088 = 0
	inst.membershipSupportFieldBuilder093.Append(uint64(l))
	l = inst.membershipContainerLength089
	inst.membershipContainerLength089 = 0
	inst.membershipSupportFieldBuilder094.Append(uint64(l))
	l = inst.membershipContainerLength090
	inst.membershipContainerLength090 = 0
	inst.membershipSupportFieldBuilder095.Append(uint64(l))
}
func (inst *InEntityFactsSectionU64ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength087
	inst.homogenousArrayContainerLength087 = 0
	inst.homogenousArraySupportFieldBuilder092.Append(uint64(l))
}
func (inst *InEntityFactsSectionU64ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionU64ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionU64ArrayInAttr) EndAttribute() *InEntityFactsSectionU64Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionU64ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionU64ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU64ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU64Set struct {
	errs               []error
	inAttr             *InEntityFactsSectionU64SetInAttr
	state              runtime.EntityStateE
	parent             *InEntityFacts
	setFieldBuilder096 *array.Uint64Builder
	setListBuilder096  *array.ListBuilder
}

func NewInEntityFactsSectionU64Set(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionU64Set) {
	inst = &InEntityFactsSectionU64Set{}
	inAttr := NewInEntityFactsSectionU64SetInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.setFieldBuilder096 = builder.Field(96).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.setListBuilder096 = builder.Field(96).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU64Set) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionU64Set) BeginAttribute() *InEntityFactsSectionU64SetInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionU64Set) BeginAttributeSingle(value96 uint64) *InEntityFactsSectionU64SetInAttr {
	return inst.BeginAttribute().AddToContainer(value96)
}
func (inst *InEntityFactsSectionU64Set) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionU64Set) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionU64Set) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionU64Set) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionU64Set) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU64Set) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU64SetInAttr struct {
	errs                             []error
	state                            runtime.EntityStateE
	parent                           *InEntityFactsSectionU64Set
	setFieldBuilder096               *array.Uint64Builder
	setListBuilder096                *array.ListBuilder
	membershipFieldBuilder097        *array.Uint64Builder
	membershipListBuilder097         *array.ListBuilder
	membershipFieldBuilder098        *array.Uint64Builder
	membershipListBuilder098         *array.ListBuilder
	membershipFieldBuilder099        *array.Uint64Builder
	membershipListBuilder099         *array.ListBuilder
	membershipFieldBuilder100        *array.BinaryBuilder
	membershipListBuilder100         *array.ListBuilder
	setSupportFieldBuilder101        *array.Uint64Builder
	setSupportListBuilder101         *array.ListBuilder
	membershipSupportFieldBuilder102 *array.Uint64Builder
	membershipSupportListBuilder102  *array.ListBuilder
	membershipSupportFieldBuilder103 *array.Uint64Builder
	membershipSupportListBuilder103  *array.ListBuilder
	membershipSupportFieldBuilder104 *array.Uint64Builder
	membershipSupportListBuilder104  *array.ListBuilder

	membershipContainerLength097 int

	membershipContainerLength098 int

	membershipContainerLength099 int

	membershipContainerLength100 int

	setContainerLength096 int
}

func NewInEntityFactsSectionU64SetInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionU64Set) (inst *InEntityFactsSectionU64SetInAttr) {
	inst = &InEntityFactsSectionU64SetInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.setFieldBuilder096 = builder.Field(96).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.setListBuilder096 = builder.Field(96).(*array.ListBuilder)
	inst.membershipFieldBuilder097 = builder.Field(97).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder097 = builder.Field(97).(*array.ListBuilder)
	inst.membershipFieldBuilder098 = builder.Field(98).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder098 = builder.Field(98).(*array.ListBuilder)
	inst.membershipFieldBuilder099 = builder.Field(99).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder099 = builder.Field(99).(*array.ListBuilder)
	inst.membershipFieldBuilder100 = builder.Field(100).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder100 = builder.Field(100).(*array.ListBuilder)
	inst.setSupportFieldBuilder101 = builder.Field(101).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.setSupportListBuilder101 = builder.Field(101).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder102 = builder.Field(102).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder102 = builder.Field(102).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder103 = builder.Field(103).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder103 = builder.Field(103).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder104 = builder.Field(104).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder104 = builder.Field(104).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU64SetInAttr) beginAttribute() {
	inst.setListBuilder096.Append(true)
	inst.membershipListBuilder097.Append(true)
	inst.membershipListBuilder098.Append(true)
	inst.membershipListBuilder099.Append(true)
	inst.membershipListBuilder100.Append(true)
	inst.setContainerLength096 = 0
	inst.membershipContainerLength097 = 0
	inst.membershipContainerLength098 = 0
	inst.membershipContainerLength099 = 0
	inst.membershipContainerLength100 = 0
	inst.setSupportListBuilder101.Append(true)
	inst.membershipSupportListBuilder102.Append(true)
	inst.membershipSupportListBuilder103.Append(true)
	inst.membershipSupportListBuilder104.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionU64SetInAttr) AddToContainer(value96 uint64) *InEntityFactsSectionU64SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.setFieldBuilder096.Append(value96)
	inst.setContainerLength096++
	return inst
}
func (inst *InEntityFactsSectionU64SetInAttr) AddToContainerP(value96 uint64) {
	inst.AddToContainer(value96)
}
func (inst *InEntityFactsSectionU64SetInAttr) AddMembershipHighCardRef(hr97 uint64) *InEntityFactsSectionU64SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder097.Append(hr97)
	inst.membershipContainerLength097++
	return inst
}
func (inst *InEntityFactsSectionU64SetInAttr) AddMembershipHighCardRefP(hr97 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder097.Append(hr97)
	inst.membershipContainerLength097++
	return
}
func (inst *InEntityFactsSectionU64SetInAttr) AddMembershipLowCardRef(lr98 uint64) *InEntityFactsSectionU64SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder098.Append(lr98)
	inst.membershipContainerLength098++
	return inst
}
func (inst *InEntityFactsSectionU64SetInAttr) AddMembershipLowCardRefP(lr98 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder098.Append(lr98)
	inst.membershipContainerLength098++
	return
}
func (inst *InEntityFactsSectionU64SetInAttr) AddMembershipMixedLowCardRef(lmr99 uint64, mrhp100 []byte) *InEntityFactsSectionU64SetInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder099.Append(lmr99)
	inst.membershipFieldBuilder100.Append(mrhp100)
	inst.membershipContainerLength099++
	inst.membershipContainerLength100++
	return inst
}
func (inst *InEntityFactsSectionU64SetInAttr) AddMembershipMixedLowCardRefP(lmr99 uint64, mrhp100 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder099.Append(lmr99)
	inst.membershipFieldBuilder100.Append(mrhp100)
	inst.membershipContainerLength099++
	inst.membershipContainerLength100++
	return
}
func (inst *InEntityFactsSectionU64SetInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength097
	inst.membershipContainerLength097 = 0
	inst.membershipSupportFieldBuilder102.Append(uint64(l))
	l = inst.membershipContainerLength098
	inst.membershipContainerLength098 = 0
	inst.membershipSupportFieldBuilder103.Append(uint64(l))
	l = inst.membershipContainerLength099
	inst.membershipContainerLength099 = 0
	inst.membershipSupportFieldBuilder104.Append(uint64(l))
}
func (inst *InEntityFactsSectionU64SetInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.setContainerLength096
	inst.setContainerLength096 = 0
	inst.setSupportFieldBuilder101.Append(uint64(l))
}
func (inst *InEntityFactsSectionU64SetInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionU64SetInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionU64SetInAttr) EndAttribute() *InEntityFactsSectionU64Set {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionU64SetInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionU64SetInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU64SetInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU8Array struct {
	errs                           []error
	inAttr                         *InEntityFactsSectionU8ArrayInAttr
	state                          runtime.EntityStateE
	parent                         *InEntityFacts
	homogenousArrayFieldBuilder051 *array.Uint8Builder
	homogenousArrayListBuilder051  *array.ListBuilder
}

func NewInEntityFactsSectionU8Array(builder *array.RecordBuilder, parent *InEntityFacts) (inst *InEntityFactsSectionU8Array) {
	inst = &InEntityFactsSectionU8Array{}
	inAttr := NewInEntityFactsSectionU8ArrayInAttr(builder, inst)
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
	inst.homogenousArrayFieldBuilder051 = builder.Field(51).(*array.ListBuilder).ValueBuilder().(*array.Uint8Builder)
	inst.homogenousArrayListBuilder051 = builder.Field(51).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU8Array) endAttribute() {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
}
func (inst *InEntityFactsSectionU8Array) BeginAttribute() *InEntityFactsSectionU8ArrayInAttr {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInAttribute
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.inAttr
	}

	inst.inAttr.state = inst.state
	return inst.inAttr
}
func (inst *InEntityFactsSectionU8Array) BeginAttributeSingle(value51 uint8) *InEntityFactsSectionU8ArrayInAttr {
	return inst.BeginAttribute().AddToContainer(value51)
}
func (inst *InEntityFactsSectionU8Array) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs, inst.inAttr.errs))
	return
}
func (inst *InEntityFactsSectionU8Array) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInSection:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	return inst.parent
}

func (inst *InEntityFactsSectionU8Array) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
}

func (inst *InEntityFactsSectionU8Array) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}

func (inst *InEntityFactsSectionU8Array) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU8Array) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}

type InEntityFactsSectionU8ArrayInAttr struct {
	errs                                  []error
	state                                 runtime.EntityStateE
	parent                                *InEntityFactsSectionU8Array
	homogenousArrayFieldBuilder051        *array.Uint8Builder
	homogenousArrayListBuilder051         *array.ListBuilder
	membershipFieldBuilder052             *array.Uint64Builder
	membershipListBuilder052              *array.ListBuilder
	membershipFieldBuilder053             *array.Uint64Builder
	membershipListBuilder053              *array.ListBuilder
	membershipFieldBuilder054             *array.Uint64Builder
	membershipListBuilder054              *array.ListBuilder
	membershipFieldBuilder055             *array.BinaryBuilder
	membershipListBuilder055              *array.ListBuilder
	homogenousArraySupportFieldBuilder056 *array.Uint64Builder
	homogenousArraySupportListBuilder056  *array.ListBuilder
	membershipSupportFieldBuilder057      *array.Uint64Builder
	membershipSupportListBuilder057       *array.ListBuilder
	membershipSupportFieldBuilder058      *array.Uint64Builder
	membershipSupportListBuilder058       *array.ListBuilder
	membershipSupportFieldBuilder059      *array.Uint64Builder
	membershipSupportListBuilder059       *array.ListBuilder

	membershipContainerLength052 int

	membershipContainerLength053 int

	membershipContainerLength054 int

	membershipContainerLength055 int

	homogenousArrayContainerLength051 int
}

func NewInEntityFactsSectionU8ArrayInAttr(builder *array.RecordBuilder, parent *InEntityFactsSectionU8Array) (inst *InEntityFactsSectionU8ArrayInAttr) {
	inst = &InEntityFactsSectionU8ArrayInAttr{}
	inst.errs = make([]error, 0, 8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
	inst.homogenousArrayFieldBuilder051 = builder.Field(51).(*array.ListBuilder).ValueBuilder().(*array.Uint8Builder)
	inst.homogenousArrayListBuilder051 = builder.Field(51).(*array.ListBuilder)
	inst.membershipFieldBuilder052 = builder.Field(52).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder052 = builder.Field(52).(*array.ListBuilder)
	inst.membershipFieldBuilder053 = builder.Field(53).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder053 = builder.Field(53).(*array.ListBuilder)
	inst.membershipFieldBuilder054 = builder.Field(54).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipListBuilder054 = builder.Field(54).(*array.ListBuilder)
	inst.membershipFieldBuilder055 = builder.Field(55).(*array.ListBuilder).ValueBuilder().(*array.BinaryBuilder)
	inst.membershipListBuilder055 = builder.Field(55).(*array.ListBuilder)
	inst.homogenousArraySupportFieldBuilder056 = builder.Field(56).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.homogenousArraySupportListBuilder056 = builder.Field(56).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder057 = builder.Field(57).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder057 = builder.Field(57).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder058 = builder.Field(58).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder058 = builder.Field(58).(*array.ListBuilder)
	inst.membershipSupportFieldBuilder059 = builder.Field(59).(*array.ListBuilder).ValueBuilder().(*array.Uint64Builder)
	inst.membershipSupportListBuilder059 = builder.Field(59).(*array.ListBuilder)

	return inst
}
func (inst *InEntityFactsSectionU8ArrayInAttr) beginAttribute() {
	inst.homogenousArrayListBuilder051.Append(true)
	inst.membershipListBuilder052.Append(true)
	inst.membershipListBuilder053.Append(true)
	inst.membershipListBuilder054.Append(true)
	inst.membershipListBuilder055.Append(true)
	inst.homogenousArrayContainerLength051 = 0
	inst.membershipContainerLength052 = 0
	inst.membershipContainerLength053 = 0
	inst.membershipContainerLength054 = 0
	inst.membershipContainerLength055 = 0
	inst.homogenousArraySupportListBuilder056.Append(true)
	inst.membershipSupportListBuilder057.Append(true)
	inst.membershipSupportListBuilder058.Append(true)
	inst.membershipSupportListBuilder059.Append(true)
	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddToContainer(value51 uint8) *InEntityFactsSectionU8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.homogenousArrayFieldBuilder051.Append(value51)
	inst.homogenousArrayContainerLength051++
	return inst
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddToContainerP(value51 uint8) {
	inst.AddToContainer(value51)
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddMembershipHighCardRef(hr52 uint64) *InEntityFactsSectionU8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder052.Append(hr52)
	inst.membershipContainerLength052++
	return inst
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddMembershipHighCardRefP(hr52 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder052.Append(hr52)
	inst.membershipContainerLength052++
	return
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddMembershipLowCardRef(lr53 uint64) *InEntityFactsSectionU8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder053.Append(lr53)
	inst.membershipContainerLength053++
	return inst
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddMembershipLowCardRefP(lr53 uint64) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder053.Append(lr53)
	inst.membershipContainerLength053++
	return
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddMembershipMixedLowCardRef(lmr54 uint64, mrhp55 []byte) *InEntityFactsSectionU8ArrayInAttr {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst
	}
	inst.membershipFieldBuilder054.Append(lmr54)
	inst.membershipFieldBuilder055.Append(mrhp55)
	inst.membershipContainerLength054++
	inst.membershipContainerLength055++
	return inst
}
func (inst *InEntityFactsSectionU8ArrayInAttr) AddMembershipMixedLowCardRefP(lmr54 uint64, mrhp55 []byte) {
	if inst.state != runtime.EntityStateInAttribute {
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return
	}
	inst.membershipFieldBuilder054.Append(lmr54)
	inst.membershipFieldBuilder055.Append(mrhp55)
	inst.membershipContainerLength054++
	inst.membershipContainerLength055++
	return
}
func (inst *InEntityFactsSectionU8ArrayInAttr) handleMembershipSupportColumns() {
	var l int
	var _ = l
	l = inst.membershipContainerLength052
	inst.membershipContainerLength052 = 0
	inst.membershipSupportFieldBuilder057.Append(uint64(l))
	l = inst.membershipContainerLength053
	inst.membershipContainerLength053 = 0
	inst.membershipSupportFieldBuilder058.Append(uint64(l))
	l = inst.membershipContainerLength054
	inst.membershipContainerLength054 = 0
	inst.membershipSupportFieldBuilder059.Append(uint64(l))
}
func (inst *InEntityFactsSectionU8ArrayInAttr) handleNonScalarSupportColumns() {
	var l int
	var _ = l
	l = inst.homogenousArrayContainerLength051
	inst.homogenousArrayContainerLength051 = 0
	inst.homogenousArraySupportFieldBuilder056.Append(uint64(l))
}
func (inst *InEntityFactsSectionU8ArrayInAttr) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
func (inst *InEntityFactsSectionU8ArrayInAttr) EndSection() *InEntityFacts {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInitial
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent.parent
	}

	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
func (inst *InEntityFactsSectionU8ArrayInAttr) EndAttribute() *InEntityFactsSectionU8Array {
	switch inst.state {
	case runtime.EntityStateInAttribute:
		inst.state = runtime.EntityStateInSection
		break
	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return inst.parent
	}

	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
func (inst *InEntityFactsSectionU8ArrayInAttr) EndAttributeP() {
	inst.EndAttribute()
}

func (inst *InEntityFactsSectionU8ArrayInAttr) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs, err)
}
func (inst *InEntityFactsSectionU8ArrayInAttr) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}
