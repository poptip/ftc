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
	clients map[string]*conn
}

// get returns the connection with the given ID, nil otherwise.
// empty ids are not supported and will always return nil.
func (c *clientSet) get(id string) *conn {
	c.RLock()
	defer c.RUnlock()
	return c.clients[id]
}

// add adds a connection to the set keyed off its ID field.
// If a conn has an empty ID, it is not added to the set.
func (c *clientSet) add(con *conn) {
	if len(con.id) == 0 {
		return
	}
	c.Lock()
	c.clients[con.id] = con
	c.Unlock()
}

// remove removes a connection from the set.
func (c *clientSet) remove(con *conn) {
	c.Lock()
	delete(c.clients, con.id)
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
		if s.closed {
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
