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
	"sync"
	"testing"

	"code.google.com/p/go.net/websocket"
	"github.com/golang/glog"
)

var (
	serverAddr string
	ftcServer  *server
	once       sync.Once
)

func startServer() {
	// Basic echo testing server.
	h := Handler(func(c *Conn) {
		for {
			var msg string
			if err := c.Receive(&msg); err != nil {
				glog.Errorf("receive: %v", err)
				continue
			}
			if err := c.Send(msg); err != nil {
				glog.Errorf("send: %v", err)
			}
		}
	})
	ftcServer = NewServer(nil, h)
	serverAddr = httptest.NewServer(ftcServer).Listener.Addr().String()
	glog.Infoln("test server listening on", serverAddr)
}

func TestTransportParam(t *testing.T) {
	once.Do(startServer)
	testCases := map[string]int{
		DefaultBasePath + "?transport=hyperloop": 400,
		DefaultBasePath + "?transport=polling":   200,
	}
	for path, statusCode := range testCases {
		resp, err := http.Get("http://" + serverAddr + path)
		if err != nil {
			t.Error(err)
		}
		if resp.StatusCode != statusCode {
			t.Errorf("%s: got status code %d. expected %d.", path, resp.StatusCode, statusCode)
		}
		resp.Body.Close()
	}
	ws, err := websocket.Dial("ws://"+serverAddr+DefaultBasePath+"?transport=websocket", "", "http://"+serverAddr)
	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}
	ws.Close()
}

func TestBadSID(t *testing.T) {
	once.Do(startServer)
	addr := "http://" + serverAddr + DefaultBasePath + "?transport=polling&sid=test"
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
	original := ftcServer.cookieName
	defer func(n string) { ftcServer.cookieName = n }(original)
	newName := "woot"
	ftcServer.cookieName = newName
	addr := "http://" + serverAddr + DefaultBasePath + "?transport=polling"
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

func handshakePolling(t *testing.T) *Conn {
	addr := "http://" + serverAddr + DefaultBasePath + "?transport=polling"
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
	var c Conn
	if err := json.Unmarshal([]byte(p[0].data.(string)), &c); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	if len(c.ID) == 0 {
		t.Error("session id of conn is empty.")
	}
	for _, u := range c.Upgrades {
		if !validUpgrades[u] {
			t.Errorf("%s is not a valid upgrade.", u)
		}
	}
	if c.PingInterval != ftcServer.pingInterval {
		t.Errorf("ping intervals don’t match. client: %d; server: %d", c.PingInterval, ftcServer.pingInterval)
	}
	if c.PingTimeout != ftcServer.pingTimeout {
		t.Errorf("ping timeouts don’t match. client: %d; server: %d", c.PingTimeout, ftcServer.pingTimeout)
	}
	return &c
}

func TestXHRPolling(t *testing.T) {
	c := handshakePolling(t)
	defer c.Close()
	// Send a message.
	p := payload{newPacket(packetTypeMessage, "hello")}
	b, err := p.MarshalText()
	if err != nil {
		t.Fatalf("could not marshal payload: %v", err)
	}
	addr := "http://" + serverAddr + DefaultBasePath + "?transport=polling&sid=" + c.ID
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
