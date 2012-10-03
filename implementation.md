# Design #

## Why parse client http request ##

Of course we need to parse http request to know the address of the web server.

Besides, HTTP requests sent to proxy servers are a little different from those sent directly to the web servers. So proxy server need to reconstruct http request

- Normal HTTP 1.1 `GET` request has request URI like '/index.html', but when sending to proxy, it would be something like 'host.com/index.html'
- The `CONNECT` request requires special handling by the proxy (send a 200 back to the client)

## Parse http response or not? ##

The initial implementation serves client request one by one. For each request:

1. Parse client HTTP request
2. Connect to the server and send the request, send the response back to the client

We need to know whether a response is finished so we can start to serve another request. (This is the oppisite to HTTP pipelining.) That's why we need to parse content-length header and chunked encoding.

Parsing responses allow the proxy to put server connections back to a pool, thus allows different clients to reuse server connections.

After supporting `CONNECT`, I realized that I can use a separate goroutine to read HTTP response from the server and pass it directly back to the client. This approach doesn't need to parse response to know when the response ends and then starts to process another request.

**Update: not parsing HTTP response do have some problems.** Refer to section "But response parsing is necessary".

This approach has several implications needs to be considered:

- The proxy doesn't know whether the web server closes the connection by setting the header "Connection: close"
  - This should not be a big problem because web server should use persistent connection normally
- And this header is passed directly to the client which would close it's connection to the proxy (even though the proxy didn't close this connection)
  - Even if the closed connection header is passed to the client, the client can simply create a new connection to the proxy and the proxy will detect the closed client connection
- The server connection can only serve a single client connection. Because we don't know the boundary of responses, the proxy is unable to identify different responses and sends to different clients
  - This means that multiple clients connecting to the same server has to create different server connections
  - We have to create multiple connection to the same server to reduce latency any way, but makes it impossible to reuse server connection for different clients

### Why choose not parse ###

I choosed not parsing the response because:

- Associating client with dedicated server connection is simpler in implementation
  - As client could create multiple proxy connections to concurrently issue requests to reduce latency, the proxy can allow only a single connection to different web servers and thus connection pool is not needed
- Not parsing the response reduces overhead
  - Need additional goroutine to handle response, so hard to say this definitely has better performance
  - If we are going to support HTTP pipelining, we may still need to handle response in separate goroutine

### But response parsing is necessary ###

I've got a bug in handling HTTP response 302 when not parsing the response.

When trying to visit "youku.com", it gives a "302" response with "Connection: close". The browser doesn't close the connection and still tries to get more content from the server after seeing the response.

I tried polipo and see it will send back "302" response along with a "Content-Length: 0" to indicate the client that the response has finished.

To add this kind of response editing capability for my proxy, I have to parse HTTP response.

So the current solution is to parse the response in the a separate goroutine, which doesn't require lots of code change against the not parsing approach.
