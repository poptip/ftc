// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package scratch

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
)

const (
	packetTypeOpen    byte = '0'
	packetTypeClose   byte = '1'
	packetTypePing    byte = '2'
	packetTypePong    byte = '3'
	packetTypeMessage byte = '4'
	packetTypeUpgrade byte = '5'
	packetTypeNoop    byte = '6'
)

var packetTypeLookup = map[byte]struct{}{}

func init() {
	for _, typ := range []byte{
		packetTypeOpen,
		packetTypeClose,
		packetTypePing,
		packetTypePong,
		packetTypeMessage,
		packetTypeUpgrade,
		packetTypeNoop,
	} {
		packetTypeLookup[typ] = struct{}{}
	}
}

// A packet represents an underlying FTC packet.
type packet struct {
	typ  byte
	data []byte
}

// A packetDecoder reads and decodes FTC Packets from an input stream.
type packetDecoder struct {
	r io.Reader
}

// newPacketDecoder allocates and returns a new decoder that reads from r.
func newPacketDecoder(r io.Reader) *packetDecoder {
	return &packetDecoder{r: r}
}

// decode reads the next encoded packet from its input
// and stores it in the value pointed to by pkt.
func (dec *packetDecoder) decode(pkt *packet) error {
	// Get the packet type.
	var pktType [1]byte
	if _, err := io.ReadFull(dec.r, pktType[:]); err != nil {
		return err
	}
	if _, valid := packetTypeLookup[pktType[0]]; !valid {
		return fmt.Errorf("invalid packet type %q", pktType)
	}
	pkt.typ = pktType[0]
	// Read the rest of the packet.
	b, err := ioutil.ReadAll(dec.r)
	if err != nil {
		return fmt.Errorf("unable to read: %v", err)
	}
	pkt.data = b
	return nil
}

type writer interface {
	Flush() error
	io.ByteWriter
	io.Writer
}

// A packetEncoder writes FTC Packets to an output stream.
type packetEncoder struct {
	w   writer
	err error
}

func (e *packetEncoder) write(p []byte) {
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(p)
}

func (e *packetEncoder) flush() {
	if e.err != nil {
		return
	}
	e.err = e.w.Flush()
}

func (e *packetEncoder) writeByte(p byte) {
	if e.err != nil {
		return
	}
	e.err = e.w.WriteByte(p)
}

// newPacketEncoder allocates and returns a new encoder that writes to w.
func newPacketEncoder(w io.Writer) *packetEncoder {
	e := &packetEncoder{}
	if pw, ok := w.(writer); ok {
		e.w = pw
	} else {
		e.w = bufio.NewWriter(w)
	}
	return e
}

// encode writes the encoded packet to the stream.
func (e *packetEncoder) encode(p packet) error {
	// Write the type info.
	e.writeByte(p.typ)
	if p.data != nil {
		e.write(p.data)
	}
	e.flush()
	return e.err
}

// A payloadEncoder writes FTC Payloads to an output stream.
type payloadEncoder struct {
	w   writer
	err error
}

func (e *payloadEncoder) write(p []byte) {
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(p)
}

func (e *payloadEncoder) flush() {
	if e.err != nil {
		return
	}
	e.err = e.w.Flush()
}

func (e *payloadEncoder) writeByte(p byte) {
	if e.err != nil {
		return
	}
	e.err = e.w.WriteByte(p)
}

// newPayloadEncoder allocates and returns a PayloadEncoder that writes to w.
func newPayloadEncoder(w io.Writer) *payloadEncoder {
	e := &payloadEncoder{}
	if pw, ok := w.(writer); ok {
		e.w = pw
	} else {
		e.w = bufio.NewWriter(w)
	}
	return e
}

// encode writes the encoded packets as a payload to the stream.
func (e *payloadEncoder) encode(p []packet) error {
	// The bytes cannot be written directly to the underlying
	// writer because the size of each payload is required as
	// a prefix.
	var buf bytes.Buffer
	pEnc := newPacketEncoder(&buf)
	for _, pkt := range p {
		if err := pEnc.encode(pkt); err != nil {
			return err
		}

		e.write([]byte(strconv.Itoa(buf.Len())))
		e.writeByte(':')
		e.write(buf.Bytes())
		buf.Reset()
	}
	e.flush()
	return e.err
}

// A payloadDecoder reads and decodes FTC Payloads from an input stream.
type payloadDecoder struct {
	r io.Reader
}

// newPayloadDecoder allocates and returns a new decoder that reads from r.
func newPayloadDecoder(r io.Reader) *payloadDecoder {
	return &payloadDecoder{r: r}
}

// scanPacket is used as the split function by the Scanner within Decode.
func scanPacket(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, ':'); i >= 0 {
		size, err := strconv.Atoi(string(data[0:i]))
		if err != nil {
			return 0, nil, err
		}
		// Add 1 to account for delimiter.
		return i + 1 + size, data[i+1 : i+1+size], nil
	}
	// Request more data.
	return 0, nil, nil
}

// decode reads the next encoded payload from its input
// and stores it in the value pointed to by pkts.
//
// This method is not symmetrical with Encode, in that it
// does not take an arbitrary type and fill its value. The
// caller will always need the underlying packet type. This
// method overwrites any existing data within pkts.
func (dec *payloadDecoder) decode(pkts *[]packet) error {
	scanner := bufio.NewScanner(dec.r)
	scanner.Split(scanPacket)
	*pkts = []packet{}
	for i := 0; scanner.Scan(); i++ {
		var pkt packet
		r := bytes.NewReader(scanner.Bytes())
		if err := newPacketDecoder(r).decode(&pkt); err != nil {
			return err
		}
		*pkts = append(*pkts, pkt)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
