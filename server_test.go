// Copyright (c) 2014, Markover Inc.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.
// Source code and contact info at http://github.com/poptip/ftc

package ftc

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/golang/glog"
)

var (
	serverAddr string
	once       sync.Once
)

func startServer() {
	server := httptest.NewServer(NewServer(nil, nil))
	serverAddr = server.Listener.Addr().String()
	glog.Infoln("test server listening on", serverAddr)
}

func TestBadSID(t *testing.T) {
	once.Do(startServer)
	addr := "http://" + serverAddr + DefaultBasePath + "?transport=polling&sid=test"
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
