// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"

	"code.google.com/p/go.net/websocket"
	"github.com/dustin/randbo"
	"github.com/golang/glog"
)

const defaultTimeout = 30 * time.Second

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

// Conn represents an FTC connection.
type Conn struct {
	c    *conn
	msgs chan []byte
}

func newPubConn(c *conn) *Conn {
	return &Conn{c: c, msgs: make(chan []byte, 10)}
}

func (c *Conn) onMessage(msg []byte) {
	select {
	case c.msgs <- msg:
		return
	case <-time.After(defaultTimeout):
		glog.Warningln("onMessage timed out")
	}
}

func (c *Conn) Read(p []byte) (int, error) {
	select {
	case b := <-c.msgs:
		return copy(p, b), nil
	case <-time.After(defaultTimeout):
		return 0, errors.New("timeout")
	}
}

func (c *Conn) Write(p []byte) (int, error) {
	pkt := packet{typ: packetTypeMessage, data: p}
	if c.c.upgraded() {
		return len(p), newPacketEncoder(c.c).encode(pkt)
	}
	return len(p), newPayloadEncoder(c.c).encode([]packet{pkt})
}

func (c *Conn) Close() error {
	err := c.c.Close()
	c.c = nil
	return err
}

// conn represents an internal FTC connection.
// The publicly available Conn abstracts away the
// underlying protocol by only sending message data
// through Read/Write. If a conn is using a WebSocket
// connection as its underlying transport mechanism,
// then reads and writes go directly to that connection.
// Otherwise (XHR long polling) they are pushed onto
// a buffered channel by a POST to be read later by
// a subsequent GET.
type conn struct {
	id      string      // A unique ID assigned to the conn.
	buf     chan []byte // Storage buffer for messages.
	pubConn *Conn       // Public connection that only reads and writes message data.

	mu     sync.RWMutex    // Protects the items below.
	ws     *websocket.Conn // If upgraded, used to send and receive messages.
	closed bool            // Whether the connection is closed.
}

// newConn allocates and returns a new FTC connection.
func newConn() *conn {
	c := &conn{
		id:  newID(),
		buf: make(chan []byte, 10),
	}
	c.pubConn = newPubConn(c)
	return c
}

// Read copies the next available message to the given
// byte slice. If no message is available, it will block.
func (c *conn) Read(p []byte) (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return 0, errors.New("cannot read on closed connection")
	}
	if c.ws != nil {
		return c.ws.Read(p)
	}
	select {
	case b := <-c.buf:
		return copy(p, b), io.EOF
	case <-time.After(defaultTimeout):
		return 0, errors.New("timeout")
	}
}

// Write writes the contents of p as a single message to
// the connection. This call may block if the number of
// outstanding writes exceeds the size of buf.
func (c *conn) Write(p []byte) (int, error) {
	glog.Infof("writing %q (upgraded: %t)", p, c.upgraded())
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return 0, errors.New("cannot write on closed connection")
	}
	if c.ws != nil {
		return c.ws.Write(p)
	}
	select {
	case c.buf <- p:
		return len(p), nil
	case <-time.After(defaultTimeout):
		return 0, errors.New("timeout")
	}
}

// Close closes the connection.
func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("connection is already closed")
	}
	close(c.buf)
	close(c.pubConn.msgs)
	if c.ws != nil {
		c.ws.Close()
	}
	c.closed = true
	return nil
}

// upgrade assigns the given WebSocket connection to
// the connection.
// TODO(andybons): Flush any messages waiting in buf and close it.
func (c *conn) upgrade(ws *websocket.Conn) {
	glog.Infoln("upgrading connection...")
	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
}

// upgraded returns true if the connection has been upgraded.
func (c *conn) upgraded() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ws != nil
}
