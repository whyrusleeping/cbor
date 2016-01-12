package cbor

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"reflect"
	"strings"
)

type TagDecoder interface {
	// Handle things which match this.
	//
	// Setup like this:
	// var dec Decoder
	// var myTagDec TagDecoder
	// dec.TagDecoders[myTagDec.GetTag()] = myTagDec
	GetTag() uint64

	// Sub-object will be decoded onto the returned object.
	DecodeTarget() interface{}

	// Run after decode onto DecodeTarget has happened.
	// The return value from this is returned in place of the
	// raw decoded object.
	PostDecode(interface{}) (interface{}, error)
}

type Decoder struct {
	rin io.Reader

	// tag byte
	c []byte

	// many values fit within the next 8 bytes
	b8 []byte

	// Extra processing for CBOR TAG objects.
	TagDecoders map[uint64]TagDecoder
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		rin:         r,
		c:           make([]byte, 1),
		b8:          make([]byte, 8),
		TagDecoders: make(map[uint64]TagDecoder),
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

	if cborInfo <= 23 {
		aux = uint64(cborInfo)
		return aux, nil
	} else if cborInfo == int8Follows {
		didread, err := io.ReadFull(dec.rin, dec.b8[:1])
		if didread == 1 {
			aux = uint64(dec.b8[0])
		}
		return aux, err
	} else if cborInfo == int16Follows {
		didread, err := io.ReadFull(dec.rin, dec.b8[:2])
		if didread == 2 {
			aux = (uint64(dec.b8[0]) << 8) | uint64(dec.b8[1])
		}
		return aux, err
	} else if cborInfo == int32Follows {
		didread, err := io.ReadFull(dec.rin, dec.b8[:4])
		if didread == 4 {
			aux = (uint64(dec.b8[0]) << 24) |
				(uint64(dec.b8[1]) << 16) |
				(uint64(dec.b8[2]) << 8) |
				uint64(dec.b8[3])
		}
		return aux, err
	} else if cborInfo == int64Follows {
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
		return setInt(rv, -1-int64(aux))
	} else if cborType == cborBytes {
		//log.Printf("cborType %x bytes cborInfo %d aux %x", cborType, cborInfo, aux)
		if cborInfo == varFollows {
			parts := make([][]byte, 0, 1)
			allsize := 0
			subc := []byte{0}
			for true {
				_, err = io.ReadFull(dec.rin, subc)
				if err != nil {
					log.Printf("error reading next byte for bar bytes")
					return err
				}
				if subc[0] == 0xff {
					// done
					var out []byte = nil
					if len(parts) == 0 {
						out = make([]byte, 0)
					} else {
						pos := 0
						out = make([]byte, allsize)
						for _, p := range parts {
							pos += copy(out[pos:], p)
						}
					}
					return setBytes(rv, out)
				} else {
					var subb []byte = nil
					if (subc[0] & typeMask) != cborBytes {
						return fmt.Errorf("sub of var bytes is type %x, wanted %x", subc[0], cborBytes)
					}
					err = dec.innerDecodeC(reflect.ValueOf(&subb), subc[0])
					if err != nil {
						log.Printf("error decoding sub bytes")
						return err
					}
					allsize += len(subb)
					parts = append(parts, subb)
				}
			}
		} else {
			val := make([]byte, aux)
			_, err = io.ReadFull(dec.rin, val)
			if err != nil {
				return err
			}
			// Don't really care about count, ReadFull will make it all or none and we can just fall out with whatever error
			return setBytes(rv, val)
			/*if (rv.Kind() == reflect.Slice) && (rv.Type().Elem().Kind() == reflect.Uint8) {
				rv.SetBytes(val)
			} else {
				return fmt.Errorf("cannot write []byte to k=%s %s", rv.Kind().String(), rv.Type().String())
			}*/
		}
	} else if cborType == cborText {
		return dec.decodeText(rv, cborInfo, aux)
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
			decoder, ok := dec.TagDecoders[aux]
			if ok {
				target := decoder.DecodeTarget()
				trv := reflect.ValueOf(target)
				err = dec.innerDecodeC(trv, ic[0])
				if err != nil {
					return err
				}
				target, err = decoder.PostDecode(target)
				if err != nil {
					return err
				}
				reflect.Indirect(rv).Set(reflect.ValueOf(target))
				return nil
			} else {
				target := CBORTag{}
				target.Tag = aux
				err = dec.innerDecodeC(reflect.ValueOf(&target.WrappedObject), ic[0])
				if err != nil {
					return err
				}
				reflect.Indirect(rv).Set(reflect.ValueOf(target))
				return nil
			}
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
				val = math.Ldexp(float64(mant+1024), int(exp-25))
			} else if mant == 0 {
				val = math.Inf(1)
			} else {
				val = math.NaN()
			}
			if (aux & 0x08000) != 0 {
				val = -val
			}
			return setFloat64(rv, val)
		} else if cborInfo == int32Follows {
			f := math.Float32frombits(uint32(aux))
			return setFloat32(rv, f)
		} else if cborInfo == int64Follows {
			d := math.Float64frombits(aux)
			return setFloat64(rv, d)
		} else if cborInfo == cborFalse {
			reflect.Indirect(rv).Set(reflect.ValueOf(false))
		} else if cborInfo == cborTrue {
			reflect.Indirect(rv).Set(reflect.ValueOf(true))
		} else if cborInfo == cborNull {
			return setNil(rv)
		}
	}

	return err
}

