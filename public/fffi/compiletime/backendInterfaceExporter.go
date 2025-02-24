package compiletime

import (
	"bufio"
	"bytes"
	"go/ast"
	"hash"
	"io"
	"math"

	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"lukechampine.com/blake3"
)

type BackendInterfaceExporter struct {
	bufSignature   *bytes.Buffer
	bufDescription *bytes.Buffer
	enc            cbor.FullEncoder
	hasher         hash.Hash
	cppNamer       *Namer
	compatRecord   *CompatibilityRecord
}

type CompatibilityRecord struct {
	FeatureNoThrowTrue  bool               `cbor:"noThrowTrue"`
	FeatureNoThrowFalse bool               `cbor:"noThrowFalse"`
	MinId               runtime.FuncProcId `cbor:"minId"`
	MaxId               runtime.FuncProcId `cbor:"maxId"`
	Hash                []byte             `cbor:"hash"`
}

func NewCompatibilityRecord() *CompatibilityRecord {
	inst := &CompatibilityRecord{
		FeatureNoThrowTrue:  false,
		FeatureNoThrowFalse: false,
		Hash:                make([]byte, 0, hashSize),
	}
	inst.Reset()
	return inst
}
func (inst *CompatibilityRecord) Add(id runtime.FuncProcId, noThrow bool) {
	inst.MinId = min(id, inst.MinId)
	inst.MaxId = max(id, inst.MaxId)
	if noThrow {
		inst.FeatureNoThrowTrue = true
	} else {
		inst.FeatureNoThrowFalse = true
	}
}

func (inst *CompatibilityRecord) Reset() {
	inst.Hash = inst.Hash[:0]
	inst.MinId = runtime.FuncProcId(math.MaxUint32)
	inst.MaxId = 0
	inst.FeatureNoThrowFalse = false
	inst.FeatureNoThrowTrue = false
}
func (inst *CompatibilityRecord) Encode(enc cbor.FullEncoder) (err error) {
	_, err = enc.EncodeMapDefinite(4)
	_, err = enc.EncodeString("features")
	if err != nil {
		return
	}
	_, err = enc.EncodeMapDefinite(2)
	if err != nil {
		return
	}
	_, err = enc.EncodeString("noThrowTrue")
	if err != nil {
		return
	}
	_, err = enc.EncodeBool(inst.FeatureNoThrowTrue)
	if err != nil {
		return

	}
	_, err = enc.EncodeString("noThrowFalse")
	if err != nil {
		return
	}
	_, err = enc.EncodeBool(inst.FeatureNoThrowFalse)
	if err != nil {
		return

	}
	_, err = enc.EncodeString("minId")
	if err != nil {
		return
	}
	_, err = enc.EncodeUint(uint64(inst.MinId))
	if err != nil {
		return
	}
	_, err = enc.EncodeString("maxId")
	if err != nil {
		return
	}
	_, err = enc.EncodeUint(uint64(inst.MaxId))
	if err != nil {
		return
	}
	_, err = enc.EncodeString("hash")
	if err != nil {
		return
	}
	_, err = enc.EncodeTagSmall(cbor.TagExpectConversionToHex)
	if err != nil {
		return
	}
	_, err = enc.EncodeByteSlice(inst.Hash)
	if err != nil {
		return
	}

	return
}

const hashSize = 128 / 8

