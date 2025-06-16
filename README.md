# HTTP Proxy Logger

HTTP Proxy Logger is a small reverse proxy that prints incoming HTTP requests
and outgoing responses to stdout. Bodies compressed with `gzip` or `deflate`
are automatically decompressed in the logs so that you can easily inspect them.
The output uses ANSI colors similar to `HTTPie`: request and response lines,
header names, and JSON or XML bodies are highlighted for readability.

## Example output

```http
[2021/05/05 03:50:44] --- REQUEST 3 ---

POST /mocking/contacts HTTP/1.1
Host: demo7704619.mockable.io
User-Agent: PostmanRuntime/7.28.0
Content-Length: 63
Accept: */*
Accept-Encoding: gzip, deflate, br
Cache-Control: no-cache
Content-Type: application/json
X-Forwarded-For: 172.17.0.1

{
    "firstName": "Stanislav",
    "lastName": "Deviatov"
}

[2021/05/05 03:50:44] --- RESPONSE 3 (201 Created) ---

HTTP/1.1 201 Created
Content-Length: 68
Access-Control-Allow-Origin: *
Content-Type: application/json; charset=UTF-8
Date: Wed, 05 May 2021 03:50:45 GMT
Server: Google Frontend
X-Cloud-Trace-Context: 83ac5937ae7ba8f3ef96ee941227b1b0

{
  "salesforceId": "a0C3L0000008ZSNUA2",
  "action": "updated"
}
```

## Building

The project requires **Go 1.23**.

### Build a binary

```bash
go build -o http-proxy-logger
```

### Build a Docker image

```bash
docker build -t stn1slv/http-proxy-logger .
```

## Running

Set the `TARGET` environment variable to the upstream server and optionally
`PORT` for the listen address. These values can also be provided with the
`-target` and `-port` flags which override the environment.

Use the `-requests` and `-responses` flags to control which messages are
printed. Both default to `true`.

### Local execution

```bash
./http-proxy-logger -target http://example.com -port 8888 -responses=false
```

### Docker

```bash
docker run --rm -it -p 8888:8888 \
  stn1slv/http-proxy-logger \
  -target http://demo7704619.mockable.io \
  -port 8888
```
Add `-responses=false` to log only requests or `-requests=false` to log only
responses. Flags `-target` and `-port` may be used instead of the corresponding
environment variables.

The proxy will forward traffic to the target and log each request/response pair
using the format shown above.

## License

This project is licensed under the MIT License.
