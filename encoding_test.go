// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"bytes"
	"io/ioutil"
	"log"
	"strings"
	"testing"
)

func TestPacketEncodeDecode(t *testing.T) {
	testCases := []struct {
		typ    byte
		data   []byte
		output string
	}{
		{packetTypeNoop, nil, "6"},
		{packetTypeMessage, []byte("Foo 世 bar baz 界 qux"), "4Foo 世 bar baz 界 qux"},
		{packetTypePing, []byte("Foo 世 bar baz"), "2Foo 世 bar baz"},
		{packetTypeOpen, []byte("{\"Val\":\"Foo 世 bar baz 界 qux\"}\n"), "0{\"Val\":\"Foo 世 bar baz 界 qux\"}\n"},
	}
	for _, testCase := range testCases {
		pkt := packet{typ: testCase.typ, data: testCase.data}
		var buf bytes.Buffer
		if err := newPacketEncoder(&buf).encode(pkt); err != nil {
			t.Errorf("could not encode packet %+v: %v", pkt, err)
		}
		if buf.String() != testCase.output {
			t.Errorf("output mismatch. expected %q, got %q", testCase.output, buf.String())
		}
		var newPkt packet
		if err := newPacketDecoder(&buf).decode(&newPkt); err != nil {
			t.Errorf("could not decode: %v", err)
		}
		if newPkt.typ != pkt.typ {
			t.Errorf("packet type mismatch. expected %q, got %q", pkt.typ, newPkt.typ)
		}
		if !bytes.Equal(pkt.data, newPkt.data) {
			t.Errorf("packet data mismatch. expected %q, got %q", pkt.data, newPkt.data)
		}
	}
	var buf bytes.Buffer
	var str string
	for i := 0; i < 256; i++ {
		str += "Foo 世 bar baz 界 qux"
	}
	if err := newPacketEncoder(&buf).encode(packet{typ: packetTypeMessage, data: []byte(str)}); err != nil {
		t.Errorf("could not encode string %q: %v", str, err)
	}
	expected := "4" + str
	if buf.String() != expected {
		t.Errorf("output mismatch. expected %q, got %q", expected, buf.String())
	}
	var pkt packet
	if err := newPacketDecoder(&buf).decode(&pkt); err != nil {
		t.Errorf("could not decode: %v", err)
	}
	if pkt.typ != packetTypeMessage {
		t.Errorf("packet type mismatch. expected %q, got %q", packetTypeMessage, pkt.typ)
	}
	if !bytes.Equal(pkt.data, []byte(str)) {
		t.Errorf("packet data mismatch. expected %q, got %q", str, pkt.data)
	}
	if buf.Len() != 0 {
		t.Fatalf("buffer should be empty but has len %d", buf.Len())
	}
}

func TestPayloadEncodeDecode(t *testing.T) {
	p := []packet{
		packet{typ: packetTypeOpen, data: []byte("{\"Val\":\"Foo 世 bar baz 界 qux\"}\n")},
		packet{typ: packetTypeMessage, data: []byte("Foo 世 bar baz")},
		packet{typ: packetTypePing, data: []byte("Foo 世 bar")},
		packet{typ: packetTypeUpgrade, data: nil},
		packet{typ: packetTypeClose, data: nil},
	}
	var buf bytes.Buffer
	if err := newPayloadEncoder(&buf).encode(p); err != nil {
		t.Fatalf("could not encode payload: %v", err)
	}
	var pkts []packet
	if err := newPayloadDecoder(&buf).decode(&pkts); err != nil {
		t.Errorf("could not decode payload: %v", err)
	}
	for i, pkt := range p {
		if pkt.typ != pkts[i].typ {
			t.Errorf("packet type mismatch. expected %q, got %q", pkt.typ, pkts[i].typ)
		}
		if !bytes.Equal(pkt.data, pkts[i].data) {
			t.Errorf("packet data mismatch. expected %q, got %q", pkt.data, pkts[i].data)
		}
	}
	log.Println(buf.String())
}

func BenchmarkPacketEncode(b *testing.B) {
	b.StopTimer()
	enc := newPacketEncoder(ioutil.Discard)
	p := packet{typ: packetTypeMessage, data: []byte("Foo 世 bar baz 界 qux")}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		enc.encode(p)
	}
}

func BenchmarkPacketDecode(b *testing.B) {
	b.StopTimer()
	dec := newPacketDecoder(strings.NewReader("4{\"Val\":\"Foo 世 bar baz 界 qux\"}\n"))
	var p packet
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		dec.decode(&p)
	}
}
