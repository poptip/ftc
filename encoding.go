// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"
)

const (
	packetTypeOpen    = "0"
	packetTypeClose   = "1"
	packetTypePing    = "2"
	packetTypePong    = "3"
	packetTypeMessage = "4"
	packetTypeUpgrade = "5"
	packetTypeNoop    = "6"
)

// A packet is a single unit of data to be sent or received.
// Usually, they are encompassed in payload objects.
type packet struct {
	typ  string      `json:"type"`
	data interface{} `json:"data"`
}

// Type returns the packet type.
func (p *packet) Type() string {
	return p.typ
}

// Data returns the packet data.
func (p *packet) Data() interface{} {
	return p.data
}

// MarshalText encodes the packet into UTF-8-encoded text and returns the result.
func (p *packet) MarshalText() ([]byte, error) {
	if p.data == nil {
		return []byte(p.typ), nil
	}
	switch t := p.data.(type) {
	case string:
		return []byte(p.typ + t), nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("could not marshal value %v of type %T: %v", t, t, err)
		}
		return []byte(p.typ + string(b)), nil
	}
}

// UnmarshalText must be able to decode the form generated by MarshalText.
// UnmarshalText must copy the text if it wishes to retain the text after returning.
func (p *packet) UnmarshalText(text []byte) error {
	s := string(text)
	for _, typ := range []string{
		packetTypeOpen,
		packetTypeClose,
		packetTypePing,
		packetTypePong,
		packetTypeMessage,
		packetTypeUpgrade,
		packetTypeNoop,
	} {
		if strings.HasPrefix(s, typ) {
			*p = packet{typ: typ, data: strings.TrimPrefix(s, typ)}
			return nil
		}
	}
	return fmt.Errorf("invalid packet type for %q", s)
}

func newPacket(typ string, data interface{}) *packet {
	return &packet{typ: typ, data: data}
}

// A payload is a series of encoded packets.
type payload []*packet

// MarshalText encodes the payload into UTF-8-encoded text and returns the result.
func (p *payload) MarshalText() ([]byte, error) {
	str := ""
	for _, pkt := range *p {
		// TODO: JS uses utf-16 and go uses utf-8. Account for the length disparity.
		b, err := pkt.MarshalText()
		if err != nil {
			glog.Errorf("encoding: could not marshal packet %+v: %s", pkt, err)
			break
		}
		str += strconv.Itoa(len(b)) + ":" + string(b)
	}
	return []byte(str), nil
}

// UnmarshalText must be able to decode the form generated by MarshalText.
// UnmarshalText must copy the text if it wishes to retain the text after returning.
func (p *payload) UnmarshalText(text []byte) error {
	s := string(text)
	for i := strings.Index(s, ":"); i != -1; {
		var l int
		l, _ = strconv.Atoi(s[l:i])
		var pkt packet
		i++ // Skip over the semicolon.
		if err := pkt.UnmarshalText([]byte(s[i : i+l])); err != nil {
			return err
		}
		if p == nil {
			*p = make(payload, 1)
		}
		*p = append(*p, &pkt)
		s = s[i+l:]
		i = strings.Index(s, ":")
	}
	return nil
}