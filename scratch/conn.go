// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package scratch

import (
	"bytes"
	"io"

	"github.com/golang/glog"
)

// Conn represents an FTC connection.
type Conn interface {
	io.ReadWriteCloser // The underlying stream.

	upgrade() error // Called when upgrading to WebSocket transport.
}

type conn struct {
	io.Reader
	io.WriteCloser

	buf bytes.Buffer
}

func newConn(r io.Reader, wc io.WriteCloser) Conn {
	return &conn{Reader: r, WriteCloser: wc}
}

func (c *conn) upgrade() error {
	glog.Fatalln("not implemented")
	return nil
}
