package cbor

import "bytes"
import "encoding/base64"
import "encoding/json"
import "fmt"
import "log"
import "math"
import "math/big"
import "os"
import "reflect"
import "strings"
import "testing"

type testVector struct {
	Cbor string
	Hex string
	Roundtrip bool
	Decoded interface{}
	Diagnostic string
}

var errpath string = "../test-vectors/appendix_a.json"

func readVectors(t *testing.T) ([]testVector, error) {
	fin, err := os.Open(errpath)
	if err != nil {
		t.Error("could not open test vectors at: ", errpath)
		return nil, err
	}
	jd := json.NewDecoder(fin)
	jd.UseNumber()
	they := new([]testVector)
	err = jd.Decode(they)
	return *they, err
}


func jeq(jsonv, cborv interface{}, t *testing.T) bool {
	switch i := cborv.(type) {
	case uint64:
		switch jv := jsonv.(type) {
		case uint64:
			return i == jv;
		case float64:
			return math.Abs(float64(i) - jv) < math.Max(math.Abs(jv / 1000000000.0), 1.0/1000000000.0);
		case json.Number:
			return jv.String() == fmt.Sprintf("%d", i)
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case big.Int:
		switch jv := jsonv.(type) {
		case json.Number:
			return jv.String() == i.String()
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case int64:
		switch jv := jsonv.(type) {
		case json.Number:
			return jv.String() == fmt.Sprintf("%d", i)
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case float32:
		switch jv := jsonv.(type) {
		case json.Number:
			//return jv.String() == fmt.Sprintf("%f", i)
			fv, err := jv.Float64()
			if err != nil {
				t.Errorf("error getting json float: %s", err)
				return false;
			}
			return fv == float64(i)
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case float64:
		switch jv := jsonv.(type) {
		case json.Number:
			//return jv.String() == fmt.Sprintf("%f", i)
			fv, err := jv.Float64()
			if err != nil {
				t.Errorf("error getting json float: %s", err)
				return false;
			}
			return fv == i
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case bool:
		switch jv := jsonv.(type) {
		case bool:
			return jv == i
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case string:
		switch jv := jsonv.(type) {
		case string:
			return jv == i
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case []interface{}:
		switch jv := jsonv.(type) {
		case []interface{}:
			if len(i) != len(jv) {
				return false
			}
			for cai, cav := range(i) {
				if !jeq(jv[cai], cav, t) {
					t.Errorf("array mismatch at [%d]: json=%#v cbor=%#v", cai, jv[cai], cav)
					return false
				}
/*
				if fmt.Sprintf("%v", cav) != fmt.Sprintf("%v", jv[cai]) {
					return false
				}
*/
			}
			return true
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jsonv);
			return false
		}
	case nil:
		switch jv := jsonv.(type) {
		case nil:
			return true;
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jv);
			return false
		}
	case map[interface{}]interface{}:
		switch jv := jsonv.(type) {
		case map[string]interface{}:
			for jmk, jmv := range(jv) {
				cmv, ok := i[jmk]
				if !ok {
					t.Errorf("json key %v missing from cbor", jmk)
					return false
				}
				if !jeq(jmv, cmv, t) {
					t.Errorf("map key=%#v values: json=%#v cbor=%#v", jmk, jmv, cmv)
					return false
				}
/*
				if !reflect.DeepEqual(cmv, jmv) {
					t.Errorf("map key=%#v values: json=%#v cbor=%#v", jmk, jmv, cmv)
					return false
				}
*/
			}
			return true
		default:
			t.Errorf("wat types cbor %T json %T", cborv, jv);
			return false
		}
	default:
		eq := reflect.DeepEqual(jsonv, cborv)
		if ! eq {
			log.Printf("unexpected cbor type %T = %#v", cborv, cborv)
			t.Errorf("unexpected cbor type %T = %#v", cborv, cborv)
		}
		return eq
		//return fmt.Sprintf("%v", jsonv) == fmt.Sprintf("%v", cborv)
		//return jsonv == cborv
	}
}


func TestDecodeVectors(t *testing.T) {
	t.Parallel()
	they, err := readVectors(t)
	if err != nil {
		t.Fatal("could not load test vectors:", err)
		return
	}
	t.Logf("got %d test vectors", len(they))
	if len(they) <= 0 {
		t.Fatal("got no test vectors")
		return
	}
	for i, testv := range(they) {
		if testv.Decoded != nil && len(testv.Cbor) > 0 {
			//log.Printf("hex %s", testv.Hex)
			t.Logf("hex %s", testv.Hex)
			bin, err := base64.StdEncoding.DecodeString(testv.Cbor)
			if err != nil {
				t.Logf("test[%d] %#v", i, testv)
				t.Logf("decoding [%d] %#v ...\n", i, testv.Cbor)
				t.Fatal("could not decode test vector b64")
				return
			}
			ring := NewDecoder(bytes.NewReader(bin))
			var cborObject interface{}
			err = ring.Decode(&cborObject)
			if err != nil {
				t.Logf("test[%d] %#v", i, testv)
				t.Logf("decoding [%d] %#v ...\n", i, testv.Cbor)
				t.Fatalf("error decoding cbor: %v", err)
				return
			}
			if !jeq(testv.Decoded, cborObject, t) {
				//t.Logf("test[%d] %#v", i, testv)
				t.Logf("decoding [%d] %#v ...\n", i, testv.Cbor)
				t.Errorf("json %T %#v != cbor %T %#v", testv.Decoded, testv.Decoded, cborObject, cborObject)
				t.Logf("------")
			}
		}
	}
}


type RefTestOb struct {
	AString string
	BInt int
	CUint uint64
	DFloat float64
	EIntArray []int
	FStrIntMap map[string]int
	GBool bool
}


func checkObOne(ob RefTestOb, t *testing.T) bool {
	ok := true
	if ob.AString != "astring val" {
		t.Errorf("AString wanted \"astring val\" but got %#v", ob.AString)
		ok = false
	}
	if ob.BInt != -33 {
		t.Errorf("BInt wanted -33 but got %#v", ob.BInt)
		ok = false
	}
	if ob.CUint != 42 {
		t.Errorf("CUint wanted 42 but got %#v", ob.CUint)
		ok = false
	}
	if ob.DFloat != 0.25 {
		t.Errorf("DFloat wanted 02.5 but got %#v", ob.DFloat)
		ok = false
	}
	return ok
}


const (
	reflexObOneJson = "{\"astring\": \"astring val\", \"bint\": -33, \"cuint\": 42, \"dfloat\": 0.25, \"eintarray\": [1,2,3], \"fstrintmap\":{\"a\":13, \"b\":14}, \"gbool\": false}"
	reflexObOneCborB64 = "p2dhc3RyaW5na2FzdHJpbmcgdmFsamZzdHJpbnRtYXCiYWENYWIOZWdib29s9GRiaW50OCBpZWludGFycmF5gwECA2VjdWludBgqZmRmbG9hdPs/0AAAAAAAAA=="
)

/*
#python
import json
import cbor
import base64
# copy in the above json string literal here:
jsonstr = 
print base64.b64encode(cbor.dumps(json.loads(jsonstr)))
*/

func TestDecodeReflectivelyOne(t *testing.T) {
	t.Parallel()

	var err error
	
	jd := json.NewDecoder(strings.NewReader(reflexObOneJson))
	jd.UseNumber()
	they := RefTestOb{}
	err = jd.Decode(&they)
	if err != nil {
		t.Fatal("could not decode json", err)
		return
	}

	t.Log("check json")
	if !checkObOne(they, t) {
		return
	}

	bin, err := base64.StdEncoding.DecodeString(reflexObOneCborB64)
	if err != nil {
		t.Fatal("error decoding cbor b64", err)
		return
	}
	ring := NewDecoder(bytes.NewReader(bin))
	cob := RefTestOb{}
	err = ring.Decode(&cob)
	if err != nil {
		t.Fatal("error decoding cbor", err)
		return
	}
	t.Log("check cbor")
	if !checkObOne(they, t) {
		return
	}
}
