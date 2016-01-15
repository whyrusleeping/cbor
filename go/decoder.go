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

	return dec.DecodeAny(newReflectValue(rv))
}

func (dec *Decoder) DecodeAny(v DecodeValue) error {
	var didread int
	var err error

	_, err = io.ReadFull(dec.rin, dec.c)

	if didread == 1 {
		/* log.Printf("got one %x\n", dec.c[0]) */
	}

	if err != nil {
		return err
	}

	if err := v.Prepare(); err != nil {
		return err
	}

	return dec.innerDecodeC(v, dec.c[0])
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

func (dec *Decoder) innerDecodeC(rv DecodeValue, c byte) error {
	cborType := c & typeMask
	cborInfo := c & infoBits

	aux, err := dec.handleInfoBits(cborInfo)
	if err != nil {
		log.Printf("error in handleInfoBits: %v", err)
		return err
	}
	//log.Printf("cborType %x cborInfo %d aux %x", cborType, cborInfo, aux)

	if cborType == cborUint {
		return rv.SetUint(aux)
	} else if cborType == cborNegint {
		if aux > 0x7fffffffffffffff {
			//return errors.New(fmt.Sprintf("cannot represent -%d", aux))
			bigU := &big.Int{}
			bigU.SetUint64(aux)
			minusOne := big.NewInt(-1)
			bn := &big.Int{}
			bn.Sub(minusOne, bigU)
			//log.Printf("built big negint: %v", bn)
			return rv.SetBignum(bn)
		}
		return rv.SetInt(-1 - int64(aux))
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
					return rv.SetBytes(out)
				} else {
					var subb []byte = nil
					if (subc[0] & typeMask) != cborBytes {
						return fmt.Errorf("sub of var bytes is type %x, wanted %x", subc[0], cborBytes)
					}
					err = dec.innerDecodeC(newReflectValue(reflect.ValueOf(&subb)), subc[0])
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
			return rv.SetBytes(val)
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
			return rv.SetBignum(bn)
		} else if aux == tagNegBignum {
			bn, err := dec.decodeBignum(ic[0])
			if err != nil {
				return err
			}
			minusOne := big.NewInt(-1)
			bnOut := &big.Int{}
			bnOut.Sub(minusOne, bn)
			return rv.SetBignum(bnOut)
		} else if aux == tagDecimal {
			log.Printf("TODO: directly read bytes into decimal")
		} else if aux == tagBigfloat {
			log.Printf("TODO: directly read bytes into bigfloat")
		} else {
			decoder := dec.TagDecoders[aux]
			var target interface{}
			var trv DecodeValue
			var err error

			trv, target, err = rv.CreateTag(aux, decoder)
			if err != nil {
				return err
			}

			err = dec.innerDecodeC(trv, ic[0])
			if err != nil {
				return err
			}

			return rv.SetTag(aux, trv, decoder, target)
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
			return rv.SetFloat64(val)
		} else if cborInfo == int32Follows {
			f := math.Float32frombits(uint32(aux))
			return rv.SetFloat32(f)
		} else if cborInfo == int64Follows {
			d := math.Float64frombits(aux)
			return rv.SetFloat64(d)
		} else if cborInfo == cborFalse {
			return rv.SetBool(false)
		} else if cborInfo == cborTrue {
			return rv.SetBool(true)
		} else if cborInfo == cborNull {
			return rv.SetNil()
		}
	}

	return err
}

func (dec *Decoder) decodeText(rv DecodeValue, cborInfo byte, aux uint64) error {
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
				return rv.SetString(joined)
			} else {
				var subtext interface{}
				err = dec.innerDecodeC(newReflectValue(reflect.ValueOf(&subtext)), subc[0])
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
		return rv.SetString(xs)
	}
	return errors.New("internal error in decodeText, shouldn't get here")
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

func (dec *Decoder) setMapKV(dvm DecodeValueMap, krv DecodeValue) error {
	var err error
	val, err := dvm.CreateMapValue(krv)
	if err != nil {
		var throwaway interface{}
		err = dec.Decode(&throwaway)
		if err != nil {
			return err
		}
		return nil
	}
	err = dec.DecodeAny(val)
	if err != nil {
		log.Printf("error decoding map val: T %T v %#v", val, val)
		return err
	}
	err = dvm.SetMap(krv, val)
	if err != nil {
		log.Printf("error setting value")
		return err
	}

	return nil
}

func (dec *Decoder) decodeMap(rv DecodeValue, cborInfo byte, aux uint64) error {
	//log.Print("decode map into   ", rv.Type().String())
	// dereferenced reflect value
	var dvm DecodeValueMap
	var err error

	dvm, err = rv.CreateMap()
	if err != nil {
		return err
	}

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
				krv, err := dvm.CreateMapKey()
				if err != nil {
					return err
				}
				//var val interface{}
				err = dec.innerDecodeC(krv, subc[0])
				if err != nil {
					log.Printf("error decoding map key V, %s", err)
					return err
				}

				err = dec.setMapKV(dvm, krv)
				if err != nil {
					return err
				}
			}
		}
	} else {
		var i uint64
		for i = 0; i < aux; i++ {
			//var key interface{}
			krv, err := dvm.CreateMapKey()
			if err != nil {
				return err
			}
			//var val interface{}
			//err = dec.Decode(&key)
			err = dec.DecodeAny(krv)
			if err != nil {
				log.Printf("error decoding map key #, %s", err)
				return err
			}
			err = dec.setMapKV(dvm, krv)
			if err != nil {
				return err
			}
		}
	}

	return dvm.EndMap()
}

func (dec *Decoder) decodeArray(rv DecodeValue, cborInfo byte, aux uint64) error {

	var err error
	var dva DecodeValueArray

	var makeLength int = 0
	if cborInfo == varFollows {
		// no special capacity to allocate the slice to
	} else {
		makeLength = int(aux)
	}

	dva, err = rv.CreateArray(makeLength)
	if err != nil {
		return err
	}

	if cborInfo == varFollows {
		//log.Printf("var array")
		subc := []byte{0}
		var idx uint64 = 0
		for true {
			_, err = io.ReadFull(dec.rin, subc)
			if err != nil {
				log.Printf("error reading next byte for var text")
				return err
			}
			if subc[0] == 0xff {
				// Done
				break
			}
			subrv, err := dva.GetArrayValue(idx)
			if err != nil {
				return err
			}
			err = dec.innerDecodeC(subrv, subc[0])
			if err != nil {
				log.Printf("error decoding array subob")
				return err
			}
			err = dva.AppendArray(subrv)
			if err != nil {
				return err
			}
			idx++
		}
	} else {
		var i uint64
		for i = 0; i < aux; i++ {
			subrv, err := dva.GetArrayValue(i)
			if err != nil {
				return err
			}
			err = dec.DecodeAny(subrv)
			if err != nil {
				log.Printf("error decoding array subob")
				return err
			}
			err = dva.AppendArray(subrv)
			if err != nil {
				return err
			}
		}
	}

	return dva.EndArray()
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
