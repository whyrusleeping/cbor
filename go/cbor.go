// Should be roughly like encoding/gob
// encoding/json has an inferior interface that only works on whole messages to/from whole blobs at once. Reader/Writer based interfaces are better.

package cbor

import (
	"bytes"
	"io"
	"reflect"
	"strings"
)

var typeMask byte = 0xE0
var infoBits byte = 0x1F

/* type values */
var cborUint byte = 0x00
var cborNegint byte = 0x20
var cborBytes byte = 0x40
var cborText byte = 0x60
var cborArray byte = 0x80
var cborMap byte = 0xA0
var cborTag byte = 0xC0
var cbor7 byte = 0xE0

/* cbor7 values */
const (
	cborFalse byte = 20
	cborTrue  byte = 21
	cborNull  byte = 22
)

/* info bits */
var int8Follows byte = 24
var int16Follows byte = 25
var int32Follows byte = 26
var int64Follows byte = 27
var varFollows byte = 31

/* tag values */
var tagBignum uint64 = 2
var tagNegBignum uint64 = 3
var tagDecimal uint64 = 4
var tagBigfloat uint64 = 5

// TODO: honor encoding.BinaryMarshaler interface and encapsulate blob returned from that.

// Load one object into v
func Loads(blob []byte, v interface{}) error {
	dec := NewDecoder(bytes.NewReader(blob))
	return dec.Decode(v)
}

// copied from encoding/json/decode.go
// An InvalidUnmarshalError describes an invalid argument passed to Unmarshal.
// (The argument to Unmarshal must be a non-nil pointer.)
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "json: Unmarshal(nil)"
	}

	if e.Type.Kind() != reflect.Ptr {
		return "json: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "json: Unmarshal(nil " + e.Type.String() + ")"
}

type CBORTag struct {
	Tag           uint64
	WrappedObject interface{}
}

// parse StructField.Tag.Get("json" or "cbor")
func fieldTagName(xinfo string) (string, bool) {
	if len(xinfo) != 0 {
		// e.g. `json:"field_name,omitempty"`, or same for cbor
		// TODO: honor 'omitempty' option
		jiparts := strings.Split(xinfo, ",")
		if len(jiparts) > 0 {
			fieldName := jiparts[0]
			if len(fieldName) > 0 {
				return fieldName, true
			}
		}
	}
	return "", false
}

// Return fieldname, bool; if bool is false, don't use this field
func fieldname(fieldinfo reflect.StructField) (string, bool) {
	if fieldinfo.PkgPath != "" {
		// has path to private package. don't export
		return "", false
	}
	fieldname, ok := fieldTagName(fieldinfo.Tag.Get("cbor"))
	if !ok {
		fieldname, ok = fieldTagName(fieldinfo.Tag.Get("json"))
	}
	if ok {
		if fieldname == "" {
			return fieldinfo.Name, true
		}
		if fieldname == "-" {
			return "", false
		}
		return fieldname, true
	}
	return fieldinfo.Name, true
}

// Write out an object to an io.Writer
func Encode(out io.Writer, ob interface{}) error {
	return NewEncoder(out).Encode(ob)
}

// Write out an object to a new byte slice
func Dumps(ob interface{}) ([]byte, error) {
	writeTarget := &bytes.Buffer{}
	writeTarget.Grow(20000)
	err := Encode(writeTarget, ob)
	if err != nil {
		return nil, err
	}
	return writeTarget.Bytes(), nil
}
