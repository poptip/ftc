// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code.google.com/p/go.net/websocket"
	"github.com/golang/glog"
)

var echoHandler = Handler(func(c Conn) {
	for {
		var msg string
		if err := c.Receive(&msg); err != nil {
			continue
		}
		if err := c.Send(msg); err != nil {
			glog.Errorf("send: %v", err)
		}
	}
})

func TestTransportParam(t *testing.T) {
	ts := httptest.NewServer(NewServer(nil, echoHandler))
	defer ts.Close()
	testCases := map[string]int{
		DefaultBasePath + "?transport=hyperloop": 400,
		DefaultBasePath + "?transport=polling":   200,
	}
	for path, statusCode := range testCases {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Error(err)
		}
		if resp.StatusCode != statusCode {
			t.Errorf("%s: got status code %d. expected %d.", path, resp.StatusCode, statusCode)
		}
		resp.Body.Close()
	}
	serverAddr := ts.Listener.Addr().String()
	ws, err := websocket.Dial("ws://"+serverAddr+DefaultBasePath+"?transport=websocket", "", ts.URL)
	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}
	ws.Close()
}

func TestBadSID(t *testing.T) {
	ts := httptest.NewServer(NewServer(nil, echoHandler))
	defer ts.Close()
	addr := ts.URL + DefaultBasePath + "?transport=polling&sid=test"
	resp, err := http.Get(addr)
	if err != nil {
		t.Error(err)
	}
	expected := 400
	if resp.StatusCode != expected {
		t.Errorf("%s: got status code %d. expected %d.", addr, resp.StatusCode, expected)
	}
	resp.Body.Close()
}

func TestSetCookie(t *testing.T) {
	ftcServer := NewServer(nil, echoHandler)
	ts := httptest.NewServer(ftcServer)
	defer ts.Close()
	newName := "woot"
	ftcServer.cookieName = newName
	addr := ts.URL + DefaultBasePath + "?transport=polling"
	resp, err := http.Get(addr)
	if err != nil {
		t.Fatalf("http get error: %v", err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Set-Cookie"), newName) {
		t.Errorf("cookie not set to %s as expected. cookie set header %s", newName, resp.Header.Get("Set-Cookie"))
	}
	// The server should not set the cookie in this case.
	ftcServer.cookieName = ""
	resp, err = http.Get(addr)
	if err != nil {
		t.Fatalf("http get error: %v", err)
	}
	defer resp.Body.Close()
	if len(resp.Header.Get("Set-Cookie")) > 0 {
		t.Errorf("cookie is set to %s when it should not be", resp.Header.Get("Set-Cookie"))
	}
}

func validateConn(c *conn, s *server, t *testing.T) {
	if len(c.id) == 0 {
		t.Error("session id of conn is empty.")
	}
	for _, u := range c.upgrades {
		if !validUpgrades[u] {
			t.Errorf("%s is not a valid upgrade.", u)
		}
	}
	if c.pingInterval != s.pingInterval {
		t.Errorf("ping intervals don’t match. client: %d; server: %d", c.pingInterval, s.pingInterval)
	}
	if c.pingTimeout != s.pingTimeout {
		t.Errorf("ping timeouts don’t match. client: %d; server: %d", c.pingTimeout, s.pingTimeout)
	}
}

func handshakePolling(url string, s *server, t *testing.T) string {
	addr := url + DefaultBasePath + "?transport=polling"
	resp, err := http.Get(addr)
	if err != nil {
		t.Fatalf("http get error: %v", err)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read response body: %v", err)
	}
	var p payload
	if err := p.UnmarshalText(b); err != nil {
		t.Fatalf("could not unmarshal response into payload: %v", err)
	}
	if len(p) != 1 {
		t.Fatalf("expected payload to have one packet. it has %d", len(p))
	}
	var c conn
	if err := json.Unmarshal([]byte(p[0].data.(string)), &c); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	validateConn(&c, s, t)
	return c.id
}

func TestXHRPolling(t *testing.T) {
	ftcServer := NewServer(nil, echoHandler)
	ts := httptest.NewServer(ftcServer)
	defer ts.Close()
	sid := handshakePolling(ts.URL, ftcServer, t)
	// Send a message.
	p := payload{newPacket(packetTypeMessage, "hello")}
	b, err := p.MarshalText()
	if err != nil {
		t.Fatalf("could not marshal payload: %v", err)
	}
	addr := ts.URL + DefaultBasePath + "?transport=polling&sid=" + sid
	resp, err := http.Post(addr, "text/plain;charset=UTF-8", bytes.NewBuffer(b))
	if err != nil {
		t.Fatalf("http post error: %v", err)
	}
	defer resp.Body.Close()
	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read post response body: %v", err)
	}
	if string(b) != "ok" {
		t.Errorf("expected post response to be \"ok\", was %q", b)
	}
	// Receive a message.
	resp, err = http.Get(addr)
	if err != nil {
		t.Fatalf("http get error: %+v", err)
	}
	defer resp.Body.Close()
	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read get response body: %v", err)
	}
	var msg payload
	if err := msg.UnmarshalText(b); err != nil {
		t.Fatalf("could not unmarshal %q into payload: %v", b, err)
	}
	if len(msg) != len(p) {
		t.Errorf("differing lengths of payloads: original: %d; response: %d", len(msg), len(p))
	}
	if msg[0].typ != p[0].typ || msg[0].data.(string) != msg[0].data.(string) {
		t.Errorf("mismatch of packets: %+v and %+v", msg[0], p[0])
	}
}

func TestWebSockets(t *testing.T) {
	ftcServer := NewServer(nil, echoHandler)
	ts := httptest.NewServer(ftcServer)
	defer ts.Close()
	serverAddr := ts.Listener.Addr().String()
	ws, err := websocket.Dial("ws://"+serverAddr+DefaultBasePath+"?transport=websocket", "", "http://"+serverAddr)
	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}
	defer ws.Close()
	var msg []byte
	if err := websocket.Message.Receive(ws, &msg); err != nil {
		t.Fatalf("error receiving websocket message: %v", err)
	}
	var pkt packet
	if err := pkt.UnmarshalText(msg); err != nil {
		t.Fatalf("error unmarshaling packet: %v", err)
	}
	if pkt.typ != packetTypeOpen {
		t.Errorf("expected packet type to be open (0), got %q", pkt.typ)
	}
	var c conn
	if err := json.Unmarshal([]byte(pkt.data.(string)), &c); err != nil {
		t.Errorf("json unmarshal error: %v", err)
	}
	validateConn(&c, ftcServer, t)
	sent := "hello"
	pkt.typ = packetTypeMessage
	pkt.data = sent
	b, err := pkt.MarshalText()
	if err != nil {
		t.Fatalf("error marshaling packet: %v", err)
	}
	if err := websocket.Message.Send(ws, b); err != nil {
		t.Fatalf("unable to send message %q: %v", b, err)
	}
	if err := websocket.Message.Receive(ws, &msg); err != nil {
		t.Fatalf("error receiving websocket message: %v", err)
	}
	if err := pkt.UnmarshalText(msg); err != nil {
		t.Fatalf("error unmarshaling packet: %v", err)
	}
	if pkt.typ != packetTypeMessage || pkt.data.(string) != sent {
		t.Errorf("original and returned packets don’t match. returned packet: %+v", pkt)
	}
}
