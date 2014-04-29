// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import "sync"

type clientSet struct {
	sync.RWMutex
	clients map[string]*Conn
}

func newClientSet() *clientSet {
	return &clientSet{
		clients: map[string]*Conn{},
	}
}

func (c *clientSet) get(id string) *Conn {
	c.RLock()
	defer c.RUnlock()
	return c.clients[id]
}

func (c *clientSet) set(id string, s *Conn) {
	c.Lock()
	c.clients[id] = s
	c.Unlock()
}

func (c *clientSet) remove(id string) {
	c.Lock()
	delete(c.clients, id)
	c.Unlock()
}

func (c *clientSet) len() int {
	c.RLock()
	defer c.RUnlock()
	return len(c.clients)
}

func (c *clientSet) reap() {
	c.RLock()
	toDelete := []string{}
	for k, s := range c.clients {
		if s.readyState == readyStateClosed {
			toDelete = append(toDelete, k)
		}
	}
	c.RUnlock()
	c.Lock()
	for _, k := range toDelete {
		delete(c.clients, k)
	}
	c.Unlock()
}
