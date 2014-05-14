// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"sync"

	"code.google.com/p/go.net/websocket"
	"github.com/dustin/randbo"
	"github.com/golang/glog"
)

const (
	readyStateOpening = "opening"
	readyStateOpen    = "open"
	readyStateClosing = "closing"
	readyStateClosed  = "closed"

	messageProbe = "probe"
)

type Opener interface {
	// open sends an "open" packet to w.
	open(io.Writer) error
}

type Sender interface {
	Send(v interface{}) error
}

type Receiver interface {
	Receive(v interface{}) error
}

type Conn interface {
	Opener
	Sender
	Receiver
	io.ReadWriteCloser
	json.Marshaler
	json.Unmarshaler
}

// newID returns a pseudo-random, URL-encoded, base64
// string used for connection identifiers.
func newID() string {
	buf := make([]byte, 15)
	n, err := randbo.New().Read(buf)
	if err != nil {
		glog.Fatal(err)
	}
	if n != len(buf) {
		glog.Fatal("short read")
	}
	return base64.URLEncoding.EncodeToString(buf)
}

// A conn represents an FTC connection.
type conn struct {
	wsMu sync.RWMutex
	ws   *websocket.Conn

	id           string
	upgrades     []string
	pingInterval int
	pingTimeout  int

	stateMu sync.RWMutex
	state   string

	transport string
	upgraded  bool
	send      chan payload
	messages  chan interface{}
}

// MarshalJSON encodes the connection as a JSON object.
func (c *conn) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"sid":          c.id,
		"upgrades":     getValidUpgrades(),
		"pingInterval": c.pingInterval,
		"pingTimeout":  c.pingTimeout,
	})
}

// MarshalJSON decodes a JSON byte array into the connection receiver.
func (c *conn) UnmarshalJSON(data []byte) error {
	s := struct {
		SID          string
		Upgrades     []string
		PingInterval int
		PingTimeout  int
	}{}
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	c.id = s.SID
	c.upgrades = s.Upgrades
	c.pingTimeout = s.PingTimeout
	c.pingInterval = s.PingInterval
	return nil
}

// Receive receives a single message from the connection into v.
func (c *conn) Receive(v interface{}) error {
	if c == nil {
		return errors.New("attempt to receive on a nil connection")
	}
	if c.readyState() == readyStateClosing || c.readyState() == readyStateClosed {
		return errors.New("attempt to receive on a closed connection")
	}
	msg := <-c.messages
	// TODO(andybons): Error checking for proper values.
	reflect.ValueOf(v).Elem().Set(reflect.ValueOf(msg))
	return nil
}

// Send sends v as a single message to the connection.
func (c *conn) Send(v interface{}) error {
	if c == nil {
		return errors.New("attempt to receive on a nil connection")
	}
	if c.readyState() == readyStateClosing || c.readyState() == readyStateClosed {
		return errors.New("attempt to receive on a closed connection")
	}
	c.send <- payload{newPacket(packetTypeMessage, v)}
	return nil
}

// newConn allocates and returns a conn with the given options.
func newConn(pingInterval, pingTimeout int, transport string) *conn {
	return &conn{
		id:           newID(),
		upgrades:     getValidUpgrades(),
		pingInterval: pingInterval,
		pingTimeout:  pingTimeout,
		state:        readyStateOpening,
		send:         make(chan payload),
		messages:     make(chan interface{}, 256),
		transport:    transport,
	}
}

// readyState returns the current state of the connection. It is safe
// to be called from multiple goroutines.
func (c *conn) readyState() string {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

// setReadyState sets the current state of the connection. It is safe
// to be called from multiple goroutines.
func (c *conn) setReadyState(s string) {
	c.stateMu.Lock()
	c.state = s
	c.stateMu.Unlock()
}

// open sends an "open" packet to the underlying connection
// and is used during the initial handshake process. If the
// underlying connection is a http.ResponseWriter, it is
// wrapped in a payload.
func (c *conn) open(w io.Writer) error {
	if c.readyState() == readyStateOpen {
		return errors.New("connection is already open")
	}
	var (
		b   []byte
		err error
	)
	switch w.(type) {
	case *websocket.Conn:
		b, err = newPacket(packetTypeOpen, c).MarshalText()
	case http.ResponseWriter:
		payload := payload{newPacket(packetTypeOpen, c)}
		b, err = payload.MarshalText()
	}
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s", b); err != nil {
		return err
	}
	c.setReadyState(readyStateOpen)
	return nil
}

