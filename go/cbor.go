// Should be roughly like encoding/gob
// encoding/json has an inferior interface that only works on whole messages to/from whole blobs at once. Reader/Writer based interfaces are better.

package cbor


import "errors"
import "fmt"
import "io"
import "log"
import "math"
import "math/big"
import "reflect"
import "strings"

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

type Decoder struct {
	rin io.Reader

	// tag byte
	c []byte

	// many values fit within the next 8 bytes
	b8 []byte
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		r,
		make([]byte, 1),
		make([]byte, 8),
	}
}
func (dec *Decoder) Decode(v interface{}) error {
        rv := reflect.ValueOf(v)

	return dec.reflectDecode(rv)
}
func (dec *Decoder) reflectDecode(rv reflect.Value) error {
	var didread int
	var err error

	didread, err = io.ReadFull(dec.rin, dec.c)

	if didread == 1 {
		/* log.Printf("got one %x\n", dec.c[0]) */
	}

	if err != nil {
		return err
	}

        if (!rv.CanSet()) && (rv.Kind() != reflect.Ptr || rv.IsNil()) {
                return &InvalidUnmarshalError{rv.Type()}
        }
	return dec.innerDecodeC(rv, dec.c[0])
}

func (dec *Decoder) handleInfoBits(cborInfo byte) (uint64, error) {
	var aux uint64

	if (cborInfo <= 23) {
		aux = uint64(cborInfo)
		return aux, nil
	} else if (cborInfo == int8Follows) {
		didread, err := io.ReadFull(dec.rin, dec.b8[:1])
		if didread == 1 {
			aux = uint64(dec.b8[0])
		}
		return aux, err
	} else if (cborInfo == int16Follows) {
		didread, err := io.ReadFull(dec.rin, dec.b8[:2])
		if didread == 2 {
			aux = (uint64(dec.b8[0]) << 8) | uint64(dec.b8[1])
		}
		return aux, err
	} else if (cborInfo == int32Follows) {
		didread, err := io.ReadFull(dec.rin, dec.b8[:4])
		if didread == 4 {
		aux = (uint64(dec.b8[0]) << 24) |
			(uint64(dec.b8[1]) << 16) |
			(uint64(dec.b8[2]) <<  8) |
			uint64(dec.b8[3])
		}
		return aux, err
	} else if (cborInfo == int64Follows) {
		didread, err := io.ReadFull(dec.rin, dec.b8)
		if didread == 8 {
			var shift uint = 56
			i := 0
			aux = uint64(dec.b8[i]) << shift
			for i < 7 {
				i += 1
				shift -= 8
				aux |= uint64(dec.b8[i]) << shift
			}
		}
		return aux, err
	}
	return 0, nil
}

