#!/bin/bash

set -e

. $(dirname ${0})/util/lib.sh
init


echo "## Setup"

build

# Remove old request log files, since we will be checking their contents.
rm -f .01-fe.requests.log .01-be.requests.log

# Make sure we don't accidentally use this from the caller.
unset CACERT

# Launch the backend serving static files and CGI.
gofer_bg -v=1 -logfile=.01-be.log -configfile=01-be.yaml
BE_PID=$PID
wait_until_ready 8450

# Launch the frontend. Tell it to accept the generated cert as a valid root.
generate_certs
SSL_CERT_FILE=".certs/localhost/fullchain.pem" \
    gofer_bg -v=1 -logfile=.01-fe.log -configfile=01-fe.yaml
FE_PID=$PID
wait_until_ready 8441  # http
wait_until_ready 8442  # https (cert files)
wait_until_ready 8443  # https (autocert)
wait_until_ready 8445  # raw

snoop

#
# Test cases.
#
echo "## Tests"

echo "### Config"

curl -sS "http://127.0.0.1:8440/debug/config" > .fe-debug-conf
if ! gofer -configfile=.fe-debug-conf -configcheck; then
	echo "Failed to parse FE config from monitoring output"
	exit 1
fi

curl -sS "http://127.0.0.1:8459/debug/config" > .be-debug-conf
if ! gofer -configfile=.be-debug-conf -configcheck; then
	echo "Failed to parse BE config from monitoring output"
	exit 1
fi

# Use gofer to print the parsed config. Strip out the coverage summary output
# (if present), which unfortunately cannot be disabled.
../gofer -configfile=01-be.yaml -configprint \
	| grep -v -E '^(PASS|coverage: .* of statements in .*)$' \
	> .be-print-conf
if ! diff -q testdata/expected-printed-01-be-config .be-print-conf; then
	echo "Printed config is different than expected:"
	diff -u testdata/expected-printed-01-be-config .be-print-conf
	exit 1
fi

if ! gofer -configfile=.be-print-conf -configcheck; then
	echo "Failed to parse BE config from -configprint"
	exit 1
fi

if gofer -configfile=does-not-exist; then
	echo "Expected error on a non-existing config"
	exit 1
fi

if gofer -configfile=bad-conf-1.yaml; then
	echo "bad config 1: expected error exit"
	exit 1
fi
if ! waitgrep -q "invalid configuration" .out.log; then
	echo "bad config 1: expected 'invalid configuration'"
	exit 1
fi

if gofer -configfile=bad-conf-2.yaml; then
	echo "bad config 2: Expected error exit"
	exit 1
fi
if ! waitgrep -q "reqlog \"log\" failed to initialize:" .out.log; then
	echo "bad config 2: Expected 'reqlog "log" failed to initialize'"
	exit 1
fi

# Common tests, for both servers.
for base in \
	http://localhost:8441 \
	https://localhost:8442 ;