func NewBackendInterfaceExporter(enc cbor.FullEncoder, cppNamer *Namer) *BackendInterfaceExporter {
	buf := bytes.NewBuffer(make([]byte, 0, 4096*4))
	hasher := blake3.New(hashSize, nil)
	inst := &BackendInterfaceExporter{
		bufSignature:   buf,
		bufDescription: bytes.NewBuffer(make([]byte, 0, 4096*4)),
		enc:            enc,
		hasher:         hasher,
		cppNamer:       cppNamer,
		compatRecord:   NewCompatibilityRecord(),
	}
	inst.Reset()
	return inst
}
func (inst *BackendInterfaceExporter) encodeUtf8StringSlice(sl []string) (err error) {
	enc := inst.enc
	n := len(sl)
	_, err = enc.EncodeArrayDefinite(uint64(n))
	if err != nil || n == 0 {
		return
	}
	for _, s := range sl {
		_, err = enc.EncodeString(s)
		if err != nil {
			return
		}
	}
	return
}
func (inst *BackendInterfaceExporter) encodeNamesAndTypes(names []string, goTypes []string) (err error) {
	var n uint64 = 1
	cppNamer := inst.cppNamer
	if cppNamer != nil {
		n++
	}
	enc := inst.enc
	_, err = enc.EncodeString("names")
	if err != nil {
		return
	}
	err = inst.encodeUtf8StringSlice(names)
	if err != nil {
		return
	}
	_, err = enc.EncodeString("types")
	if err != nil {
		return
	}
	_, err = enc.EncodeMapDefinite(n)
	if err != nil {
		return
	}
	_, err = enc.EncodeString("go")
	if err != nil {
		return
	}
	err = inst.encodeUtf8StringSlice(goTypes)
	if err != nil {
		return
	}
	if cppNamer != nil {
		_, err = enc.EncodeString("c++")
		if err != nil {
			return
		}
		_, err = enc.EncodeArrayDefinite(uint64(len(goTypes)))
		if err != nil {
			return
		}
		for _, t := range goTypes {
			var c string
			c, err = cppNamer.GoTypeNameToCppTypeName(t)
			if err != nil {
				return
			}
			_, err = enc.EncodeString(c)
			if err != nil {
				return
			}
		}
	}
	return
}
func (inst *BackendInterfaceExporter) encodeTypes(goTypes []string) (err error) {
	enc := inst.enc
	_, err = enc.EncodeString("types")
	if err != nil {
		return
	}
	_, err = enc.EncodeMapDefinite(1)
	if err != nil {
		return
	}
	_, err = enc.EncodeString("go")
	if err != nil {
		return
	}
	err = inst.encodeUtf8StringSlice(goTypes)
	if err != nil {
		return
	}
	return
}

func (inst *BackendInterfaceExporter) AddFunction(decl *ast.FuncDecl, resolver TypeResolver, id runtime.FuncProcId, nothrow bool) (err error) {
	inst.compatRecord.Add(id, nothrow)
	enc := inst.enc
	enc.SetWriter(inst.bufSignature)
	err = inst.addFunctionSignature(decl, resolver, id, nothrow)
	if err != nil {
		return err
	}
	enc.SetWriter(inst.bufDescription)
	err = inst.addFunctionDescription(decl, resolver, id, nothrow)
	if err != nil {
		return err
	}
	return
}
func (inst *BackendInterfaceExporter) addFunctionSignature(decl *ast.FuncDecl, resolver TypeResolver, id runtime.FuncProcId, nothrow bool) (err error) {
	var paramGoTypes, resultGoTypes []string
	_, paramGoTypes, _, _, _, resultGoTypes, _, _, _, err = getParamsAndResultTypes(decl, resolver)
	if err != nil {
		return err
	}
	enc := inst.enc

	_, err = enc.EncodeMapDefinite(4)
	if err != nil {
		return
	}
	{ // 0
		_, err = enc.EncodeString("name")
		if err != nil {
			return
		}
		_, err = enc.EncodeString(decl.Name.Name)
		if err != nil {
			return
		}

	}
	{ // 1
		_, err = enc.EncodeString("id")
		if err != nil {
			return
		}
		_, err = enc.EncodeUint(uint64(id))
		if err != nil {
			return
		}
	}
	{ // 2
		_, err = enc.EncodeString("parameters")
		if err != nil {
			return
		}
		_, err = enc.EncodeMapDefinite(1)
		if err != nil {
			return
		}
		err = inst.encodeTypes(paramGoTypes)
		if err != nil {
			return
		}
	}
	{ // 3
		_, err = enc.EncodeString("results")
		if err != nil {
			return
		}
		_, err = enc.EncodeMapDefinite(1)
		if err != nil {
			return
		}
		err = inst.encodeTypes(resultGoTypes)
		if err != nil {
			return
		}
	}
	return
}
func (inst *BackendInterfaceExporter) addFunctionDescription(decl *ast.FuncDecl, resolver TypeResolver, id runtime.FuncProcId, nothrow bool) (err error) {
	var paramNames, paramGoTypes, resultNames, resultGoTypes []string
	var explicitErrVarName string
	paramNames, paramGoTypes, _, _, resultNames, resultGoTypes, _, _, explicitErrVarName, err = getParamsAndResultTypes(decl, resolver)
	if err != nil {
		return err
	}
	enc := inst.enc

	_, err = enc.EncodeMapDefinite(7)
	if err != nil {
		return
	}
	{ // 0
		_, err = enc.EncodeString("kind")
		if err != nil {
			return
		}
		if len(resultNames) > 0 {
			_, err = enc.EncodeString("function")
		} else {
			_, err = enc.EncodeString("procedure")
		}
		if err != nil {
			return
		}
	}
	{ // 1
		_, err = enc.EncodeString("name")
		if err != nil {
			return
		}
		_, err = enc.EncodeString(decl.Name.Name)
		if err != nil {
			return
		}

	}
	{ // 2
		_, err = enc.EncodeString("id")
		if err != nil {
			return
		}
		_, err = enc.EncodeUint(uint64(id))
		if err != nil {
			return
		}
	}
	{ // 3
		_, err = enc.EncodeString("explicitErrVarName")
		if err != nil {
			return
		}
		_, err = enc.EncodeString(explicitErrVarName)
		if err != nil {
			return
		}
	}
	{ // 4
		_, err = enc.EncodeString("parameters")
		if err != nil {
			return
		}
		_, err = enc.EncodeMapDefinite(2)
		if err != nil {
			return
		}
		err = inst.encodeNamesAndTypes(paramNames, paramGoTypes)
		if err != nil {
			return
		}
	}
	{ // 5
		_, err = enc.EncodeString("results")
		if err != nil {
			return
		}
		_, err = enc.EncodeMapDefinite(2)
		if err != nil {
			return
		}
		err = inst.encodeNamesAndTypes(resultNames, resultGoTypes)
		if err != nil {
			return
		}
	}
	{ // 6
		_, err = enc.EncodeString("comment")
		if err != nil {
			return
		}
		if decl.Doc == nil {
			_, err = enc.EncodeNil()
		} else {
			_, err = enc.EncodeString(decl.Doc.Text())
		}
		if err != nil {
			return
		}
	}
	return
}

