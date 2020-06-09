// This is a cue file with the schema for the gofer configuration file.
// It can be used to validate that the configuration file is reasonable and
// well formed.
//
// Example:
//   cue vet /etc/gofer.schema.cue /etc/gofer.yaml

control_addr?: string

reqlog?:
	[string]: close({
		file:     string
		bufsize?: number
		format?:  string
	})

http?:
	[string]: close(_http)

https?:
	[string]: close(_http & {
		certs: string
	})

_http: {
	dir?: [string]: string

	static?: [string]: string

	proxy?: [string]: string

	redirect?: [string]: string

	cgi?: [string]: string

	auth?: [string]: string

	setheader?: [string]: [string]: string

	diropts?: [string]: #diropts

	reqlog?: [string]: string
}

#diropts:: {
	listing?: [string]: bool

	exclude?: [string]
}

raw?:
	[string]: close({
		certs?:  string
		to:      string
		to_tls?: bool
		reqlog?: string
	})