func (dec *Decoder) innerDecodeC(rv reflect.Value, c byte) error {
	cborType := c & typeMask
	cborInfo := c & infoBits

	aux, err := dec.handleInfoBits(cborInfo)
	if err != nil {
		log.Printf("error in handleInfoBits: %v", err)
		return err
	}
	//log.Printf("cborType %x cborInfo %d aux %x", cborType, cborInfo, aux)

	if cborType == cborUint {
		return setUint(rv, aux)
	} else if cborType == cborNegint {
		if aux > 0x7fffffffffffffff {
			//return errors.New(fmt.Sprintf("cannot represent -%d", aux))
			bigU := &big.Int{}
			bigU.SetUint64(aux)
			minusOne := big.NewInt(-1)
			bn := &big.Int{}
			bn.Sub(minusOne, bigU)
			//log.Printf("built big negint: %v", bn)
			return setBignum(rv, bn)
		}
		return setInt(rv, -1 - int64(aux))
	} else if cborType == cborBytes {
		log.Printf("cborType %x bytes cborInfo %d aux %x", cborType, cborInfo, aux)
		if cborInfo == varFollows {
			return errors.New("TODO: implement var bytes")
		} else {
			val := make([]byte, aux)
			_, err = io.ReadFull(dec.rin, val)
			// Don't really care about count, ReadFull will make it all or none and we can just fall out with whatever error
		}
	} else if cborType == cborText {
		if cborInfo == varFollows {
			parts := make([]string, 0, 1)
			subc := []byte{0}
			for true {
				_, err = io.ReadFull(dec.rin, subc)
				if err != nil {
					log.Printf("error reading next byte for var text")
					return err
				}
				if subc[0] == 0xff {
					// done
					joined := strings.Join(parts, "")
					reflect.Indirect(rv).Set(reflect.ValueOf(joined))
					return nil
				} else {
					var subtext interface{}
					err = dec.innerDecodeC(reflect.ValueOf(&subtext), subc[0])
					if err != nil {
						log.Printf("error decoding subtext")
						return err
					}
					st, ok := subtext.(string)
					if ok {
						parts = append(parts, st)
					} else {
						return fmt.Errorf("var text sub element not text but %T", subtext)
					}
				}
			}
		} else {
			raw := make([]byte, aux)
			_, err = io.ReadFull(dec.rin, raw)
			//reflect.Indirect(rv).SetString(string(raw))
			reflect.Indirect(rv).Set(reflect.ValueOf(string(raw)))
			 return nil
		 }
	 } else if cborType == cborArray {
		 return dec.decodeArray(rv, cborInfo, aux)
	 } else if cborType == cborMap {
		 return dec.decodeMap(rv, cborInfo, aux)
	 } else if cborType == cborTag {
		 /*var innerOb interface{}*/
		 ic := []byte{0}
		 _, err = io.ReadFull(dec.rin, ic)
		 if err != nil {
			 return err
		 }
		 if aux == tagBignum {
			 bn, err := dec.decodeBignum(ic[0])
			 if err != nil {
				 return err
			 }
			 return setBignum(rv, bn)
		 } else if aux == tagNegBignum {
			 bn, err := dec.decodeBignum(ic[0])
			 if err != nil {
				 return err
			 }
			 minusOne := big.NewInt(-1)
			 bnOut := &big.Int{}
			 bnOut.Sub(minusOne, bn)
			 return setBignum(rv, bnOut)
		 } else if aux == tagDecimal {
			 log.Printf("TODO: directly read bytes into decimal")
		 } else if aux == tagBigfloat {
			 log.Printf("TODO: directly read bytes into bigfloat")
		 } else {
			 log.Printf("TODO: handle cbor tag: %x", aux)
			 return dec.innerDecodeC(rv, ic[0])
		 }
		 return nil
	 } else if cborType == cbor7 {
		 if cborInfo == int16Follows {
			 exp := (aux >> 10) & 0x01f
			 mant := aux & 0x03ff
			 var val float64
			 if exp == 0 {
				 val = math.Ldexp(float64(mant), -24)
			 } else if exp != 31 {
				 val = math.Ldexp(float64(mant + 1024), int(exp - 25))
			 } else if mant == 0 {
				 val = math.Inf(1)
			 } else {
				 val = math.NaN()
			 }
			 if (aux & 0x08000) != 0 {
				 val = -val;
			 }
			 return setFloat64(rv, val)
		 } else if cborInfo == int32Follows {
			 f := math.Float32frombits(uint32(aux))
			 return setFloat32(rv, f)
		 } else if cborInfo == int64Follows {
			 d := math.Float64frombits(aux)
			 return setFloat64(rv, d)
		 } else if cborInfo == 20 {
			 reflect.Indirect(rv).Set(reflect.ValueOf(false))
		 } else if cborInfo == 21 {
			 reflect.Indirect(rv).Set(reflect.ValueOf(true))
		 } else if cborInfo == 22 {
			 rv.Set(reflect.Zero(rv.Type()))
		 }
	 }

	
	return err
}

type MapAssignable interface {
	SetKeyValue(key, value interface{})
	ReflectValueForKey(key interface{}) (*reflect.Value, bool)
	SetReflectValueForKey(key interface{}, value reflect.Value) error
}

type MapReflectValue struct {
	reflect.Value
}

