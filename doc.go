package ftc

/**
FTC takes a hybrid approach to persistent connections to the server
by providing a base-level connection method (XHR long polling) and
allowing the client to "upgrade" the connection.

Heavily inspired and compatible with engine.io, an initial client/server
connection looks like this.

+ Client makes a GET request with the specified transport being the only
GET parameter.
+ Server creates a new connection over the transport specified in the
initial request and returns the pertinent info to the client such as
the session ID which is then used for subsequent requests on the
connection.
+ If the connection can be upgraded, the client concurrently makes an
attempt to connect using one of the upgraded transports (WebSockets).
+ If that upgrade process is successful, the server finishes sending
any messages on the low-grade method and begins to send over the
upgraded protocol.

Since both the websocket.Conn satisfies net.Conn which satisfies the
ReadWriteCloser interface, the underlying connection can be merely
a composite of an io.ReadWriteCloser plus some additional metadata.
*/
