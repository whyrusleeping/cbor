package cbor

import (
	"fmt"
	"log"
	"math/big"
	"reflect"
)

type DecodeValue interface {
	// Before decoding, check if there is no error
	Prepare() error

	// Got binary string
	SetBytes(buf []byte) error

	// Got a number (different formats)
	SetBignum(x *big.Int) error
	SetUint(u uint64) error
	SetInt(i int64) error
	SetFloat32(f float32) error
	SetFloat64(d float64) error

	// Got null
	SetNil() error

	// Got boolean
	SetBool(b bool)

	// Got text string
	SetString(s string)

	// Got a Map (beginning)
	CreateMap() (DecodeValueMap, error)

	// Got an array (beginning)
	CreateArray(makeLength int) (DecodeValueArray, error)

	// Got a tag not managed by a TagDecoder
	TagValue(aux uint64) (DecodeValue, interface{})

	// Got a tag managed by a TagDecoder
	TagValueFrom(v interface{}) DecodeValue

	// Got the tag value (maybe transfoemed by TagDecoder.PostDecode)
	SetTag(v DecodeValue, i interface{})
}

type DecodeValueMap interface {
	// Got a map key
	CreateMapKey() DecodeValue

	// Got a map value
	CreateMapValue(key DecodeValue) (DecodeValue, bool)

	// Got a key / value pair
	SetMap(key, val DecodeValue) error

	// The map is at the end
	EndMap()
}

type DecodeValueArray interface {
	// Got an array item
	GetArrayValue() DecodeValue

	// After the array item is decoded
	AppendArray(value DecodeValue)

	// The array is at the end
	EndArray()
}

type reflectValue struct {
	v reflect.Value
}

func newReflectValue(rv reflect.Value) *reflectValue {
	return &reflectValue{rv}
}

func (r *reflectValue) Prepare() error {
	if (!r.v.CanSet()) && (r.v.Kind() != reflect.Ptr || r.v.IsNil()) {
		return &InvalidUnmarshalError{r.v.Type()}
	}
	return nil
}

func (r *reflectValue) SetBignum(x *big.Int) error {
	switch r.v.Kind() {
	case reflect.Ptr:
		return newReflectValue(reflect.Indirect(r.v)).SetBignum(x)
	case reflect.Interface:
		r.v.Set(reflect.ValueOf(*x))
		return nil
	case reflect.Int32:
		if x.BitLen() < 32 {
			r.v.SetInt(x.Int64())
			return nil
		} else {
			return fmt.Errorf("int too big for int32 target")
		}
	case reflect.Int, reflect.Int64:
		if x.BitLen() < 64 {
			r.v.SetInt(x.Int64())
			return nil
		} else {
			return fmt.Errorf("int too big for int64 target")
		}
	default:
		return fmt.Errorf("cannot assign bignum into Kind=%s Type=%s %#v", r.v.Kind().String(), r.v.Type().String(), r.v)
	}
}

func (r *reflectValue) SetBytes(buf []byte) error {
	switch r.v.Kind() {
	case reflect.Ptr:
		return newReflectValue(reflect.Indirect(r.v)).SetBytes(buf)
	case reflect.Interface:
		r.v.Set(reflect.ValueOf(buf))
		return nil
	case reflect.Slice:
		if r.v.Type().Elem().Kind() == reflect.Uint8 {
			r.v.SetBytes(buf)
			return nil
		} else {
			return fmt.Errorf("cannot write []byte to k=%s %s", r.v.Kind().String(), r.v.Type().String())
		}
	case reflect.String:
		r.v.Set(reflect.ValueOf(string(buf)))
		return nil
	default:
		return fmt.Errorf("cannot assign []byte into Kind=%s Type=%s %#v", r.v.Kind().String(), "" /*r.v.Type().String()*/, r.v)
	}
}

