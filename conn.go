// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"time"

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

// newID returns a pseudo-random, URL-encoded, base64
// string used for connection identifiers.
func newID() string {
	buf := make([]byte, 15)
	n, err := randbo.New().Read(buf)
	if err != nil {
		panic(err)
	}
	if n != len(buf) {
		panic("short read")
	}
	return base64.URLEncoding.EncodeToString(buf)
}

// A Conn represents an FTC connection.
type Conn struct {
	rw http.ResponseWriter `json:"-"`
	ws *websocket.Conn     `json:"-"`

	ID           string   `json:"sid"`
	Upgrades     []string `json:"upgrades"`
	PingInterval int      `json:"pingInterval"`
	PingTimeout  int      `json:"pingTimeout"`

	readyState string           `json:"-"`
	transport  string           `json:"-"`
	upgraded   bool             `json:"-"`
	send       chan payload     `json:"-"`
	messages   chan interface{} `json:"-"`
	closer     chan struct{}    `json:"-"`
}

// Receive receives a single message from the connection into v.
func (c *Conn) Receive(v interface{}) error {
	if c == nil {
		return errors.New("attempt to receive on a nil connection")
	}
	if c.readyState == readyStateClosing || c.readyState == readyStateClosed {
		return errors.New("attempt to receive on a closed connection")
	}
	msg := <-c.messages
	// TODO(andybons): Error checking for proper values.
	reflect.ValueOf(v).Elem().Set(reflect.ValueOf(msg))
	return nil
}

// Send sends v as a single message to the connection.
func (c *Conn) Send(v interface{}) error {
	if c == nil {
		return errors.New("attempt to receive on a nil connection")
	}
	if c.readyState == readyStateClosing || c.readyState == readyStateClosed {
		return errors.New("attempt to receive on a closed connection")
	}
	c.send <- payload{newPacket(packetTypeMessage, v)}
	return nil
}

// newConn allocates and returns a Conn with the given options.
func newConn(pingInterval, pingTimeout int, transport string) *Conn {
	return &Conn{
		ID:           newID(),
		Upgrades:     getValidUpgrades(),
		PingInterval: pingInterval,
		PingTimeout:  pingTimeout,
		readyState:   readyStateOpening,
		send:         make(chan payload),
		messages:     make(chan interface{}, 256),
		closer:       make(chan struct{}, 1),
		transport:    transport,
	}
}

// wsOpen sends an "open" packet to the underlying WebSocket
// connection and is used during the initial handshake process.
func (c *Conn) wsOpen(ws *websocket.Conn) {
	// TODO(andybons): do we need to keep references to this around?
	// TODO(andybons): ensure that this is only called once on a connection.
	c.ws = ws
	b, err := newPacket(packetTypeOpen, c).MarshalText()
	if err != nil {
		glog.Errorf("problem marshaling open payload: %v", err)
		c.close()
		return
	}
	if err := websocket.Message.Send(ws, string(b)); err != nil {
		glog.Errorf("problem sending open payload: %v", err)
		c.close()
		return
	}
	c.upgraded = true
	c.readyState = readyStateOpen
	c.transport = transportWebSocket
	go c.webSocketListener()
}

// pollingOpen sends an "open" packet to the underlying XHR polling
// connection and is used during the initial handshake.
func (c *Conn) pollingOpen(w http.ResponseWriter, r *http.Request) {
	c.rw = w
	setPollingHeaders(w, r)
	payload := payload{newPacket(packetTypeOpen, c)}
	b, err := payload.MarshalText()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, "%s", b)
	c.readyState = readyStateOpen
}

// close closes the connection.
func (c *Conn) close() {
	glog.Infof("closing ftc Connection %p", c)
	c.readyState = readyStateClosing
	close(c.send)
	close(c.closer)
	c.ws = nil
	c.rw = nil
	c.readyState = readyStateClosed
}

// onPacket is called for each packet received by the connection
// and responds to or dispatches the packets accordingly.
func (c *Conn) onPacket(p *packet) {
	if c.readyState != readyStateOpen {
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
		c.close()
	}
}

// onUpgrade is called when an upgrade packet is received and
// the connection should be upgraded to use a WebSocket.
// TODO(andybons): consolidate with wsOpen?
func (c *Conn) onUpgrade(pkt *packet) {
	glog.Infof("got upgrade packet: %+v", pkt)
	if c.readyState != readyStateOpen {
		glog.Errorf("upgrade called with non-open ready state %s; packet: %+v", c.readyState, pkt)
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
func (c *Conn) onMessage(data interface{}) {
	go func() { c.messages <- data }()
}

// sendPacket sends the given packet p to the connection.
func (c *Conn) sendPacket(p *packet) {
	if c.readyState != readyStateOpen {
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
func (c *Conn) pollingDataPost(w http.ResponseWriter, r *http.Request) {
	glog.Infoln("XHR polling POST")
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	glog.Infoln(string(b))
	var p payload
	if err := p.UnmarshalText(b); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, pkt := range p {
		c.onPacket(pkt)
	}
	setPollingHeaders(w, r)
	fmt.Fprint(w, "ok")
}

// pollingDataGet is called on an XHR polling connection and is the "long polling"
// aspect of the FTC connection. The request will wait until either a message is
// ready to be sent back, or it will timeout according to the DefaultPingTimeout
// constant. In the case of a timout, a no-op packet is sent to complete the request.
func (c *Conn) pollingDataGet(w http.ResponseWriter, r *http.Request) {
	glog.Infoln("XHR polling GET")
	if c.transport != transportPolling {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	var buf payload
	select {
	case buf = <-c.send:
	case <-time.After(DefaultPingTimeout * time.Millisecond):
		glog.Infof("GET timeout for Conn %p", c)
		buf = payload{newPacket(packetTypeNoop, nil)}
	}
	glog.Infof("sending buffer: %+v", buf)
	b, err := buf.MarshalText()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	setPollingHeaders(w, r)
	fmt.Fprintf(w, "%s", b)
}

// webSocketListener continuously listens on the send channel for any
// messages that need to be sent over the connection. It is shut down
// by sending an empty struct to the closer channel.
func (c *Conn) webSocketListener() {
	glog.Infof("starting websocket listener for socket %s", c.ID)
	defer c.ws.Close()
	for {
		select {
		case p := <-c.send:
			for _, pkt := range p {
				if err := sendWSPacket(c.ws, pkt); err != nil {
					glog.Errorln("websocket send error:", err)
					continue
				}
			}
		case <-c.closer:
			return
		}
	}
}

// setPollingHeaders sets the appropriate headers when responding
// to an XHR polling request.
func setPollingHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if len(origin) > 0 {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	} else {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
}
