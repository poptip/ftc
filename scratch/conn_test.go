// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package scratch

import (
	"bytes"
	"io"
	"testing"
)

type nopWriter struct{ io.Writer }

func (n nopWriter) Close() error { return nil }

func TestReadWrite(t *testing.T) {
	buf := bytes.NewBuffer([]byte{})
	c := newConn(buf, nopWriter{Writer: buf})
	n, err := c.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("error writing to buffer: %v", err)
	}
	t.Logf("%+v", c)
	t.Logf("Wrote %d. Buf: %+v", n, buf.String())
	b := make([]byte, 1024)
	n, err = c.Read(b)
	if err != nil {
		t.Fatalf("error reading from buffer: %v", err)
	}
	t.Logf("Read str: %q. Buf: %q", b[:n], buf.String())
}
