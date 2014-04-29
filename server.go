// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.google.com/p/go.net/websocket"
	"github.com/golang/glog"
)

// Protocol error codes and mappings.
const (
	errorTransportUnknown   = 0
	errorUnknownSID         = 1
	errorBadHandshakeMethod = 2
	errorBadRequest         = 3
)

var errorMessage = map[int]string{
	errorTransportUnknown:   "Transport unknown",
	errorUnknownSID:         "Session ID unknown",
	errorBadHandshakeMethod: "Bad handshake method",
	errorBadRequest:         "Bad request",
}

const (
	defaultBasePath       = "/engine.io/"
	defaultCookieName     = "io"
	defaultPingTimeout    = 60000
	defaultPingInterval   = 25000
	defaultUpgradeTimeout = 10000

	paramTransport          = "transport"
	paramSessionID          = "sid"
	paramJSONPResponseIndex = "j"

	transportWebSocket = "websocket"
	transportPolling   = "polling"

	clientReapTimeout = 5 * time.Second
)

var (
	validTransports = map[string]bool{
		transportWebSocket: true,
		transportPolling:   true,
	}
	validUpgrades = map[string]bool{
		transportWebSocket: true,
	}
)

func getValidUpgrades() []string {
	upgrades := make([]string, len(validUpgrades))
	i := 0
	for u := range validUpgrades {
		upgrades[i] = u
		i++
	}
	return upgrades
}

type Handler func(*Conn)

type server struct {
	basePath       string
	cookieName     string
	pingTimeout    int
	pingInterval   int
	upgradeTimeout int
	clients        *clientSet
	wsServer       *websocket.Server

	Handler
}

type Options struct {
	BasePath       string
	CookieName     string
	DisableCookie  bool
	PingTimeout    int
	PingInterval   int
	UpgradeTimeout int
}

func NewServer(o *Options, h Handler) *server {
	if o == nil {
		o = &Options{}
	}
	if len(o.BasePath) == 0 {
		o.BasePath = defaultBasePath
	}
	if len(o.CookieName) == 0 && !o.DisableCookie {
		o.CookieName = defaultCookieName
	}
	if o.PingInterval == 0 {
		o.PingInterval = defaultPingInterval
	}
	if o.PingTimeout == 0 {
		o.PingTimeout = defaultPingTimeout
	}
	if o.UpgradeTimeout == 0 {
		o.UpgradeTimeout = defaultUpgradeTimeout
	}

	s := &server{
		Handler:        h,
		basePath:       o.BasePath,
		cookieName:     o.CookieName,
		pingTimeout:    o.PingTimeout,
		pingInterval:   o.PingInterval,
		upgradeTimeout: o.UpgradeTimeout,
		clients:        newClientSet(),
	}
	go s.startReaper()
	s.wsServer = &websocket.Server{Handler: s.wsMainHandler}
	return s
}

func (s *server) startReaper() {
	for {
		if s.clients == nil {
			break
		}
		glog.Infof("reaping clients. count: %d", s.clients.len())
		s.clients.reap()
		glog.Infof("clients reaped. count: %d", s.clients.len())
		time.Sleep(clientReapTimeout)
	}
}

func receiveWSPacket(ws *websocket.Conn, pkt *packet) error {
	var msg string
	if err := websocket.Message.Receive(ws, &msg); err != nil {
		return err
	}
	glog.Infoln("got websocket message", msg)
	if err := pkt.UnmarshalText([]byte(msg)); err != nil {
		return err
	}
	return nil
}

func sendWSPacket(ws *websocket.Conn, pkt *packet) error {
	if ws == nil || pkt == nil {
		return fmt.Errorf("nil websocket or packet ws: %+v, pkt: %+v", ws, pkt)
	}
	glog.Infof("sending websocket packet: %+v", pkt)
	b, err := pkt.MarshalText()
	if err != nil {
		return err
	}
	if err := websocket.Message.Send(ws, string(b)); err != nil {
		return err
	}
	return nil
}

func handleWSPing(pkt *packet, ws *websocket.Conn) error {
	resp := newPacket(packetTypePong, pkt.Data())
	b, err := resp.MarshalText()
	if err != nil {
		return err
	}
	if err := websocket.Message.Send(ws, string(b)); err != nil {
		return err
	}
	return nil
}

