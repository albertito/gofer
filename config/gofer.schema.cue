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

ratelimit?:
	[string]: close({
		rate:  string
		size?: number

		rate64?: string
		rate56?: string
		rate48?: string
	})

http?:
	[string]: close(#http)

https?:
	[string]: close(#http & {
		certs?: string

		autocerts?: {
			hosts: [string, ...string]
			cachedir?: string
			email?:    string
			acmeurl?:  string
		}
	})

#http: {
	routes: [string]: {
		dir?:      string
		file?:     string
		proxy?:    string
		redirect?: string
		cgi?: [string, ...string]
		status?: int
		redirect_re?: [#redirect_re, ...#redirect_re]

		// TODO: Check that only one of the above is set.

		diropts?: {
			listing?: [string]: bool
			exclude?: [string]
		}

		// If diropts is set, then dir must be set too.
		if diropts != _|_ {
			dir: string
		}
	}

	auth?: [string]: string

	setheader?: [string]: [string]: string

	reqlog?: [string]: string

	ratelimit?: [string]: string

	...
}

#redirect_re: {
	from: string
	to: string
	status?: int
}

raw?:
	[string]: close({
		certs?:  string
		to:      string
		to_tls?: bool
		reqlog?: string
		ratelimit?: string
	})
