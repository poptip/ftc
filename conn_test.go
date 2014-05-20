// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"bytes"
	"io"
	"testing"
)

type nopWriter struct{ io.Writer }

func (n nopWriter) Close() error { return nil }

func TestReadWrite(t *testing.T) {
	c := newConn()
	defer c.Close()
	data := []byte("hello")
	_, err := c.Write(data)
	if err != nil {
		t.Fatalf("error writing to conn: %v", err)
	}
	b := make([]byte, len(data))
	_, err = io.ReadFull(c, b)
	if err != nil {
		t.Fatalf("error reading from conn: %v", err)
	}
	if !bytes.Equal(b, data) {
		t.Errorf("expected read to be %q, got %q", data, b)
	}
}

func TestClosedConnection(t *testing.T) {
	c1 := newConn()
	if err := c1.Close(); err != nil {
		t.Fatalf("problem closing connection: %v", err)
	}
	c2 := newConn()
	if err := c2.pubConn.Close(); err != nil {
		t.Fatalf("problem closing public connection: %v", err)
	}
	for _, c := range []*conn{c1, c2} {
		b := make([]byte, 5)
		if n, err := c.Read(b); err == nil || n != 0 {
			t.Errorf("expected zero bytes read and error due to closed conn. read %d bytes.", n)
		}
		if n, err := c.Write(b); err == nil || n != 0 {
			t.Errorf("expected zero bytes written and error due to closed conn. wrote %d bytes.", n)
		}
	}
	if err := c2.Close(); err == nil {
		t.Error("expected error from closing closed connection")
	}
}
