package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"

	cbor "github.com/whyrusleeping/cbor/go"
)

func main() {
	in := flag.String("i", "-", "JSON file")
	out := flag.String("o", "-", "CBOR file")
	flag.Parse()

	var i io.ReadCloser
	var o io.WriteCloser
	var err error
	var object interface{}

	if *in == "-" {
		i = os.Stdin
	} else {
		i, err = os.Open(*in)
		if err != nil {
			log.Fatal(err)
		}
		defer i.Close()
	}

	if *out == "-" {
		o = os.Stdout
	} else {
		o, err = os.Open(*out)
		if err != nil {
			log.Fatal(err)
		}
		defer o.Close()
	}

	err = json.NewDecoder(i).Decode(&object)
	if err != nil {
		log.Fatal(err)
	}

	err = cbor.NewEncoder(o).Encode(object)
	if err != nil {
		log.Fatal(err)
	}
}