func (s *server) wsMainHandler(ws *websocket.Conn) {
	var c *Conn
	var sid string
	for {
		if len(sid) == 0 {
			glog.Infoln("websocket: no session id.")
			sid = ws.Request().FormValue(paramSessionID)
			if len(sid) == 0 {
				sid = s.wsHandshake(ws)
			}
		}
		c = s.clients.get(sid)
		if c == nil {
			glog.Errorln("could not find connection with id", sid)
			continue
		}

		glog.Infoln("waiting for websocket message")
		pkt := &packet{}
		if err := receiveWSPacket(ws, pkt); err != nil {
			glog.Errorln("websocket read message error:", err)
			break
		}
		glog.Infof("got websocket packet %+v from conn %p", pkt, ws)
		if c.upgraded {
			c.onPacket(pkt)
			continue
		}

		// Client has not been upgraded yet.
		if c.ws != nil && c.ws != ws {
			glog.Errorf("websocket already associated with session %s", c.ID)
			break
		}
		c.ws = ws

		switch pkt.Type() {
		case packetTypePing:
			if err := handleWSPing(pkt, ws); err != nil {
				glog.Errorf("problem handling websocket ping packet: %+v; error: %v", pkt, err)
				break
			}
			// Force a polling cycle to ensure a fast upgrade.
			if data, _ := pkt.Data().(string); data == messageProbe {
				c.sendPacket(newPacket(packetTypeNoop, nil))
			}
		case packetTypeUpgrade:
			c.onUpgrade(pkt)
		}
	}
	glog.Infof("closing websocket connection %p", ws)
	c.close()
}

func (s *server) pollingHandler(w http.ResponseWriter, r *http.Request) {
	sid := r.FormValue(paramSessionID)
	if len(sid) > 0 {
		socket := s.clients.get(sid)
		if socket == nil {
			serverError(w, errorUnknownSID)
			return
		}
		if r.Method == "POST" {
			socket.pollingDataPost(w, r)
			return
		} else if r.Method == "GET" {
			socket.pollingDataGet(w, r)
			return
		}
		http.Error(w, "bad method", http.StatusBadRequest)
		return
	}
	s.pollingHandshake(w, r)
}

func (s *server) wsHandshake(ws *websocket.Conn) string {
	glog.Infof("starting websocket handleshake. Handler: %+v", s.Handler)
	c := newConn(s.pingInterval, s.pingTimeout, transportWebSocket)
	s.clients.set(c.ID, c)
	c.wsOpen(ws)

	s.Handler(c)
	return c.ID
}

func (s *server) pollingHandshake(w http.ResponseWriter, r *http.Request) {
	glog.Infof("starting polling handleshake. Handler: %+v", s.Handler)
	c := newConn(s.pingInterval, s.pingTimeout, transportPolling)
	s.clients.set(c.ID, c)

	if len(s.cookieName) > 0 {
		glog.Infoln("setting cookie:", s.cookieName, c.ID)
		http.SetCookie(w, &http.Cookie{
			Name:  s.cookieName,
			Value: c.ID,
		})
	}
	c.pollingOpen(w, r)
	glog.Infof("polling handleshake complete. calling Handler: %+v", s.Handler)
	go s.Handler(c)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	remoteAddr := r.Header.Get("X-Forwarded-For")
	if len(remoteAddr) == 0 {
		remoteAddr = r.RemoteAddr
	}
	glog.Infof("%s (%s) %s %s %s", r.Proto, r.Header.Get("X-Forwarded-Proto"), r.Method, remoteAddr, r.URL)

	transport := r.FormValue(paramTransport)
	if strings.HasPrefix(r.URL.Path, s.basePath) && !validTransports[transport] {
		serverError(w, errorTransportUnknown)
		return
	}

	if transport == transportWebSocket {
		s.wsServer.ServeHTTP(w, r)
		return
	}

	s.pollingHandler(w, r)
}

func serverError(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	msg := struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{
		Code:    code,
		Message: errorMessage[code],
	}
	if err := json.NewEncoder(w).Encode(msg); err != nil {
		glog.Errorln("error encoding error msg %+v: %s", msg, err)
		return
	}
}
