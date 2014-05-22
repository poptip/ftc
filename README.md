FTC [![Build Status](https://travis-ci.org/poptip/ftc.svg?branch=master)](https://travis-ci.org/poptip/ftc) [![GoDoc](https://godoc.org/github.com/poptip/ftc?status.png)](https://godoc.org/github.com/poptip/ftc)
=========
FTC (fault tolerant connection) is an [engine.io][0]-compatible library that provides fault tolerant, persistent client-server connections.

A basic echo server is shown below and is compatible with the [engine-io example][1].
```go
package main

import (
	"io"
	"log"
	"net/http"

	"github.com/poptip/ftc"
)

func EchoServer(c *ftc.Conn) {
	io.Copy(c, c)
}

func main() {
	http.Handle("/engine.io/", ftc.NewServer(nil, ftc.Handler(EchoServer)))
	log.Println("Serving at localhost:5000...")
	log.Fatal(http.ListenAndServe(":5000", nil))
}
```

[0]: https://github.com/LearnBoost/engine.io-protocol
[1]: https://github.com/LearnBoost/engine.io/tree/master/examples/latency
