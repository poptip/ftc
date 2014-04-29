// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import "testing"

func TestEncodeDecodePacket(t *testing.T) {
	p := packet{}
	expected := "Foo © bar 𝌆 baz ☃ qux"
	text, err := newPacket(packetTypeMessage, expected).MarshalText()
	if err != nil {
		t.Fatalf("unable to marshal packet: %s", err)
	}
	err = p.UnmarshalText(text)
	if err != nil {
		t.Fatalf("unable to umarshal packet with text %q: %s", text, err)
	}
	if p.data.(string) != expected {
		t.Errorf("marshaled data %v does not match original text %q", p.data, expected)
	}
}

func TestEncodeDecodePayload(t *testing.T) {
	p := payload{
		newPacket(packetTypeOpen, nil),
		newPacket(packetTypeMessage, "Foo © bar 𝌆 baz ☃ qux"),
		newPacket(packetTypePing, "Foo © bar"),
		newPacket(packetTypeUpgrade, nil),
		newPacket(packetTypeClose, nil),
	}
	text, err := p.MarshalText()
	if err != nil {
		t.Fatalf("unable to marshal payload: %s", err)
	}
	var newP payload
	err = newP.UnmarshalText(text)
	if err != nil {
		t.Fatalf("unable to umarshal payload with text %q: %s", text, err)
	}
	for i, pkt := range newP {
		if p[i].type_ != pkt.type_ {
			t.Errorf("packet at index %d expected type %s, but got %s", i, p[i], pkt.type_)
		}
		if p[i].data != nil {
			if pkt.data == nil {
				t.Errorf("packet at index %d is nil, expected %+v", i, p[i].data)
			}
			// For now, data is only strings.
			s1, s2 := p[i].data.(string), pkt.data.(string)
			if s1 != s2 {
				t.Errorf("packet at index %d expected data %s, got %s", i, s1, s2)
			}
		}
	}
}