func (irv *MapReflectValue) SetKeyValue(key, value interface{}) {
	irv.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(value))
}
func (irv *MapReflectValue) ReflectValueForKey(key interface{}) (*reflect.Value, bool) {
	//var x interface{}
	//rv := reflect.ValueOf(&x)
	rv := reflect.New(irv.Type().Elem())
	return &rv, true
}
func (irv *MapReflectValue) SetReflectValueForKey(key interface{}, value reflect.Value) error {
	//log.Printf("k T %T v%#v, v T %s v %#v", key, key, value.Type().String(), value.Interface())
	krv := reflect.Indirect(reflect.ValueOf(key))
	vrv := reflect.Indirect(value)
	//log.Printf("irv T %s v %#v", irv.Type().String(), irv.Interface())
	//log.Printf("k T %s v %#v, v T %s v %#v", krv.Type().String(), krv.Interface(), vrv.Type().String(), vrv.Interface())
	irv.SetMapIndex(krv, vrv)
	return nil
}


type StructAssigner struct {
	Srv reflect.Value

	//keyType reflect.Type
}

func (sa *StructAssigner) SetKeyValue(key, value interface{}) {
	skey, ok := key.(string)
	if ! ok {
		log.Printf("skv key is not string, got %T", key)
		return
	}

	ft := sa.Srv.Type()
	numFields := ft.NumField()
	for i := 0; i < numFields; i++ {
		sf := ft.Field(i)
		if (sf.Name == skey) || strings.EqualFold(sf.Name, skey) {
			sa.Srv.FieldByName(sf.Name).Set(reflect.ValueOf(value))
			return
		}
		//log.Printf("field[%d] %#v %s", i, sf.Name, sf.Type)
	}
}

func (sa *StructAssigner) ReflectValueForKey(key interface{}) (*reflect.Value, bool) {
	var skey string
	switch tkey := key.(type) {
	case string:
		skey = tkey
	case *string:
		skey= *tkey
	default:
		log.Printf("rvfk key is not string, got %T", key)
		return nil, false
	}

	ft := sa.Srv.Type()
	numFields := ft.NumField()
	for i := 0; i < numFields; i++ {
		sf := ft.Field(i)
		if (sf.Name == skey) || strings.EqualFold(sf.Name, skey) {
			fieldVal := sa.Srv.FieldByName(sf.Name)
			if !fieldVal.CanSet() {
				log.Printf("cannot set field %s", sf.Name)
				return nil, false
			}
			return &fieldVal, true
		}
	}
	return nil, false
}
func (sa *StructAssigner) SetReflectValueForKey(key interface{}, value reflect.Value) error {
	return nil
}


func (dec *Decoder) setMapKV(krv reflect.Value, ma MapAssignable) error {
	var err error
	val, ok := ma.ReflectValueForKey(krv.Interface())
	if !ok {
		var throwaway interface{}
		err = dec.Decode(&throwaway)
		if err != nil {
			return err
		}
		return nil
	}
	err = dec.reflectDecode(*val)
	if err != nil {
		log.Printf("error decoding map val: T %T v %#v v.T %s", val, val, val.Type().String())
		return err
	}
	err = ma.SetReflectValueForKey(krv.Interface(), *val)
	if err != nil {
		log.Printf("error setting value")
		return err
	}

	return nil
}