// Read implements the io.Reader interface and reads data from the FTC connection.
func (c *conn) Read(msg []byte) (int, error) {
	glog.Fatalln("not implemented")
	return 0, nil
}

// Write implements the io.Writer interface and writes data to the FTC connection.
func (c *conn) Write(msg []byte) (int, error) {
	glog.Fatalln("not implemented")
	return 0, nil
}

// Close closes the connection.
func (c *conn) Close() error {
	if c.readyState() == readyStateClosing || c.readyState() == readyStateClosed {
		return errors.New("connection is already closed")
	}
	glog.Infof("closing ftc Connection %p", c)
	c.setReadyState(readyStateClosing)

	if c.send != nil {
		close(c.send)
	}
	c.wsMu.Lock()
	if c.ws != nil {
		c.Close()
		c.ws = nil
	}
	c.wsMu.Unlock()
	c.setReadyState(readyStateClosed)
	return nil
}

// onPacket is called for each packet received by the connection
// and responds to or dispatches the packets accordingly.
func (c *conn) onPacket(p *packet) {
	if c.readyState() != readyStateOpen {
		glog.Errorln("called on non-open connection")
		return
	}
	glog.Infoln("got packet:", *p)
	switch p.typ {
	case packetTypePing:
		c.sendPacket(newPacket(packetTypePong, p.data))
	case packetTypeMessage:
		c.onMessage(p.data)
	case packetTypeUpgrade:
		c.onUpgrade(p)
	case packetTypeClose:
		c.Close()
	}
}

// onUpgrade is called when an upgrade packet is received and
// the connection should be upgraded to use a WebSocket.
// TODO(andybons): consolidate with wsOpen?
func (c *conn) onUpgrade(pkt *packet) {
	glog.Infof("got upgrade packet: %+v", pkt)
	if c.readyState() != readyStateOpen {
		glog.Errorf("upgrade called with non-open ready state %s; packet: %+v", c.readyState(), pkt)
		return
	}
	if c.upgraded && c.transport == transportWebSocket {
		glog.Warningf("transport already upgraded: %+v", pkt)
		return
	}
	c.upgraded = true
	c.transport = transportWebSocket
	go c.webSocketListener()
}

// onMessage is called when a message packet is received.
// It then sends the data to the buffered messages channel
// to be received by the client.
// TODO(andybons): There is a chance that messages could fill up and
// there would be no push-back from spawning unlimited goroutines.
func (c *conn) onMessage(data interface{}) {
	go func() { c.messages <- data }()
}

// sendPacket sends the given packet p to the connection.
func (c *conn) sendPacket(p *packet) {
	if c.readyState() != readyStateOpen {
		glog.Errorln("called on non-open Connection")
		return
	}
	glog.Infoln("sending packet:", *p)
	c.send <- payload{p}
}

// pollingDataPost is called on an XHR polling connection when a POST
// method request is received. It is expected that a payload is present
// in the request body and is meant to serve as the method in which
// clients can send FTC payloads to the server.
func (c *conn) pollingDataPost(w http.ResponseWriter, r *http.Request) {
	glog.Infoln("XHR polling POST")
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	var p payload
	if err := p.UnmarshalText(b); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, pkt := range p {
		c.onPacket(pkt)
	}
	fmt.Fprint(w, "ok")
}

// pollingDataGet is called on an XHR polling connection and is the "long polling"
// aspect of the FTC connection. The request will block until a message is ready to
// be sent back.
func (c *conn) pollingDataGet(w http.ResponseWriter, r *http.Request) {
	glog.Infoln("XHR polling GET")
	if c.transport != transportPolling {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		c.Close()
		return
	}
	buf, ok := <-c.send
	if !ok {
		c.Close()
		return
	}
	b, err := buf.MarshalText()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "%s", b)
}

// webSocketListener continuously listens on the send channel for any
// messages that need to be sent over the connection. It will return
// and close the underlying connection upon any error.
func (c *conn) webSocketListener() {
	glog.Infof("starting websocket listener for socket %s", c.id)
	send := c.send
	for {
		p, ok := <-send
		if !ok {
			return
		}
		for _, pkt := range p {
			c.wsMu.RLock()
			err := sendWSPacket(c.ws, pkt)
			c.wsMu.RUnlock()
			if err != nil {
				glog.Errorln("websocket send error:", err)
				return
			}
		}
	}
}
