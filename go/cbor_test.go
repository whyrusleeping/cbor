package cbor

import "bytes"
import "encoding/base64"
import "encoding/json"
import "fmt"
import "math"
import "math/big"
import "os"
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
	default:
		t.Errorf("unexpected cbor type %T", cborv)
		return fmt.Sprintf("%v", jsonv) == fmt.Sprintf("%v", cborv)
		//return jsonv == cborv
	}
}


func TestDecodeInt(t *testing.T) {
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
				t.Logf("test[%d] %#v", i, testv)
				t.Logf("decoding [%d] %#v ...\n", i, testv.Cbor)
				t.Errorf("json %T %#v != cbor %T %#v", testv.Decoded, testv.Decoded, cborObject, cborObject)
			}
		}
	}
}
