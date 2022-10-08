
# gofer

[gofer](https://blitiri.com.ar/git/r/gofer) is a small web server and reverse
proxy, written in Go.


## Status

[![github tests](https://github.com/albertito/gofer/actions/workflows/tests.yaml/badge.svg)](https://github.com/albertito/gofer/actions)
[![coverage](https://coveralls.io/repos/github/albertito/gofer/badge.svg?branch=next)](https://coveralls.io/github/albertito/gofer?branch=next)

Gofer is under active development, and breaking changes are expected.
It is fully functional and being used to serve some small websites.


## Install

To install from source, you'll need the [Go](https://golang.org/) compiler.

```sh
# Clone the repository.
git clone https://blitiri.com.ar/repos/gofer

# Build the binary and install basic config files.
cd gofer; sudo make install

# Start the server.
sudo systemctl start gofer
```


## Configure

Configuration lives in `/etc/gofer.yaml` by default.

See the [reference config](config/gofer.yaml) for details on how to configure
gofer, and what features are available.

There are also [practical configuration examples](doc/examples.md) that cover
the most common use cases.


## Contact

If you have any questions, comments or patches please send them to
albertito@blitiri.com.ar.