func (dec *Decoder) decodeText(rv reflect.Value, cborInfo byte, aux uint64) error {
	var err error
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
				dtStringSet(rv, joined)
				//reflect.Indirect(rv).Set(reflect.ValueOf(joined))
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
		xs := string(raw)
		dtStringSet(rv, xs)
		return nil
	}
	return errors.New("internal error in decodeText, shouldn't get here")
}
func dtStringSet(rv reflect.Value, xs string) {
	// handle either concrete string or string* to nil
	deref := reflect.Indirect(rv)
	if !deref.CanSet() {
		rv.Set(reflect.ValueOf(&xs))
	} else {
		deref.Set(reflect.ValueOf(xs))
	}
}

type mapAssignable interface {
	ReflectValueForKey(key interface{}) (*reflect.Value, bool)
	SetReflectValueForKey(key interface{}, value reflect.Value) error
}

type mapReflectValue struct {
	reflect.Value
}

func (irv *mapReflectValue) ReflectValueForKey(key interface{}) (*reflect.Value, bool) {
	//var x interface{}
	//rv := reflect.ValueOf(&x)
	rv := reflect.New(irv.Type().Elem())
	return &rv, true
}
func (irv *mapReflectValue) SetReflectValueForKey(key interface{}, value reflect.Value) error {
	//log.Printf("k T %T v%#v, v T %s v %#v", key, key, value.Type().String(), value.Interface())
	krv := reflect.Indirect(reflect.ValueOf(key))
	vrv := reflect.Indirect(value)
	//log.Printf("irv T %s v %#v", irv.Type().String(), irv.Interface())
	//log.Printf("k T %s v %#v, v T %s v %#v", krv.Type().String(), krv.Interface(), vrv.Type().String(), vrv.Interface())
	if krv.Kind() == reflect.Interface {
		krv = krv.Elem()
		//log.Printf("ke T %s v %#v", krv.Type().String(), krv.Interface())
	}
	if (krv.Kind() == reflect.Slice) || (krv.Kind() == reflect.Array) {
		//log.Printf("key is slice or array")
		if krv.Type().Elem().Kind() == reflect.Uint8 {
			//log.Printf("key is []uint8")
			ks := string(krv.Bytes())
			krv = reflect.ValueOf(ks)
		}
	}
	irv.SetMapIndex(krv, vrv)
	return nil
}

type structAssigner struct {
	Srv reflect.Value

	//keyType reflect.Type
}

func (sa *structAssigner) ReflectValueForKey(key interface{}) (*reflect.Value, bool) {
	var skey string
	switch tkey := key.(type) {
	case string:
		skey = tkey
	case *string:
		skey = *tkey
	default:
		log.Printf("rvfk key is not string, got %T", key)
		return nil, false
	}

	ft := sa.Srv.Type()
	numFields := ft.NumField()
	for i := 0; i < numFields; i++ {
		sf := ft.Field(i)
		fieldname, ok := fieldname(sf)
		if !ok {
			continue
		}
		if (fieldname == skey) || strings.EqualFold(fieldname, skey) {
			fieldVal := sa.Srv.FieldByName(sf.Name)
			if !fieldVal.CanSet() {
				log.Printf("cannot set field %s for key %s", sf.Name, skey)
				return nil, false
			}
			return &fieldVal, true
		}
	}
	return nil, false
}
func (sa *structAssigner) SetReflectValueForKey(key interface{}, value reflect.Value) error {
	return nil
}