func (inst *BackendInterfaceExporter) Reset() {
	inst.bufSignature.Reset()
	inst.enc.Reset()
	inst.enc.SetWriter(inst.bufSignature)
	inst.hasher.Reset()
	inst.compatRecord.Reset()
}

func (inst *BackendInterfaceExporter) Emit(out io.Writer) (n int, err error) {
	enc := inst.enc
	b := bufio.NewWriter(out)
	defer b.Flush()
	enc.SetWriter(b)
	_, err = enc.EncodeMapIndefinite()
	if err != nil {
		return
	}
	_, err = enc.EncodeString("interface")
	if err != nil {
		return
	}
	_, err = enc.EncodeMapDefinite(2)
	if err != nil {
		return
	}
	{ // 0
		_, err = enc.EncodeString("signature")
		if err != nil {
			return
		}
		_, err = enc.EncodeArrayIndefinite()
		if err != nil {
			return
		}
		err = b.Flush()
		if err != nil {
			return
		}
		_, err = inst.bufSignature.WriteTo(b)
		if err != nil {
			return
		}
		_, err = enc.EncodeBreak()
		if err != nil {
			return
		}
	}
	{ // 1
		_, err = enc.EncodeString("description")
		if err != nil {
			return
		}
		_, err = enc.EncodeArrayIndefinite()
		if err != nil {
			return
		}
		err = b.Flush()
		if err != nil {
			return
		}
		_, err = inst.bufDescription.WriteTo(b)
		if err != nil {
			return
		}
		_, err = enc.EncodeBreak()
		if err != nil {
			return
		}
	}

	_, err = enc.EncodeString("compatibility")
	if err != nil {
		return
	}
	inst.compatRecord.Hash = inst.hasher.Sum(inst.compatRecord.Hash[:0])
	err = inst.compatRecord.Encode(enc)
	if err != nil {
		return
	}

	_, err = enc.EncodeBreak()
	if err != nil {
		return
	}

	var n2 int64
	n2, err = inst.bufSignature.WriteTo(out)
	n = int(n2)
	inst.Reset()
	return
}
func (inst *BackendInterfaceExporter) GetCompatibilityRecord() *CompatibilityRecord {
	return inst.compatRecord
}

var _ CodeTransformerBackend = (*BackendInterfaceExporter)(nil)
