// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import "testing"

func TestClientSetBasic(t *testing.T) {
	s := &clientSet{clients: map[string]*conn{}}
	c1 := newConn()
	c2 := newConn()
	c3 := newConn()
	s.add(c1)
	s.add(c2)
	s.add(c3)
	if s.len() != 3 {
		t.Errorf("expected set length to be 3, was %d", s.len())
	}
	if s.get(c1.id) != c1 {
		t.Errorf("expected conn with ID %s, got %+v", c1.id, s.get(c1.id))
	}
	c3.Close()
	s.reap()
	c := s.get(c3.id)
	if c != nil || s.len() != 2 {
		t.Errorf("expected conn to be reaped due to being in closed state, got %+v", c)
	}
	s.remove(c1)
	c = s.get(c1.id)
	if c != nil || s.len() != 1 {
		t.Errorf("expected conn with ID %s to be removed, got %+v", c1.id, c)
	}
}

func TestAddingEmptyID(t *testing.T) {
	s := &clientSet{clients: map[string]*conn{}}
	c := newConn()
	c.id = ""
	s.add(c)
	if r := s.get(c.id); r != nil {
		t.Errorf("expected connection with empty id to not be added. got %+v", r)
	}
}