func (dec *Decoder) setMapKV(krv reflect.Value, ma mapAssignable) error {
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
	//log.Print("decode map into   ", rv.Type().String())
	// dereferenced reflect value
	var drv reflect.Value
	if rv.Kind() == reflect.Ptr {
		drv = reflect.Indirect(rv)
	} else {
		drv = rv
	}
	//log.Print("decode map into d ", drv.Type().String())

	// inner reflect value
	var irv reflect.Value
	var ma mapAssignable

	var keyType reflect.Type

	switch drv.Kind() {
	case reflect.Interface:
		//log.Print("decode map into interface ", drv.Type().String())
		// TODO: maybe I should make this map[string]interface{}
		nob := make(map[interface{}]interface{})
		irv = reflect.ValueOf(nob)
		ma = &mapReflectValue{irv}
		keyType = irv.Type().Key()
	case reflect.Struct:
		//log.Print("decode map into struct ", drv.Type().String())
		ma = &structAssigner{drv}
		keyType = reflect.TypeOf("")
	case reflect.Map:
		//log.Print("decode map into map ", drv.Type().String())
		if drv.IsNil() {
			if drv.CanSet() {
				drv.Set(reflect.MakeMap(drv.Type()))
			} else {
				return fmt.Errorf("target map is nil and not settable")
			}
		}
		keyType = drv.Type().Key()
		ma = &mapReflectValue{drv}
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
					log.Printf("error decoding map key V, %s", err)
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
				log.Printf("error decoding map key #, %s", err)
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
	var elemType reflect.Type

	switch rv.Kind() {
	case reflect.Interface:
		// make a slice
		nob := make([]interface{}, 0, makeLength)
		irv = reflect.ValueOf(nob)
		elemType = irv.Type().Elem()
	case reflect.Slice:
		// we have a slice
		irv = rv
		elemType = irv.Type().Elem()
	case reflect.Array:
		// no irv, no elemType
	default:
		return fmt.Errorf("can't read array into %s", rv.Type().String())
	}

	var err error

	if cborInfo == varFollows {
		var arrayPos int = 0
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
			} else if rv.Kind() == reflect.Array {
				err := dec.innerDecodeC(rv.Index(arrayPos), subc[0])
				if err != nil {
					log.Printf("error decoding array subob")
					return err
				}
				arrayPos++
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
			if rv.Kind() == reflect.Array {
				err := dec.reflectDecode(rv.Index(int(i)))
				if err != nil {
					log.Printf("error decoding array subob")
					return err
				}
			} else {
				subrv := reflect.New(elemType)
				err := dec.reflectDecode(subrv)
				if err != nil {
					log.Printf("error decoding array subob")
					return err
				}
				irv = reflect.Append(irv, reflect.Indirect(subrv))
			}
		}
	}

	if rv.Kind() != reflect.Array {
		rv.Set(irv)
	}

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
	for _, bv := range rawbytes {
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

func setBytes(rv reflect.Value, buf []byte) error {
	switch rv.Kind() {
	case reflect.Ptr:
		return setBytes(reflect.Indirect(rv), buf)
	case reflect.Interface:
		rv.Set(reflect.ValueOf(buf))
		return nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			rv.SetBytes(buf)
			return nil
		} else {
			return fmt.Errorf("cannot write []byte to k=%s %s", rv.Kind().String(), rv.Type().String())
		}
	case reflect.String:
		rv.Set(reflect.ValueOf(string(buf)))
		return nil
	default:
		return fmt.Errorf("cannot assign []byte into Kind=%s Type=%s %#v", rv.Kind().String(), "" /*rv.Type().String()*/, rv)
	}
}

func setUint(rv reflect.Value, u uint64) error {
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			if rv.CanSet() {
				rv.Set(reflect.New(rv.Type().Elem()))
				// fall through to set indirect below
			} else {
				return fmt.Errorf("trying to put uint into unsettable nil ptr")
			}
		}
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
func setNil(rv reflect.Value) error {
	switch rv.Kind() {
	case reflect.Ptr:
		//return setNil(reflect.Indirect(rv))
		rv.Set(reflect.Zero(rv.Type()))
	case reflect.Interface:
		if rv.IsNil() {
			// already nil, okay!
			return nil
		}
		rv.Set(reflect.Zero(rv.Type()))
	default:
		log.Printf("setNil wat %s", rv.Type())
		rv.Set(reflect.Zero(rv.Type()))
	}
	return nil
}
