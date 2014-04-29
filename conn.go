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

func (c *Conn) wsOpen(ws *websocket.Conn) {
	c.ws = ws
	b, err := newPacket(packetTypeOpen, c).MarshalText()
	if err != nil {
		glog.Errorf("problem sending open payload: %v", err)
		return
	}
	if err := websocket.Message.Send(ws, b); err != nil {
		glog.Errorf("problem sending open payload: %v", err)
	}
	c.upgraded = true
	c.readyState = readyStateOpen
	c.transport = transportWebSocket
	go c.websocketListener()
}

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

func (c *Conn) close() {
	glog.Infof("closing ftc Connection %p", c)
	c.readyState = readyStateClosing
	close(c.send)
	close(c.closer)
	c.ws = nil
	c.rw = nil
	c.readyState = readyStateClosed
}

func (c *Conn) onPacket(p *packet) {
	if c.readyState != readyStateOpen {
		glog.Errorln("called on non-open connection")
		return
	}
	glog.Infoln("got packet:", *p)
	switch p.Type() {
	case packetTypePing:
		c.sendPacket(newPacket(packetTypePong, p.Data()))
	case packetTypeMessage:
		c.onMessage(p.Data())
	case packetTypeUpgrade:
		c.onUpgrade(p)
	case packetTypeClose:
		c.close()
	}
}

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
	go c.websocketListener()
}

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

func (c *Conn) onMessage(data interface{}) {
	go func() { c.messages <- data }()
}

func (c *Conn) sendPacket(p *packet) {
	if c.readyState != readyStateOpen {
		glog.Errorln("called on non-open Connection")
		return
	}
	glog.Infoln("sending packet:", *p)
	c.send <- payload{p}
}

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

func (c *Conn) pollingDataGet(w http.ResponseWriter, r *http.Request) {
	glog.Infoln("XHR polling GET")
	if c.transport != transportPolling {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	var buf payload
	select {
	case buf = <-c.send:
	case <-time.After(defaultPingTimeout * time.Millisecond):
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

func (c *Conn) websocketListener() {
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
