package runtime

import (
	"iter"
)

func GetBoolRetr[D UnmarshallReaderI, T ~bool](unmarshaller D) (r T) {
	r = GetUint8Retr[D, uint8](unmarshaller) != 0
	return
}

func GetUint8Retr[D UnmarshallReaderI, T ~uint8](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadUInt8())
	return
}

func GetUint16Retr[D UnmarshallReaderI, T ~uint16](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadUInt16())
	return
}

func GetUint32Retr[D UnmarshallReaderI, T ~uint32](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadUInt32())
	return
}

func GetUint64Retr[D UnmarshallReaderI, T ~uint64](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadUInt64())
	return
}

func GetStringRetr[D UnmarshallReaderI, T ~string](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadString())
	return
}
func GetStringRetrMostLikelyEmpty[D UnmarshallReaderI, T ~string](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadStringMostLikelyEmpty())
	return
}

func GetInt8Retr[D UnmarshallReaderI, T ~int8](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadInt8())
	return
}

func GetInt16Retr[D UnmarshallReaderI, T ~int16](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadInt16())
	return
}

func GetInt32Retr[D UnmarshallReaderI, T ~int32](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadInt32())
	return
}

func GetInt64Retr[D UnmarshallReaderI, T ~int64](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadInt64())
	return
}

func GetFloat32Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadFloat32())
	return
}

func GetFloat32Array3Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r [3]T) {
	r[0] = T(unmarshaller.ReadFloat32())
	r[1] = T(unmarshaller.ReadFloat32())
	r[2] = T(unmarshaller.ReadFloat32())
	return
}

func GetFloat32Array4Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r [4]T) {
	r[0] = T(unmarshaller.ReadFloat32())
	r[1] = T(unmarshaller.ReadFloat32())
	r[2] = T(unmarshaller.ReadFloat32())
	r[3] = T(unmarshaller.ReadFloat32())
	return
}

func GetFloat32Array2Retr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r [2]T) {
	r[0] = T(unmarshaller.ReadFloat32())
	r[1] = T(unmarshaller.ReadFloat32())
	return
}

func GetFloat64Retr[D UnmarshallReaderI, T ~float64](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadFloat64())
	return
}

func GetComplex64Retr[D UnmarshallReaderI, T ~complex64](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadComplex64())
	return
}

func GetComplex128Retr[D UnmarshallReaderI, T ~complex128](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadComplex128())
	return
}

func GetUintptrRetr[D UnmarshallReaderI, T ~uintptr](unmarshaller D) (r T) {
	r = T(unmarshaller.ReadUintptr())
	return
}

func GetBytesRetr[D UnmarshallReaderI, T byte](unmarshaller D) (r []byte) {
	r = unmarshaller.ReadBytes()
	return
}

func GetBoolSliceRetr[D UnmarshallReaderI, T ~bool](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadBool()))
	}
	return
}

func GetFloat32SliceRetr[D UnmarshallReaderI, T ~float32](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadFloat32()))
	}
	return
}

func GetFloat64SliceRetr[D UnmarshallReaderI, T ~float64](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadFloat64()))
	}
	return
}

func GetUint8SliceRetr[D UnmarshallReaderI, T ~uint8](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadUInt8()))
	}
	return
}

func GetUint16SliceRetr[D UnmarshallReaderI, T ~uint16](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadUInt16()))
	}
	return
}

func GetUint32SliceRetr[D UnmarshallReaderI, T ~uint32](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadUInt32()))
	}
	return
}

func GetUint64SliceRetr[D UnmarshallReaderI, T ~uint64](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadUInt64()))
	}
	return
}
func IterateUint64SliceRetr[D UnmarshallReaderI, T ~uint64](unmarshaller D) iter.Seq[T] {
	return func(yield func(T) bool) {
		l, isNil := unmarshaller.ReadSliceLength()
		if isNil {
			return
		}
		var i int
		for i = 0; i < l; i++ {
			if !yield(T(unmarshaller.ReadUInt64())) {
				break
			}
		}
		for ; i < l; i++ {
			_ = unmarshaller.ReadUInt64()
		}
	}
}
func IterateUint32SliceRetr[D UnmarshallReaderI, T ~uint32](unmarshaller D) iter.Seq[T] {
	return func(yield func(T) bool) {
		l, isNil := unmarshaller.ReadSliceLength()
		if isNil {
			return
		}
		var i int
		for i = 0; i < l; i++ {
			if !yield(T(unmarshaller.ReadUInt32())) {
				break
			}
		}
		for ; i < l; i++ {
			_ = unmarshaller.ReadUInt32()
		}
	}
}
func IterateFloat64SliceRetr[D UnmarshallReaderI, T ~float64](unmarshaller D) iter.Seq[T] {
	return func(yield func(T) bool) {
		l, isNil := unmarshaller.ReadSliceLength()
		if isNil {
			return
		}
		var i int
		for i = 0; i < l; i++ {
			if !yield(T(unmarshaller.ReadFloat64())) {
				break
			}
		}
		for ; i < l; i++ {
			_ = unmarshaller.ReadFloat64()
		}
	}
}
func IterateFloat32SliceRetr[D UnmarshallReaderI, T ~float32](unmarshaller D) iter.Seq[T] {
	return func(yield func(T) bool) {
		l, isNil := unmarshaller.ReadSliceLength()
		if isNil {
			return
		}
		var i int
		for i = 0; i < l; i++ {
			if !yield(T(unmarshaller.ReadFloat32())) {
				break
			}
		}
		for ; i < l; i++ {
			_ = unmarshaller.ReadFloat32()
		}
	}
}
func IterateInt64SliceRetr[D UnmarshallReaderI, T ~int64](unmarshaller D) iter.Seq[T] {
	return func(yield func(T) bool) {
		l, isNil := unmarshaller.ReadSliceLength()
		if isNil {
			return
		}
		var i int
		for i = 0; i < l; i++ {
			if !yield(T(unmarshaller.ReadInt64())) {
				break
			}
		}
		for ; i < l; i++ {
			_ = unmarshaller.ReadInt64()
		}
	}
}
func IterateStringSliceRetr[D UnmarshallReaderI, T ~string](unmarshaller D) iter.Seq[T] {
	return func(yield func(T) bool) {
		l, isNil := unmarshaller.ReadSliceLength()
		if isNil {
			return
		}
		var i int
		for i = 0; i < l; i++ {
			if !yield(T(unmarshaller.ReadString())) {
				break
			}
		}
		for ; i < l; i++ {
			_ = unmarshaller.ReadString()
		}
	}
}

func GetInt8SliceRetr[D UnmarshallReaderI, T ~int8](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadInt8()))
	}
	return
}

func GetInt16SliceRetr[D UnmarshallReaderI, T ~int16](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadInt16()))
	}
	return
}

func GetInt32SliceRetr[D UnmarshallReaderI, T ~int32](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadInt32()))
	}
	return
}

func GetInt64SliceRetr[D UnmarshallReaderI, T ~int64](unmarshaller D) (r []T) {
	l, isNil := unmarshaller.ReadSliceLength()
	if isNil {
		return nil
	}
	r = make([]T, 0, l)
	for i := 0; i < l; i++ {
		r = append(r, T(unmarshaller.ReadInt64()))
	}
	return
}