func (dec *Decoder) decodeMap(rv reflect.Value, cborInfo byte, aux uint64) error {
	// dereferenced reflect value
	var drv reflect.Value
	if rv.Kind() == reflect.Ptr {
		drv = reflect.Indirect(rv)
	} else {
		drv = rv
	}

	// inner reflect value
	var irv reflect.Value
	var ma MapAssignable

	var keyType reflect.Type
	//var valType reflect.Type

	switch drv.Kind() {
	case reflect.Interface:
		// TODO: maybe I should make this map[string]interface{}
		nob := make(map[interface{}]interface{})
		irv = reflect.ValueOf(nob)
		ma = &MapReflectValue{irv}
		keyType = irv.Type().Key()
		//valType = irv.Type().Elem()
	case reflect.Struct:
		/*
		ft := drv.Type()
		numFields := ft.NumField()
		for i := 0; i < numFields; i++ {
			sf := ft.Field(i)
			//log.Printf("field[%d] %#v %s", i, sf.Name, sf.Type)
		}
*/
		ma = &StructAssigner{drv}
		keyType = reflect.TypeOf("")
	case reflect.Map:
		log.Printf("decode into %s", drv.Type().String())
		if drv.IsNil() {
			if drv.CanSet() {
				drv.Set(reflect.MakeMap(drv.Type()))
			} else {
				return fmt.Errorf("target map is nil and not settable")
			}
		}
		keyType = drv.Type().Key()
		ma = &MapReflectValue{drv}
	default:
		return fmt.Errorf("can't read map into %s", rv.Type().String())
	}

	var err error

	if cborInfo == varFollows {
		subc := []byte{0}
		for true {
			_, err = io.ReadFull(dec.rin, subc)
			if err != nil {
				log.Printf("error reading next byte for var text")
				return err
			}
			if subc[0] == 0xff {
				// Done
				break
			} else {
				//var key interface{}
				krv := reflect.New(keyType)
				//var val interface{}
				err = dec.innerDecodeC(krv, subc[0])
				if err != nil {
					log.Printf("error decoding map key")
					return err
				}

				err = dec.setMapKV(krv, ma)
				if err != nil {
					return err
				}
			}
		}
	} else {
		var i uint64
		for i = 0; i < aux; i++ {
			//var key interface{}
			krv := reflect.New(keyType)
			//var val interface{}
			//err = dec.Decode(&key)
			err = dec.reflectDecode(krv)
			if err != nil {
				log.Printf("error decoding map key")
				return err
			}
			err = dec.setMapKV(krv, ma)
			if err != nil {
				return err
			}
		}
	}

	if drv.Kind() == reflect.Interface {
		drv.Set(irv)
	}
	return nil
}

func (dec *Decoder) decodeArray(rv reflect.Value, cborInfo byte, aux uint64) error {
	if rv.Kind() == reflect.Ptr {
		rv = reflect.Indirect(rv)
	}

	var makeLength int = 0
	if cborInfo == varFollows {
		// no special capacity to allocate the slice to
	} else {
		makeLength = int(aux)
	}

	// inner reflect value
	var irv reflect.Value

	switch rv.Kind() {
	case reflect.Interface:
		nob := make([]interface{}, 0, makeLength)
		irv = reflect.ValueOf(nob)
	case reflect.Slice:
		irv = rv
	default:
		return fmt.Errorf("can't read array into %s", rv.Type().String())
	}

	elemType := irv.Type().Elem()
	var err error

	if cborInfo == varFollows {
		//log.Printf("var array")
		subc := []byte{0}
		for true {
			_, err = io.ReadFull(dec.rin, subc)
			if err != nil {
				log.Printf("error reading next byte for var text")
				return err
			}
			if subc[0] == 0xff {
				// Done
				break
			} else {
				subrv := reflect.New(elemType)
				err := dec.innerDecodeC(subrv, subc[0])
				if err != nil {
					log.Printf("error decoding array subob")
					return err
				}
				irv = reflect.Append(irv, reflect.Indirect(subrv))
			}
		}
	} else {
		var i uint64
		for i = 0; i < aux; i++ {
			subrv := reflect.New(elemType)
			err := dec.reflectDecode(subrv)
			if err != nil {
				log.Printf("error decoding array subob")
				return err
			}
			irv = reflect.Append(irv, reflect.Indirect(subrv))
		}
	}

	rv.Set(irv)

	return nil
}