do
	echo "### Common tests for $base"
	exp $base/file -body "ñaca\n"

	exp $base/dir -status 301 -redir /dir/

	exp $base/dir/ -bodyre '<a href="%C3%B1aca">ñaca</a>'
	exp $base/dir/ -bodyre '<a href="%23anchor">#anchor</a>'
	exp $base/dir/ -bodyre '<a href="%3Fquery">\?query</a>'
	exp $base/dir/ -bodyre '<a href="%3Ca%3E">&lt;a&gt;</a>'
	exp $base/dir/ -bodyre '>withindex/<'
	exp $base/dir/ -bodyre '>withoutindex/<'
	exp $base/dir/ -bodynotre 'ignored'

	exp $base/dir/hola -body 'hola marola\n'
	exp $base/dir/ñaca -body "tracañaca\n"
	exp $base/dir/ignored.file -status 404
	exp $base/dir/ñaca/ -status 301 -redir '../%C3%B1aca'
	exp "$base/dir/%23anchor/?abc" -status 301 -redir '../%23anchor?abc'

	exp $base/dir/withindex -status 301 -redir withindex/
	exp $base/dir/withindex/index.html -status 301 -redir ./
	exp $base/dir/withindex/ -bodyre 'This is the index.'
	exp $base/dir/withoutindex -status 404
	exp $base/dir/withoutindex/ -status 404
	exp $base/dir/withoutindex/chau -body 'chau\n'

	exp $base/cgi/ -bodyre '"param 1" "param 2"'
	exp $base/cgi/lala -bodyre '"param 1" "param 2"'
	exp "$base/cgi/?cucu=melo&a=b" -bodyre 'QUERY_STRING=cucu=melo&a=b\n'
	exp "$base/cgiwithq/?cucu=melo&a=b" \
			-bodyre 'QUERY_STRING=x=1&y=2&cucu=melo&a=b\n'

	# The proxy will strip parts of the query when using ";" by default, for
	# safety reasons.
	exp "$base/cgi/?cucu=melo&a;b" -bodyre 'QUERY_STRING=cucu=melo\n'

	exp $base/gogo/ -status 307 -redir https://google.com/
	exp $base/gogo/gaga -status 307 -redir https://google.com/gaga
	exp $base/gogo/a/b/ -status 307 -redir https://google.com/a/b/

	exp $base/bad/unreacheable -status 502
	exp $base/bad/empty -status 502

	exp $base/status/543 -status 543

	# Test that the FE doesn't forward this - it exists on the BE, but the
	# route doesn't end in a / so it shouldn't be forwarded.
	exp $base/file/second -status 404

	# Interesting case because neither has a trailing "/", so check that
	# the striping is done correctly.
	exp $base/file/ -status 404

	# Files in authdir/; only some are covered by auth.
	exp $base/authdir/hola -body 'hola marola\n'
	exp $base/authdir/ñaca -status 401
	exp $base/authdir/withoutindex -status 301
	exp $base/authdir/withoutindex/ -status 401
	exp $base/authdir/withoutindex/chau -status 401

	# Additional headers.
	exp $base/file -hdrre "X-My-Header: my lovely header"
done


# Good auth.
for base in \
	http://oneuser:onepass@localhost:8441 \
	https://twouser:twopass@localhost:8442 ;
do
	echo "### Good auth for $base"
	exp $base/authdir/hola -body 'hola marola\n'
	exp $base/authdir/ñaca -body "tracañaca\n"
	exp $base/authdir/withoutindex -status 301
	exp $base/authdir/withoutindex/ -status 404
	exp $base/authdir/withoutindex/chau -body 'chau\n'
done


# Bad auth.
for base in \
	http://oneuser:bad@localhost:8441 \
	http://unkuser:bad@localhost:8441 \
	http://twouser:bad@localhost:8441 ;
do
	echo "### Bad auth for $base"
	exp $base/authdir/hola -body 'hola marola\n'
	exp $base/authdir/ñaca -status 401
	exp $base/authdir/withoutindex -status 301
	exp $base/authdir/withoutindex/ -status 401
	exp $base/authdir/withoutindex/chau -status 401
done


echo "### Forwarding headers"
exp http://localhost:8441/cgi/ -bodyre 'HTTP_X_FORWARDED_HOST=localhost:8441\n'
exp http://localhost:8441/cgi/ -bodyre 'HTTP_X_FORWARDED_FOR='
exp http://localhost:8441/cgi/ -bodyre 'HTTP_HOST=localhost:8450\n'
exp http://localhost:8441/cgi/ -bodyre 'HTTP_X_FORWARDED_PROTO=http\n'
exp http://localhost:8441/cgi/ \
		-bodyre 'HTTP_FORWARDED=for=".+";host="localhost:8441";proto=http\n'
exp https://localhost:8442/cgi/ -bodyre 'HTTP_X_FORWARDED_PROTO=https\n'


echo "### Autocert"
# Launch the test ACME server.
acmesrv &
wait_until_ready 8460

# exp takes the CA cert from this variable.
# It is generated by acmesrv on startup.
CACERT=".acmesrv.cert"

