// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"code.google.com/p/go.net/websocket"

	"github.com/golang/glog"
)

var numClients = expvar.NewInt("num_clients")

const (
	// Protocol error codes and mappings.
	errorTransportUnknown   = 0
	errorUnknownSID         = 1
	errorBadHandshakeMethod = 2
	errorBadRequest         = 3

	// Query parameters used in client requests.
	paramTransport = "transport"
	paramSessionID = "sid"

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
// opened successfully.
type Handler func(*Conn)

type server struct {
	// Handler handles an FTC connection.
	Handler

	basePath   string
	cookieName string

	clients  *clientSet        // The set of connections (some may be closed).
	wsServer *websocket.Server // The underlying WebSocket server.
}

// The defaults for options passed to the server.
const (
	defaultBasePath   = "/engine.io/"
	defaultCookieName = "io"
)

// Options are the parameters passed to the server.
type Options struct {
	// BasePath is the base URL path that the server handles requests for.
	BasePath string
	// CookieName is the name of the cookie set upon successful handshake.
	CookieName string
}

// NewServer allocates and returns a new server with the given
// options and handler. If nil options are passed, the defaults
// specified in the constants above are used instead.
func NewServer(o *Options, h Handler) *server {
	opts := Options{}
	if o != nil {
		opts = *o
	}
	if len(opts.BasePath) == 0 {
		opts.BasePath = defaultBasePath
	}
	if len(opts.CookieName) == 0 {
		opts.CookieName = defaultCookieName
	}
	s := &server{
		Handler:    h,
		basePath:   opts.BasePath,
		cookieName: opts.CookieName,
		clients:    &clientSet{clients: map[string]*conn{}},
	}
	go s.startReaper()
	s.wsServer = &websocket.Server{Handler: s.wsHandler}
	return s
}

// startReaper continuously removes closed connections from the
// client set via the reap function.
func (s *server) startReaper() {
	for {
		if s.clients == nil {
			glog.Fatal("server cannot have a nil client set")
		}
		s.clients.reap()
		numClients.Set(int64(s.clients.len()))
		time.Sleep(clientReapTimeout)
	}
}

// handlePacket takes the given packet and writes the appropriate
// response to the given connection.
func (s *server) handlePacket(p packet, c *conn) error {
	glog.Infof("handling packet type: %c, data: %s, upgraded: %t", p.typ, p.data, c.upgraded())
	var encode func(packet) error
	if c.upgraded() {
		encode = newPacketEncoder(c).encode
	} else {
		encode = func(pkt packet) error {
			return newPayloadEncoder(c).encode([]packet{pkt})
		}
	}
	switch p.typ {
	case packetTypePing:
		return encode(packet{typ: packetTypePong, data: p.data})
	case packetTypeMessage:
		if c.pubConn != nil {
			c.pubConn.onMessage(p.data)
		}
	case packetTypeClose:
		c.Close()
	}
	return nil
}

// wsHandler continuously receives on the given WebSocket
// connection and delegates the packets received to the
// appropriate handler functions.
func (s *server) wsHandler(ws *websocket.Conn) {
	// If the client initially attempts to connect directly using
	// WebSocket transport, the session ID parameter will be empty.
	// Otherwise, the connection with the given session ID will
	// need to be upgraded.
	glog.Infoln("Starting websocket handler...")
	var c *conn
	wsEncoder, wsDecoder := newPacketEncoder(ws), newPacketDecoder(ws)
	for {
		if c != nil {
			var pkt packet
			if err := wsDecoder.decode(&pkt); err != nil {
				glog.Errorf("could not decode packet: %v", err)
				break
			}
			glog.Infof("WS: got packet type: %c, data: %s", pkt.typ, pkt.data)
			if pkt.typ == packetTypeUpgrade {
				// Upgrade the connection to use this WebSocket Conn.
				c.upgrade(ws)
				continue
			}
			if err := s.handlePacket(pkt, c); err != nil {
				glog.Errorf("could not handle packet: %v", err)
				break
			}
			continue
		}
		id := ws.Request().FormValue(paramSessionID)
		c = s.clients.get(id)
		if len(id) > 0 && c == nil {
			serverError(ws, errorUnknownSID)
			break
		} else if len(id) > 0 && c != nil {
			// The initial handshake requires a ping (2) and pong (3) echo.
			var pkt packet
			if err := wsDecoder.decode(&pkt); err != nil {
				glog.Errorf("could not decode packet: %v", err)
				continue
			}
			glog.Infof("WS: got packet type: %c, data: %s", pkt.typ, pkt.data)
			if pkt.typ == packetTypePing {
				glog.Infof("got ping packet with data %s", pkt.data)
				if err := wsEncoder.encode(packet{typ: packetTypePong, data: pkt.data}); err != nil {
					glog.Errorf("could not encode pong packet: %v", err)
					continue
				}
				// Force a polling cycle to ensure a fast upgrade.
				glog.Infoln("forcing polling cycle")
				payload := []packet{packet{typ: packetTypeNoop}}
				if err := newPayloadEncoder(c).encode(payload); err != nil {
					glog.Errorf("could not encode packet to force polling cycle: %v", err)
					continue
				}
			}
		} else if len(id) == 0 && c == nil {
			// Create a new connection with this WebSocket Conn.
			c = newConn()
			c.ws = ws
			s.clients.add(c)
			b, err := handshakeData(c)
			if err != nil {
				glog.Errorf("could not get handshake data: %v", err)
			}
			if err := wsEncoder.encode(packet{typ: packetTypeOpen, data: b}); err != nil {
				glog.Errorf("could not encode open packet: %v", err)
				break
			}
			if s.Handler != nil {
				go s.Handler(c.pubConn)
			}
		}
	}
	glog.Infof("closing websocket connection %p", ws)
	c.Close()
}

// pollingHandler handles all XHR polling requests to the server, initiating
// a handshake if the request’s session ID does not already exist within
// the client set.
func (s *server) pollingHandler(w http.ResponseWriter, r *http.Request) {
	setPollingHeaders(w, r)
	id := r.FormValue(paramSessionID)
	if len(id) > 0 {
		c := s.clients.get(id)
		if c == nil {
			serverError(w, errorUnknownSID)
			return
		}
		if r.Method == "POST" {
			var payload []packet
			if err := newPayloadDecoder(r.Body).decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer r.Body.Close()
			for _, pkt := range payload {
				s.handlePacket(pkt, c)
			}
			fmt.Fprintf(w, "ok")
			return
		} else if r.Method == "GET" {
			glog.Infoln("GET request xhr polling data...")
			// TODO(andybons): Requests can pile up, here. Drain the conn and
			// then write the payload.
			if _, err := io.Copy(w, c); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	s.pollingHandshake(w, r)
}

// pollingHandshake creates a new FTC Conn with the given HTTP Request and
// ResponseWriter, setting a persistence cookie if necessary and calling
// the server’s Handler.
func (s *server) pollingHandshake(w http.ResponseWriter, r *http.Request) {
	c := newConn()
	s.clients.add(c)
	if len(s.cookieName) > 0 {
		http.SetCookie(w, &http.Cookie{
			Name:  s.cookieName,
			Value: c.id,
		})
	}
	b, err := handshakeData(c)
	if err != nil {
		glog.Errorf("could not get handshake data: %v", err)
	}
	payload := []packet{packet{typ: packetTypeOpen, data: b}}
	if err := newPayloadEncoder(w).encode(payload); err != nil {
		glog.Errorf("could not encode open payload: %v", err)
		return
	}
	if s.Handler != nil {
		go s.Handler(c.pubConn)
	}
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
	} else if transport == transportPolling {
		s.pollingHandler(w, r)
	}
}

// handshakeData returns the JSON encoded data needed
// for the initial connection handshake.
func handshakeData(c *conn) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"pingInterval": 25000,
		"pingTimeout":  60000,
		"upgrades":     getValidUpgrades(),
		"sid":          c.id,
	})
}

// serverError sends a JSON-encoded message to the given io.Writer
// with the given error code.
func serverError(w io.Writer, code int) {
	if rw, ok := w.(http.ResponseWriter); ok {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusBadRequest)
	}
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
	glog.Errorf("wrote server error: %+v", msg)
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
