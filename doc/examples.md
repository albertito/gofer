
# [gofer](https://blitiri.com.ar/git/r/gofer) configuration examples

See the [reference config](../config/gofer.yaml) for the full list of options.


## Static HTTP server

Static HTTP server, on port `8080`, serving the contents of `/srv/www/`.

```yaml
http:
  ":8080":
    routes:
      "/":
        dir: "/srv/www/"
```


## Virtual domains

HTTP server with different virtual domains, and redirection from
`www.domain.com` to `domain.com`.

```yaml
http:
  ":80":
    routes:
      "cats.com/":
        dir: "/srv/cats/www/"
      "www.cats.com/":
        redirect: "http://cats.com/"

      "elephants.com/":
        dir: "/srv/elephants/www/"
      "www.elephants.com/":
        redirect: "http://elephants.com/"
```


## HTTPS server with autocerts and HSTS

Static HTTPS server, with automatic SSL certificates (obtained via
[Let's Encrypt](https://letsencrypt.org)), and enabling
[HSTS](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Strict-Transport-Security).

```yaml
https:
  ":443":
    routes:
      "/":
        dir: "/srv/www/"
      "www.example.com/":
        redirect: "https://example.com/"

    autocerts:
      hosts: ["example.com", "www.example.com"]
      email: "me@example.com"

    setheader:
      "/":
        "Strict-Transport-Security": "max-age=63072000;"
```

## Request logging

Write request logs to `/var/log/gofer/requests.log`.

```yaml
reqlog:
  "requests.log":
    file: "/var/log/gofer/requests.log"
    bufsize: 16

http:
  ":80":
    reqlog:
      "/": "requests.log"
    routes:
      "/":
        dir: "/srv/www/"
```

## Reverse HTTP proxy

Proxy `http://example.com/api/` requests to another server running on
`http://localhost:8080/`.

```yaml
http:
  ":80":
    routes:
      "example.com/":
        dir: "/srv/www/"

      "example.com/api/":
        proxy: "http://localhost:8080/"
```

## Built-in monitoring server

gofer comes with a built-in monitoring HTTP server, for debugging and
troubleshooting. \
Do **not** expose it to the internet, it will leak a lot of information.

```yaml
control_addr: "127.0.0.1:8081"

http:
  ":8080":
    routes:
      "/":
        dir: "/srv/www/"
```


## Raw proxy with TLS termination

Listen for TLS connections on port 995, and proxy it to `127.0.0.1:110`.

```yaml
raw:
  ":995":
    certs: "/etc/letsencrypt/live/"
    to: "127.0.0.1:110"
```
