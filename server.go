// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

// Package ftc implements an engine.io-compatible server for persistent client-server connections.
package ftc

import (
	"encoding/json"
	"expvar"
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.google.com/p/go.net/websocket"
	"github.com/golang/glog"
)

// The defaults for options passed to the server.
const (
	DefaultBasePath       = "/engine.io/"
	DefaultCookieName     = "io"
	DefaultPingTimeout    = 60000
	DefaultPingInterval   = 25000
	DefaultUpgradeTimeout = 10000
)

const (
	// Protocol error codes and mappings.
	errorTransportUnknown   = 0
	errorUnknownSID         = 1
	errorBadHandshakeMethod = 2
	errorBadRequest         = 3

	// Query parameters used in client requests.
	paramTransport          = "transport"
	paramSessionID          = "sid"
	paramJSONPResponseIndex = "j"

	// Available transports.
	transportWebSocket = "websocket"
	transportPolling   = "polling"

	// The default time before closed connections are cleaned from
	// the client pool.
	clientReapTimeout = 5 * time.Second
)

var errorMessage = map[int]string{
	errorTransportUnknown:   "Transport unknown",
	errorUnknownSID:         "Session ID unknown",
	errorBadHandshakeMethod: "Bad handshake method",
	errorBadRequest:         "Bad request",
}

var (
	validTransports = map[string]bool{
		transportWebSocket: true,
		transportPolling:   true,
	}
	validUpgrades = map[string]bool{
		transportWebSocket: true,
	}
)

var numClients = expvar.NewInt("num_clients")

// getValidUpgrades returns a slice containing the valid protocols
// that a connection can upgrade to.
func getValidUpgrades() []string {
	upgrades := make([]string, len(validUpgrades))
	i := 0
	for u := range validUpgrades {
		upgrades[i] = u
		i++
	}
	return upgrades
}

// A Handler is called by the server when a connection is
// opened successfully. An example echo handler is shown below.
//   func EchoServer(c *ftc.Conn) {
//   	for {
//   		var msg string
//   		if err := c.Receive(&msg); err != nil {
//   			log.Printf("receive: %v", err)
//   			continue
//   		}
//   		if err := c.Send(msg); err != nil {
//   			log.Printf("send: %v", err)
//   		}
//   	}
//   }
// It can then be used in combination with NewServer as follows:
//   http.Handle("/engine.io/", ftc.NewServer(nil, ftc.Handler(EchoServer)))
type Handler func(*Conn)

// A server represents a server of an FTC connection.
type server struct {
	// Handler handles an FTC connection.
	Handler

	basePath   string
	cookieName string

	pingTimeout    int
	pingInterval   int
	upgradeTimeout int

	clients  *clientSet        // The set of connections (some may be closed).
	wsServer *websocket.Server // The underlying WebSocket server.
}

// Options are the parameters passed to the server.
type Options struct {
	// BasePath is the base URL path that the server handles requests for.
	BasePath string
	// CookieName is the name of the cookie set upon successful handshake.
	CookieName string
	// DisableCookie is true if no cookies should be set upon handshake.
	DisableCookie bool
	// PingTimeout is how long a ping packet can hang before the Conn is considered closed.
	PingTimeout int
	// PingInterval specifies how often a ping packet should be sent to the server.
	PingInterval int
	// UpgradeTimeout specifies the maximum time an upgrade can take.
	UpgradeTimeout int
}

// NewServer allocates and returns a new server with the given
// options and handler. If nil options are passed, the defaults
// specified in the constants above are used instead.
func NewServer(o *Options, h Handler) *server {
	if o == nil {
		o = &Options{}
	}
	if len(o.BasePath) == 0 {
		o.BasePath = DefaultBasePath
	}
	if len(o.CookieName) == 0 && !o.DisableCookie {
		o.CookieName = DefaultCookieName
	}
	if o.PingInterval == 0 {
		o.PingInterval = DefaultPingInterval
	}
	if o.PingTimeout == 0 {
		o.PingTimeout = DefaultPingTimeout
	}
	if o.UpgradeTimeout == 0 {
		o.UpgradeTimeout = DefaultUpgradeTimeout
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

// startReaper continuously removes closed connections from the
// client set via the reap function.
func (s *server) startReaper() {
	for {
		// TODO(andybons): does this need to be protected by a mutex?
		if s.clients == nil {
			glog.Fatal("server cannot have a nil client set")
		}
		s.clients.reap()
		numClients.Set(int64(s.clients.len()))
		time.Sleep(clientReapTimeout)
	}
}

// receiveWSPacket calls receive on the given WebSocket connection
// and unmarshals it into the given packet.
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

// sendWSPacket marshals the given packet and sends it over the
// given WebSocket connection.
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

// handleWSPing send the appropriate response packet (pong) with the
// same data as the given packet over the given WebSocket connection.
func handleWSPing(pkt *packet, ws *websocket.Conn) error {
	resp := newPacket(packetTypePong, pkt.data)
	b, err := resp.MarshalText()
	if err != nil {
		return err
	}
	if err := websocket.Message.Send(ws, string(b)); err != nil {
		return err
	}
	return nil
}

// wsMainHandler continuously receives on the given WebSocket
// connection and delegates the packets received to the appropriate
// handler functions.
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

		switch pkt.typ {
		case packetTypePing:
			if err := handleWSPing(pkt, ws); err != nil {
				glog.Errorf("problem handling websocket ping packet: %+v; error: %v", pkt, err)
				break
			}
			// Force a polling cycle to ensure a fast upgrade.
			if data, _ := pkt.data.(string); data == messageProbe {
				c.sendPacket(newPacket(packetTypeNoop, nil))
			}
		case packetTypeUpgrade:
			c.onUpgrade(pkt)
		}
	}
	glog.Infof("closing websocket connection %p", ws)
	c.close()
}

// pollingHandler handles all XHR polling requests to the server, initiating
// a handshake if the request’s session ID does not already exist within
// the client set.
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

// wsHandshake creates a new FTC Conn with the given WebSocket connection
// as the underlying transport and calls the server’s Handler. It returns
// the session ID of the newly-created connection.
func (s *server) wsHandshake(ws *websocket.Conn) string {
	glog.Infof("starting websocket handshake. Handler: %+v", s.Handler)
	c := newConn(s.pingInterval, s.pingTimeout, transportWebSocket)
	s.clients.add(c)
	c.wsOpen(ws)

	go s.Handler(c)
	return c.ID
}

// pollingHandshake creates a new FTC Conn with the given HTTP Request and
// ResponseWriter, setting a persistence cookie if necessary and calling
// the server’s Handler.
func (s *server) pollingHandshake(w http.ResponseWriter, r *http.Request) {
	glog.Infof("starting polling handshake. Handler: %+v", s.Handler)
	c := newConn(s.pingInterval, s.pingTimeout, transportPolling)
	s.clients.add(c)

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

// ServeHTTP implements the http.Handler interface for an FTC Server.
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

// serverError sends a JSON-encoded message to the given ResponseWriter
// with the given error code.
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