func (dec *Decoder) decodeBignum(c byte) (*big.Int, error) {
	cborType := c & typeMask
	cborInfo := c & infoBits

	aux, err := dec.handleInfoBits(cborInfo)
	if err != nil {
		log.Printf("error in bignum handleInfoBits: %v", err)
		return nil, err
	}
	//log.Printf("bignum cborType %x cborInfo %d aux %x", cborType, cborInfo, aux)

	if cborType != cborBytes {
		return nil, fmt.Errorf("attempting to decode bignum but sub object is not bytes but type %x", cborType)
	}

	rawbytes := make([]byte, aux)
	_, err = io.ReadFull(dec.rin, rawbytes)
	if err != nil {
		return nil, err
	}

	bn := big.NewInt(0)
	littleBig := &big.Int{}
	d := &big.Int{}
	for _, bv := range(rawbytes) {
		d.Lsh(bn, 8)
		littleBig.SetUint64(uint64(bv))
		bn.Or(d, littleBig)
	}
	
	return bn, nil
}


func setBignum(rv reflect.Value, x *big.Int) error {
	switch rv.Kind() {
	case reflect.Ptr:
		return setBignum(reflect.Indirect(rv), x)
	case reflect.Interface:
		rv.Set(reflect.ValueOf(*x))
		return nil
	case reflect.Int32:
		if x.BitLen() < 32 {
			rv.SetInt(x.Int64())
			return nil
		} else {
			return fmt.Errorf("int too big for int32 target")
		}
	case reflect.Int, reflect.Int64:
		if x.BitLen() < 64 {
			rv.SetInt(x.Int64())
			return nil
		} else {
			return fmt.Errorf("int too big for int64 target")
		}
	default:
		return fmt.Errorf("cannot assign bignum into Kind=%s Type=%s %#v", rv.Kind().String(), rv.Type().String(), rv)
	}
}
func setUint(rv reflect.Value, u uint64) error {
	switch rv.Kind() {
	case reflect.Ptr:
		return setUint(reflect.Indirect(rv), u)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if rv.OverflowUint(u) {
			return fmt.Errorf("value %d does not fit into target of type %s", u, rv.Kind().String())
		}
		rv.SetUint(u)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if (u == 0xffffffffffffffff) || rv.OverflowInt(int64(u)) {
			return fmt.Errorf("value %d does not fit into target of type %s", u, rv.Kind().String())
		}
		rv.SetInt(int64(u))
		return nil
	case reflect.Interface:
		rv.Set(reflect.ValueOf(u))
		return nil
	default:
		return fmt.Errorf("cannot assign uint into Kind=%s Type=%#v %#v", rv.Kind().String(), rv.Type(), rv)
	}
}
func setInt(rv reflect.Value, i int64) error {
	switch rv.Kind() {
	case reflect.Ptr:
		return setInt(reflect.Indirect(rv), i)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if rv.OverflowInt(i) {
			return fmt.Errorf("value %d does not fit into target of type %s", i, rv.Kind().String())
		}
		rv.SetInt(i)
		return nil
	case reflect.Interface:
		rv.Set(reflect.ValueOf(i))
		return nil
	default:
		return fmt.Errorf("cannot assign int into Kind=%s Type=%#v %#v", rv.Kind().String(), rv.Type(), rv)
	}
}
func setFloat32(rv reflect.Value, f float32) error {
	switch rv.Kind() {
	case reflect.Ptr:
		return setFloat32(reflect.Indirect(rv), f)
	case reflect.Float32, reflect.Float64:
		rv.SetFloat(float64(f))
		return nil
	case reflect.Interface:
		rv.Set(reflect.ValueOf(f))
		return nil
	default:
		return fmt.Errorf("cannot assign float32 into Kind=%s Type=%#v %#v", rv.Kind().String(), rv.Type(), rv)
	}
}
func setFloat64(rv reflect.Value, d float64) error {
	switch rv.Kind() {
	case reflect.Ptr:
		return setFloat64(reflect.Indirect(rv), d)
	case reflect.Float32, reflect.Float64:
		rv.SetFloat(d)
		return nil
	case reflect.Interface:
		rv.Set(reflect.ValueOf(d))
		return nil
	default:
		return fmt.Errorf("cannot assign float64 into Kind=%s Type=%#v %#v", rv.Kind().String(), rv.Type(), rv)
	}
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
