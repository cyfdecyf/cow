# Design #

## Requst and response handling ##

**Update** using the following design, it is actually difficult to correctly support HTTP pipelining. I've come up with a new design inspired by Naruil which should be much cleaner and easier to support HTTP pipelining. But as all major browsers, except Opera, does not enable HTTP pipelining by default, I don't think it's worth the effort to support HTTP pipelining now. I'll try to support it with the new design if the performance benefits of HTTP pipelining becomes significant in the future.

The final design is evolved from different previous implementations. The other subsections following this one describe how its evolved.

COW uses separate goroutines to read client requests and server responses.

- For each client, COW will create one *request goroutine* to
  - accept client request (read from client connection)
  - create connection if no one not exist
  - send request to the server (write to server connection)
- For each server connection, there will be an associated *response goroutine*
  - reading response from the web server (read from server connection)
  - send response back to the client (write to client connection)

One client must have one request goroutine, and may have multiple response goroutine. Response goroutine is created when the server connection is created.

This makes it possible for COW to support HTTP pipeline. (Not very sure about this.) COW does not pack multiple requests and send in batch, but it can send request before previous request response is received. If the client (browser) and the web server supports HTTP pipeline, then COW will not in effect make them go back to wating response for each request.

But this design does make COW more complicated. I must be careful to avoid concurrency problems between the request and response goroutine.

Here's things that worth noting:

- The web server connection for each host is stored in a map
  - The request goroutine creates the connection and put it into this map
  - When serving requests, this map will be be used to find already created server connections
  - We should avoid writing the map in the response goroutine. So when response goroutine finishes, it should just mark the corresonding connection as closed instead of directly removing it from the map

- Request and response goroutine may need to notify each other to stop
  - When client connection is closed, all response goroutine should stop
  - Client connection close can be detected in both request and response goroutine (as both will try to either read or write the connection), to make things simple, I just do notification in the request goroutine

## Notification between goroutines

- Notification sender should not block
  - I use a size 1 channel for this as the notification will be sent only once
- Receiver use polling to handle notification
  - For blocked calls, should set time out to actively poll notification

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

# About supporting auto refresh #

When blocked sites are detected because of error like connection resets and read time out, we can choose to redo the HTTP request by using parent proxy or just return error page and let the browser refresh.

I tried to support auto refresh. But as I want support HTTP pipelining, the client request and server response read are in separate goroutine. The response reading goroutine need to send redo request to the client request goroutine and maintain a correct request handling order. The resulting code is very complex and difficult to maintain. Besides, the extra code to support auto refresh may incur performance overhead.

As blocked sites will be recorded, the refresh is only needed for the first access to a blocked site. Auto refresh is just a minor case optimization.

So I choose not to support auto refresh as the benefit is small.

# Error printing policy #

The goal is **make it easy to find the exact error location**.

- Error should be printed as early as possible
- If an error happens in a function which will be invoked at multiple places, print the error at the call site
