// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import "sync"

// A clientSet represents a pool of connections keyed off
// of their IDs.
type clientSet struct {
	sync.RWMutex
	clients map[string]*Conn
}

// newClientSet allocates and returns a new clientSet.
func newClientSet() *clientSet {
	return &clientSet{clients: map[string]*Conn{}}
}

// get returns the connection with the given ID, nil otherwise.
func (c *clientSet) get(id string) *Conn {
	c.RLock()
	defer c.RUnlock()
	return c.clients[id]
}

// add adds a connection to the set keyed off its ID field.
func (c *clientSet) add(conn_ *Conn) {
	c.Lock()
	c.clients[conn_.ID] = conn_
	c.Unlock()
}

// remove removes a connection from the set.
func (c *clientSet) remove(conn_ *Conn) {
	c.Lock()
	delete(c.clients, conn_.ID)
	c.Unlock()
}

// len returns the number of connections in the set.
// The connections may be open or closed.
func (c *clientSet) len() int {
	c.RLock()
	defer c.RUnlock()
	return len(c.clients)
}

// reap iterates through the set and removes any closed
// connections.
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
