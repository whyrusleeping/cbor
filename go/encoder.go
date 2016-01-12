package cbor

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"reflect"
	"sort"
)

// Return new Encoder object for writing to supplied io.Writer.
//
// TODO: set options on Encoder object.
func NewEncoder(out io.Writer) *Encoder {
	return &Encoder{out, make([]byte, 9)}
}

func (enc *Encoder) Encode(ob interface{}) error {
	switch x := ob.(type) {
	case int:
		return enc.writeInt(int64(x))
	case int8:
		return enc.writeInt(int64(x))
	case int16:
		return enc.writeInt(int64(x))
	case int32:
		return enc.writeInt(int64(x))
	case int64:
		return enc.writeInt(x)
	case uint:
		return enc.tagAuxOut(cborUint, uint64(x))
	case uint8: /* aka byte */
		return enc.tagAuxOut(cborUint, uint64(x))
	case uint16:
		return enc.tagAuxOut(cborUint, uint64(x))
	case uint32:
		return enc.tagAuxOut(cborUint, uint64(x))
	case uint64:
		return enc.tagAuxOut(cborUint, x)
	case float32:
		return enc.writeFloat(float64(x))
	case float64:
		return enc.writeFloat(x)
	case string:
		return enc.writeText(x)
	case []byte:
		return enc.writeBytes(x)
	case bool:
		return enc.writeBool(x)
	case nil:
		return enc.tagAuxOut(cbor7, uint64(cborNull))
	case big.Int:
		return fmt.Errorf("TODO: encode big.Int")
	}

	// If none of the simple types work, try reflection
	return enc.writeReflection(reflect.ValueOf(ob))
}

func (enc *Encoder) writeReflection(rv reflect.Value) error {
	var err error
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return enc.writeInt(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return enc.tagAuxOut(cborUint, rv.Uint())
	case reflect.Float32, reflect.Float64:
		return enc.writeFloat(rv.Float())
	case reflect.Bool:
		return enc.writeBool(rv.Bool())
	case reflect.String:
		return enc.writeText(rv.String())
	case reflect.Slice, reflect.Array:
		elemType := rv.Type().Elem()
		if elemType.Kind() == reflect.Uint8 {
			// special case, write out []byte
			return enc.writeBytes(rv.Bytes())
		}
		alen := rv.Len()
		err = enc.tagAuxOut(cborArray, uint64(alen))
		for i := 0; i < alen; i++ {
			err = enc.writeReflection(rv.Index(i))
			if err != nil {
				log.Printf("error at array elem %d", i)
				return err
			}
		}
		return nil
	case reflect.Map:
		err = enc.tagAuxOut(cborMap, uint64(rv.Len()))
		if err != nil {
			return err
		}

		dup := func(b []byte) []byte {
			out := make([]byte, len(b))
			copy(out, b)
			return out
		}

		keys := rv.MapKeys()
		buf := new(bytes.Buffer)
		encKeys := make([]cborKeyEntry, 0, len(keys))
		for _, krv := range keys {
			tempEnc := NewEncoder(buf)
			err := tempEnc.writeReflection(krv)
			if err != nil {
				log.Println("error encoding map key", err)
				return err
			}
			kval := dup(buf.Bytes())
			encKeys = append(encKeys, cborKeyEntry{
				val: kval,
				key: krv,
			})
			buf.Reset()
		}

		sort.Sort(cborKeySorter(encKeys))

		for _, ek := range encKeys {
			vrv := rv.MapIndex(ek.key)

			_, err := enc.out.Write(ek.val)
			if err != nil {
				log.Printf("error writing map key")
				return err
			}
			err = enc.writeReflection(vrv)
			if err != nil {
				log.Printf("error encoding map val")
				return err
			}
		}

		return nil
	case reflect.Struct:
		// TODO: check for big.Int ?
		numfields := rv.NumField()
		structType := rv.Type()
		usableFields := 0
		for i := 0; i < numfields; i++ {
			fieldinfo := structType.Field(i)
			_, ok := fieldname(fieldinfo)
			if !ok {
				continue
			}
			usableFields++
		}
		err = enc.tagAuxOut(cborMap, uint64(usableFields))
		if err != nil {
			return err
		}
		for i := 0; i < numfields; i++ {
			fieldinfo := structType.Field(i)
			fieldname, ok := fieldname(fieldinfo)
			if !ok {
				continue
			}
			err = enc.writeText(fieldname)
			if err != nil {
				return err
			}
			err = enc.writeReflection(rv.Field(i))
			if err != nil {
				return err
			}
		}
		return nil
	case reflect.Interface:
		//return fmt.Errorf("TODO: serialize interface{} k=%s T=%s", rv.Kind().String(), rv.Type().String())
		return enc.Encode(rv.Interface())
	case reflect.Ptr:
		if rv.IsNil() {
			return enc.tagAuxOut(cbor7, uint64(cborNull))
		}
		return enc.writeReflection(reflect.Indirect(rv))
	}

	return fmt.Errorf("don't know how to CBOR serialize k=%s t=%s", rv.Kind().String(), rv.Type().String())
}

