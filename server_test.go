// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code.google.com/p/go.net/websocket"
)

var echoHandler = Handler(func(c *Conn) { io.Copy(c, c) })

func TestTransportParam(t *testing.T) {
	ts := httptest.NewServer(NewServer(nil, nil))
	defer ts.Close()
	testCases := map[string]int{
		defaultBasePath + "?transport=hyperloop": 400,
		defaultBasePath + "?transport=polling":   200,
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
	ws, err := websocket.Dial("ws://"+serverAddr+defaultBasePath+"?transport=websocket", "", ts.URL)
	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}
	ws.Close()
}

func TestBadSID(t *testing.T) {
	ts := httptest.NewServer(NewServer(nil, nil))
	defer ts.Close()
	addr := ts.URL + defaultBasePath + "?transport=polling&sid=test"
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
	ftcServer := NewServer(nil, nil)
	ts := httptest.NewServer(ftcServer)
	defer ts.Close()
	newName := "woot"
	ftcServer.cookieName = newName
	addr := ts.URL + defaultBasePath + "?transport=polling"
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

func handshakePolling(url string, s *server, t *testing.T) string {
	addr := url + defaultBasePath + "?transport=polling"
	resp, err := http.Get(addr)
	if err != nil {
		t.Fatalf("http get error: %v", err)
	}
	defer resp.Body.Close()
	var payload []packet
	if err := newPayloadDecoder(resp.Body).decode(&payload); err != nil {
		t.Fatalf("could not decode payload from response body: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected payload to have one packet. it has %d", len(payload))
	}
	m := map[string]interface{}{}
	if err := json.Unmarshal(payload[0].data, &m); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	for _, v := range m["upgrades"].([]interface{}) {
		u := v.(string)
		if !validUpgrades[u] {
			t.Errorf("%s is not a valid upgrade.", u)
		}
	}
	return m["sid"].(string)
}

func TestXHRPolling(t *testing.T) {
	ftcServer := NewServer(nil, echoHandler)
	ts := httptest.NewServer(ftcServer)
	defer ts.Close()
	sid := handshakePolling(ts.URL, ftcServer, t)
	// Send a message.
	p := []packet{packet{typ: packetTypeMessage, data: []byte("hello")}}
	buf := bytes.NewBuffer([]byte{})
	if err := newPayloadEncoder(buf).encode(p); err != nil {
		t.Fatalf("could not encode payload: %v", err)
	}
	addr := ts.URL + defaultBasePath + "?transport=polling&sid=" + sid
	resp, err := http.Post(addr, "text/plain;charset=UTF-8", buf)
	if err != nil {
		t.Fatalf("http post error: %v", err)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read post response body: %v", err)
	}
	expected := "ok"
	if string(b) != expected {
		t.Errorf("expected post response to be %q, was %q", expected, b)
	}
	// Receive a message.
	resp, err = http.Get(addr)
	if err != nil {
		t.Fatalf("http get error: %+v", err)
	}
	defer resp.Body.Close()
	var msg []packet
	if err := newPayloadDecoder(resp.Body).decode(&msg); err != nil {
		t.Fatalf("could not decode response body: %v", err)
	}
	if len(msg) != len(p) {
		t.Errorf("differing lengths of payloads: original: %d; response: %d", len(msg), len(p))
	}
	if msg[0].typ != p[0].typ || !bytes.Equal(msg[0].data, p[0].data) {
		t.Errorf("mismatch of packets: %+v and %+v", msg[0], p[0])
	}
}

func TestWebSockets(t *testing.T) {
	ftcServer := NewServer(nil, echoHandler)
	ts := httptest.NewServer(ftcServer)
	defer ts.Close()
	serverAddr := ts.Listener.Addr().String()
	ws, err := websocket.Dial("ws://"+serverAddr+defaultBasePath+"?transport=websocket", "", "http://"+serverAddr)
	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}
	defer ws.Close()
	var pkt packet
	if err := newPacketDecoder(ws).decode(&pkt); err != nil {
		t.Fatalf("could not decode packet: %v", err)
	}
	if pkt.typ != packetTypeOpen {
		t.Errorf("expected packet type to be open (0), got %q", pkt.typ)
	}
	m := map[string]interface{}{}
	if err := json.Unmarshal(pkt.data, &m); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	for _, v := range m["upgrades"].([]interface{}) {
		u := v.(string)
		if !validUpgrades[u] {
			t.Errorf("%s is not a valid upgrade.", u)
		}
	}
	sent := []byte("hello")
	pkt = packet{typ: packetTypeMessage, data: sent}
	if err := newPacketEncoder(ws).encode(pkt); err != nil {
		t.Fatalf("unable to send websocket message %q: %v", sent, err)
	}
	if err := newPacketDecoder(ws).decode(&pkt); err != nil {
		t.Fatalf("error decoding websocket message: %v", err)
	}
	if pkt.typ != packetTypeMessage || !bytes.Equal(pkt.data, sent) {
		t.Errorf("original and returned packets don’t match. returned packet: %+v", pkt)
	}
}
