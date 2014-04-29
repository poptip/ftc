// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import "testing"

func TestClientSetBasic(t *testing.T) {
	s := newClientSet()
	c1 := &Conn{ID: "foo"}
	c2 := &Conn{ID: "bar"}
	c3 := &Conn{ID: "baz"}
	s.set(c1.ID, c1)
	s.set(c2.ID, c2)
	s.set(c3.ID, c3)
	if s.len() != 3 {
		t.Errorf("expected set length to be 3, was %d", s.len())
	}
	if s.get(c1.ID) != c1 {
		t.Errorf("expected Conn with ID %s, got %+v", c1.ID, s.get(c1.ID))
	}
	c3.readyState = readyStateClosed
	s.reap()
	c := s.get(c3.ID)
	if c != nil || s.len() != 2 {
		t.Errorf("expected Conn to be reaped due to being in closed state, got %+v", c)
	}
	s.remove(c1.ID)
	c = s.get(c1.ID)
	if c != nil || s.len() != 1 {
		t.Errorf("expected Conn with ID %s to be removed, got %+v", c1.ID, c)
	}
}