func (r *reflectValue) SetUint(u uint64) error {
	switch r.v.Kind() {
	case reflect.Ptr:
		if r.v.IsNil() {
			if r.v.CanSet() {
				r.v.Set(reflect.New(r.v.Type().Elem()))
				// fall through to set indirect below
			} else {
				return fmt.Errorf("trying to put uint into unsettable nil ptr")
			}
		}
		return newReflectValue(reflect.Indirect(r.v)).SetUint(u)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if r.v.OverflowUint(u) {
			return fmt.Errorf("value %d does not fit into target of type %s", u, r.v.Kind().String())
		}
		r.v.SetUint(u)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if (u == 0xffffffffffffffff) || r.v.OverflowInt(int64(u)) {
			return fmt.Errorf("value %d does not fit into target of type %s", u, r.v.Kind().String())
		}
		r.v.SetInt(int64(u))
		return nil
	case reflect.Interface:
		r.v.Set(reflect.ValueOf(u))
		return nil
	default:
		return fmt.Errorf("cannot assign uint into Kind=%s Type=%#v %#v", r.v.Kind().String(), r.v.Type(), r.v)
	}
}
func (r *reflectValue) SetInt(i int64) error {
	switch r.v.Kind() {
	case reflect.Ptr:
		return newReflectValue(reflect.Indirect(r.v)).SetInt(i)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if r.v.OverflowInt(i) {
			return fmt.Errorf("value %d does not fit into target of type %s", i, r.v.Kind().String())
		}
		r.v.SetInt(i)
		return nil
	case reflect.Interface:
		r.v.Set(reflect.ValueOf(i))
		return nil
	default:
		return fmt.Errorf("cannot assign int into Kind=%s Type=%#v %#v", r.v.Kind().String(), r.v.Type(), r.v)
	}
}
func (r *reflectValue) SetFloat32(f float32) error {
	switch r.v.Kind() {
	case reflect.Ptr:
		return newReflectValue(reflect.Indirect(r.v)).SetFloat32(f)
	case reflect.Float32, reflect.Float64:
		r.v.SetFloat(float64(f))
		return nil
	case reflect.Interface:
		r.v.Set(reflect.ValueOf(f))
		return nil
	default:
		return fmt.Errorf("cannot assign float32 into Kind=%s Type=%#v %#v", r.v.Kind().String(), r.v.Type(), r.v)
	}
}
func (r *reflectValue) SetFloat64(d float64) error {
	switch r.v.Kind() {
	case reflect.Ptr:
		return newReflectValue(reflect.Indirect(r.v)).SetFloat64(d)
	case reflect.Float32, reflect.Float64:
		r.v.SetFloat(d)
		return nil
	case reflect.Interface:
		r.v.Set(reflect.ValueOf(d))
		return nil
	default:
		return fmt.Errorf("cannot assign float64 into Kind=%s Type=%#v %#v", r.v.Kind().String(), r.v.Type(), r.v)
	}
}
func (r *reflectValue) SetNil() error {
	switch r.v.Kind() {
	case reflect.Ptr:
		//return setNil(reflect.Indirect(r.v))
		r.v.Set(reflect.Zero(r.v.Type()))
	case reflect.Interface:
		if r.v.IsNil() {
			// already nil, okay!
			return nil
		}
		r.v.Set(reflect.Zero(r.v.Type()))
	default:
		log.Printf("setNil wat %s", r.v.Type())
		r.v.Set(reflect.Zero(r.v.Type()))
	}
	return nil
}

func (r *reflectValue) SetBool(b bool) {
	reflect.Indirect(r.v).Set(reflect.ValueOf(b))
}

func (r *reflectValue) SetString(xs string) {
	// handle either concrete string or string* to nil
	deref := reflect.Indirect(r.v)
	if !deref.CanSet() {
		r.v.Set(reflect.ValueOf(&xs))
	} else {
		deref.Set(reflect.ValueOf(xs))
	}
}

func (r *reflectValue) CreateMap() (DecodeValueMap, error) {
	var drv reflect.Value
	if r.v.Kind() == reflect.Ptr {
		drv = reflect.Indirect(r.v)
	} else {
		drv = r.v
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
				return nil, fmt.Errorf("target map is nil and not settable")
			}
		}
		keyType = drv.Type().Key()
		ma = &mapReflectValue{drv}
	default:
		return nil, fmt.Errorf("can't read map into %s", r.v.Type().String())
	}

	return &reflectValueMap{drv, irv, ma, keyType}, nil
}

type reflectValueMap struct {
	drv     reflect.Value
	irv     reflect.Value
	ma      mapAssignable
	keyType reflect.Type
}

func (r *reflectValueMap) CreateMapKey() DecodeValue {
	return newReflectValue(reflect.New(r.keyType))
}

func (r *reflectValueMap) CreateMapValue(key DecodeValue) (DecodeValue, bool) {
	v, ok := r.ma.ReflectValueForKey(key.(*reflectValue).v.Interface())
	return newReflectValue(*v), ok
}

func (r *reflectValueMap) SetMap(key, val DecodeValue) error {
	return r.ma.SetReflectValueForKey(key.(*reflectValue).v.Interface(), val.(*reflectValue).v)
}

func (r *reflectValueMap) EndMap() {
	if r.drv.Kind() == reflect.Interface {
		r.drv.Set(r.irv)
	}
}

func (r *reflectValue) CreateArray(makeLength int) (DecodeValueArray, error) {
	var rv reflect.Value = r.v

	if rv.Kind() == reflect.Ptr {
		rv = reflect.Indirect(r.v)
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
		return nil, fmt.Errorf("can't read array into %s", rv.Type().String())
	}

	return &reflectValueArray{rv, makeLength, irv, elemType, 0}, nil
}

type reflectValueArray struct {
	rv         reflect.Value
	makeLength int
	irv        reflect.Value
	elemType   reflect.Type
	arrayPos   int
}

func (r *reflectValueArray) GetArrayValue() DecodeValue {
	if r.rv.Kind() == reflect.Array {
		return &reflectValue{r.rv.Index(r.arrayPos)}
	} else {
		return &reflectValue{reflect.New(r.elemType)}
	}
}

func (r *reflectValueArray) AppendArray(subrv DecodeValue) {
	if r.rv.Kind() == reflect.Array {
		r.arrayPos++
	} else {
		r.irv = reflect.Append(r.irv, reflect.Indirect(subrv.(*reflectValue).v))
	}
}

func (r *reflectValueArray) EndArray() {
	if r.rv.Kind() != reflect.Array {
		r.rv.Set(r.irv)
	}
}

func (r *reflectValue) TagValue(aux uint64) (DecodeValue, interface{}) {
	target := &CBORTag{}
	target.Tag = aux
	return newReflectValue(reflect.ValueOf(&target.WrappedObject)), target
}

func (r *reflectValue) TagValueFrom(val interface{}) DecodeValue {
	return newReflectValue(reflect.ValueOf(val))
}

func (r *reflectValue) SetTag(val DecodeValue, i interface{}) {
	reflect.Indirect(r.v).Set(reflect.ValueOf(i))
}