# miau.com is what we configure the frontend to serve and request a cert for.
base="https://miau.com:8443"

exp $base/file -forcelocalhost -body "ñaca\n"
exp $base/dir/ñaca -forcelocalhost -body "tracañaca\n"

# Request for a domain not in our list, check that the request is denied, and
# also that we log it properly.
exp "https://unknown-ac:8443/file" -forcelocalhost \
	-clienterrorre "tls: internal error"
if ! waitgrep \
	-q 'request for "unknown-ac" -> acme/autocert:' \
	.01-fe.log;
then
	echo "autocert error was not logged properly"
	exit 1
fi

unset CACERT


echo "### Request log"
function logtest() {
	exp http://localhost:8441/cgi/logtest
	for f in .01-be.requests.log .01-fe.requests.log; do
		EXPECT='localhost:84.. GET /cgi/logtest "" "Go-http-client/1.1" = 200'
		if ! waitgrep -q "$EXPECT" $f; then
			echo "$f: log entry not found"
			exit 1
		fi
	done
}

# Check that the entry appears.
logtest

# Log rotation.
mv .01-fe.requests.log .01-fe.requests.log.old
mv .01-be.requests.log .01-be.requests.log.old
kill -HUP $FE_PID $BE_PID

# Expect the entry again, and make sure it's the only one.
logtest
for f in .01-be.requests.log .01-fe.requests.log; do
	if [ "$(wc -l < $f)" != 1 ]; then
		echo "$f: unexpected number of entries"
		exit 1
	fi
done


echo "### Miscellaneous"

# HTTPS-only tests.
exp https://localhost:8442/dar/ -bodyre '<a href="%C3%B1aca">ñaca</a>'

# We rely on the BE having this, so check to avoid false positives due to
# misconfiguration.
exp http://localhost:8450/file/second -body "tracañaca\n"

# Debug handler.
exp "http://127.0.0.1:8459/" -bodyre "gofer @"

# Check that the debug / handler only serves /.
exp "http://127.0.0.1:8459/notexists" -status 404

# Rate-limiting debug handler.
exp "http://127.0.0.1:8440/debug/ratelimit" -bodyre "Allow: 1 / 1s"

echo "### Raw proxying"
exp http://localhost:8445/file -body "ñaca\n"
exp https://localhost:8446/file -body "ñaca\n"
exp http://localhost:8448/file -body "ñaca\n"

true < /dev/tcp/localhost/8447
if ! waitgrep -q ":8447 = 500" .01-fe.requests.log; then
	echo "raw connection to :8447: error entry not found"
	exit 1
fi


echo "### Rate limiting (http)"
# First request must be allowed.
exp http://localhost:8441/rlme/0 -status 200

# Somewhere in these, we should start to get rejected (likely from the
# beginning, but there could be timing issues).
for i in `seq 1 3`; do
	exp http://localhost:8441/rlme/$i -statuslist 200,429
done

# By this stage, they should all be rejected.
for i in `seq 4 6`; do
	exp http://localhost:8441/rlme/$i -status 429
done


echo "### Rate limiting (raw)"
# Because these are raw proxies, we don't get nice HTTP status on rejections,
# so we count errors instead.
# We give it a rate of 1/1s, and perform 6 requests in quick succession.
# Expect at least 1 success and 3 errors.
NSUCCESS=0
NERR=0
for i in `seq 1 6`; do
	if exp http://localhost:8449/file >> .exp-raw-rl.log 2>&1; then
		NSUCCESS=$(( NSUCCESS + 1 ))
	else
		NERR=$(( NERR + 1 ))
	fi
done
if [ $NSUCCESS -lt 1 ] || [ $NERR -lt 3 ]; then
	echo "expected >=1 successes and >=3 errors, but" \
		"got $NSUCCESS successes and $NERR errors"
	exit 1
fi

# Snoop here because the next script will kill the test servers.
snoop

echo "### Checking examples from doc/examples.md"
./util/check-examples.sh


echo "## Success"
snoop