type cborKeySorter []cborKeyEntry
type cborKeyEntry struct {
	val []byte
	key reflect.Value
}

func (cks cborKeySorter) Len() int { return len(cks) }
func (cks cborKeySorter) Swap(i, j int) {
	cks[i], cks[j] = cks[j], cks[i]
}

func (cks cborKeySorter) Less(i, j int) bool {
	a := keyBytesFromEncoded(cks[i].val)
	b := keyBytesFromEncoded(cks[j].val)
	switch {
	case len(a) < len(b):
		return true
	case len(a) > len(b):
		return false
	default:
		if bytes.Compare(a, b) < 0 {
			return true
		}
		return false
	}
}

func keyBytesFromEncoded(data []byte) []byte {
	cborInfo := data[0] & infoBits

	if cborInfo <= 23 {
		return data[1:]
	} else if cborInfo == int8Follows {
		return data[2:]
	} else if cborInfo == int16Follows {
		return data[3:]
	} else if cborInfo == int32Follows {
		return data[5:]
	} else if cborInfo == int64Follows {
		return data[9:]
	}
	panic("shouldnt actually hit this")
}

func (enc *Encoder) writeInt(x int64) error {
	if x < 0 {
		return enc.tagAuxOut(cborNegint, uint64(-1-x))
	}
	return enc.tagAuxOut(cborUint, uint64(x))
}

func (enc *Encoder) tagAuxOut(tag byte, x uint64) error {
	var err error
	if x <= 23 {
		// tiny literal
		enc.scratch[0] = tag | byte(x)
		_, err = enc.out.Write(enc.scratch[:1])
	} else if x < 0x0ff {
		enc.scratch[0] = tag | int8Follows
		enc.scratch[1] = byte(x & 0x0ff)
		_, err = enc.out.Write(enc.scratch[:2])
	} else if x < 0x0ffff {
		enc.scratch[0] = tag | int16Follows
		enc.scratch[1] = byte((x >> 8) & 0x0ff)
		enc.scratch[2] = byte(x & 0x0ff)
		_, err = enc.out.Write(enc.scratch[:3])
	} else if x < 0x0ffffffff {
		enc.scratch[0] = tag | int32Follows
		enc.scratch[1] = byte((x >> 24) & 0x0ff)
		enc.scratch[2] = byte((x >> 16) & 0x0ff)
		enc.scratch[3] = byte((x >> 8) & 0x0ff)
		enc.scratch[4] = byte(x & 0x0ff)
		_, err = enc.out.Write(enc.scratch[:5])
	} else {
		err = enc.tagAux64(tag, x)
	}
	return err
}
func (enc *Encoder) tagAux64(tag byte, x uint64) error {
	enc.scratch[0] = tag | int64Follows
	enc.scratch[1] = byte((x >> 56) & 0x0ff)
	enc.scratch[2] = byte((x >> 48) & 0x0ff)
	enc.scratch[3] = byte((x >> 40) & 0x0ff)
	enc.scratch[4] = byte((x >> 32) & 0x0ff)
	enc.scratch[5] = byte((x >> 24) & 0x0ff)
	enc.scratch[6] = byte((x >> 16) & 0x0ff)
	enc.scratch[7] = byte((x >> 8) & 0x0ff)
	enc.scratch[8] = byte(x & 0x0ff)
	_, err := enc.out.Write(enc.scratch[:9])
	return err
}

func (enc *Encoder) writeText(x string) error {
	enc.tagAuxOut(cborText, uint64(len(x)))
	_, err := io.WriteString(enc.out, x)
	return err
}

func (enc *Encoder) writeBytes(x []byte) error {
	enc.tagAuxOut(cborBytes, uint64(len(x)))
	_, err := enc.out.Write(x)
	return err
}

func (enc *Encoder) writeFloat(x float64) error {
	return enc.tagAux64(cbor7, math.Float64bits(x))
}

func (enc *Encoder) writeBool(x bool) error {
	if x {
		return enc.tagAuxOut(cbor7, uint64(cborTrue))
	} else {
		return enc.tagAuxOut(cbor7, uint64(cborFalse))
	}
}

type Encoder struct {
	out io.Writer

	scratch []byte
}
