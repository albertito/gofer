#!/bin/bash

set -e

. $(dirname ${0})/util/lib.sh
init


echo "## Setup"

build

# Remove old request log files, since we will be checking their contents.
rm -f .01-fe.requests.log .01-be.requests.log

# Set up a writable directory for the PUT/DELETE tests, with a seed file
# whose mtime is known so we can drive If-Unmodified-Since deterministically.
rm -rf .writedir .symlink-target
mkdir -p .writedir/readonly .symlink-target
echo -n "original" > .writedir/existing
touch -d "2020-01-01 00:00:00 UTC" .writedir/existing

# Symlink under the writable root pointing at a sibling directory (outside
# root). Used by the security tests to verify documented symlink-follow
# behaviour and that DELETE only removes the link, not its target.
ln -sfn "$(pwd)/.symlink-target" .writedir/linked

# Per-user directory tests start from a clean slate; the per-user
# subdirectories are created on the first PUT.
rm -rf .peruserdir

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

	exp $base/dir -status 307 -redir /dir/

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

	exp $base/bad/unreachable -status 502
	exp $base/bad/empty -status 502

	exp $base/status/543 -status 543

	# Regexp-based redirects.
	exp $base/rere/x -status 404
	exp $base/rere/a/bc/x -status 307 -redir /dst/a/bc/z
	exp $base/rere/a/b/x -status 404
	exp $base/rere/1/2/zzz/3/4 -status 308 \
			-redir http://example.com/dst/z/3/4/z/1/2

	# Test that the FE doesn't forward this - it exists on the BE, but the
	# route doesn't end in a / so it shouldn't be forwarded.
	exp $base/file/second -status 404

	# Interesting case because neither has a trailing "/", so check that
	# the striping is done correctly.
	exp $base/file/ -status 404

	# Files in authdir/; only some are covered by auth.
	exp $base/authdir/hola -body 'hola marola\n'
	exp $base/authdir/ñaca -status 401
	exp $base/authdir/withoutindex -status 307
	exp $base/authdir/withoutindex/ -status 401
	exp $base/authdir/withoutindex/chau -status 401

	# Additional headers.
	exp $base/file -hdrre "X-My-Header: my lovely header"

	# Timeouts.
	exp $base/timeout/10ms -status 200
	exp $base/timeout/100ms -status 200
	# The 500ms request times out, and this appears to the client as
	# either EOF (over HTTP) or a TLS error (over HTTPS).
	# This behaviours matches reaching the timeout of http.Server.
	exp $base/timeout/500ms \
		-clienterrorre "EOF|local error: tls: bad record MAC"
done


# Good auth.
for base in \
	http://oneuser:onepass@localhost:8441 \
	https://twouser:twopass@localhost:8442 ;
do
	echo "### Good auth for $base"
	exp $base/authdir/hola -body 'hola marola\n'
	exp $base/authdir/ñaca -body "tracañaca\n"
	exp $base/authdir/withoutindex -status 307
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
	exp $base/authdir/withoutindex -status 307
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


echo "### PUT / DELETE"

BE=http://localhost:8450

# PUT disabled on /readonly/ subprefix (longest-prefix match wins).
exp $BE/writable/readonly/x -method PUT -reqbody "no" -status 405
# DELETE is still enabled on /readonly/ (only PUT is denied there),
# but the target doesn't exist.
exp $BE/writable/readonly/x -method DELETE -status 404

# PUT a new file -> 201, content readable back. The response carries a
# Last-Modified validator for subsequent If-Unmodified-Since requests.
exp $BE/writable/new.txt -method PUT -reqbody "hello\n" -status 201 \
    -hdrre "Last-Modified: "
exp $BE/writable/new.txt -body "hello\n"

# PUT over an existing file -> 204, content replaced.
exp $BE/writable/new.txt -method PUT -reqbody "again\n" -status 204
exp $BE/writable/new.txt -body "again\n"

# PUT auto-creates parent directories.
exp $BE/writable/a/b/c.txt -method PUT -reqbody "deep\n" -status 201
exp $BE/writable/a/b/c.txt -body "deep\n"

# PUT onto an existing directory -> 409.
exp $BE/writable/a -method PUT -reqbody "nope" -status 409

# Excluded paths return 404 on writes too.
exp $BE/writable/secret.txt -method PUT -reqbody "x" -status 404
exp $BE/writable/secret.txt -method DELETE -status 404

# Empty-body PUT creates a zero-byte file.
exp $BE/writable/empty -method PUT -status 201
exp $BE/writable/empty -body ""

# Path traversal: net/http normalises the path. Depending on whether the
# client (or the mux) does the cleaning, we see either a 307 redirect to
# the canonical form or a direct 404. Either way, our handler is not
# called, so nothing is written outside the root.
exp "$BE/writable/../escaped" -method PUT -reqbody "x" -statuslist 307,404
[ ! -e escaped ] || { echo "traversal wrote outside root"; exit 1; }

# If-Unmodified-Since: stale (file mtime > header) -> 412, content unchanged.
exp $BE/writable/existing -method PUT \
    -reqhdr "If-Unmodified-Since: Wed, 01 Jan 2000 00:00:00 GMT" \
    -reqbody "should not write" -status 412
exp $BE/writable/existing -body "original"

# If-Unmodified-Since: in the future -> 204, content replaced.
exp $BE/writable/existing -method PUT \
    -reqhdr "If-Unmodified-Since: Thu, 01 Jan 2099 00:00:00 GMT" \
    -reqbody "updated" -status 204
exp $BE/writable/existing -body "updated"

# DELETE existing file -> 204; then GET -> 404.
exp $BE/writable/new.txt -method DELETE -status 204
exp $BE/writable/new.txt -status 404

# DELETE with stale If-Unmodified-Since -> 412, file still there.
exp $BE/writable/empty -method DELETE \
    -reqhdr "If-Unmodified-Since: Wed, 01 Jan 2000 00:00:00 GMT" \
    -status 412
exp $BE/writable/empty -status 200

# DELETE on a directory -> 409.
exp $BE/writable/a -method DELETE -status 409

# DELETE missing file -> 404.
exp $BE/writable/never -method DELETE -status 404

# PUT through the FE proxy works end-to-end.
FE=http://localhost:8441
exp $FE/writable/via-fe.txt -method PUT -reqbody "fe\n" -status 201
exp $FE/writable/via-fe.txt -body "fe\n"


echo "### PUT / DELETE security"

# URL-encoded "..": the encoded form is preserved on the wire (clients
# that pre-normalise may behave differently). The mux still routes
# /writable/ since the cleaned-but-still-decoded path starts with it; our
# handler then sees "/../escaped-encoded" which path.Clean turns into
# "/escaped-encoded" — INSIDE the root. Lock that in.
exp "$BE/writable/%2E%2E/escaped-encoded" -method PUT -reqbody "x" -status 201
[ -f .writedir/escaped-encoded ] || \
	{ echo "expected .writedir/escaped-encoded"; exit 1; }

# URL-encoded "../" (slash also encoded): URL.Path becomes /writable/../foo,
# the mux still routes /writable/ and our handler runs. path.Clean turns
# "/../foo" into "/foo", which lands INSIDE the root (no escape). Lock in
# this behaviour and confirm the file is under .writedir/.
exp "$BE/writable/%2e%2e%2fenc-slash" -method PUT -reqbody "x" -status 201
[ -f .writedir/enc-slash ] || \
	{ echo "expected .writedir/enc-slash"; exit 1; }

# Double-encoded "..": decoded once to literal "%2E%2E", treated as a
# normal filename segment (no traversal). File lands inside root with a
# literal name.
exp "$BE/writable/%252E%252E/double-enc" -method PUT -reqbody "x" -status 201
[ -f '.writedir/%2E%2E/double-enc' ] || \
	{ echo "expected literal %2E%2E dir"; exit 1; }

# Collapsed slashes: mux normalises and redirects to the canonical form.
exp "$BE/writable//collapsed" -method PUT -reqbody "x" -statuslist 301,307,308

# Prefix-toggle bypass attempt: "/readonly/../bypass" cleans to
# "/writable/bypass". Depending on the client, either:
#   - the mux sees the dirty path, cleans it and 307-redirects (no write);
#   - or the client pre-cleans the path and the canonical form is what
#     reaches the handler (PUT succeeds, allowed by the "/" rule).
# Either way the toggle works on the canonical path: the readonly prefix
# is never effective against a request that resolves to a non-readonly
# path. Lock that in (no escape either way).
exp "$BE/writable/readonly/../bypass" -method PUT -reqbody "x" \
	-statuslist 201,307
# No "bypass" file should have appeared above the root.
[ ! -e bypass ] || { echo "bypass leaked above root"; exit 1; }

# Excludes apply after URL-decoding.
exp "$BE/writable/%73ecret.txt" -method PUT -reqbody "x" -status 404
exp "$BE/writable/%73ecret.txt" -method DELETE -status 404

# Null bytes in the filename: rejected by the OS. We don't care about the
# exact status, only that nothing was written.
exp "$BE/writable/foo%00bar" -method PUT -reqbody "x" -statuslist 400,500

# Symlink follow (documented behaviour): PUT through a symlink-directory
# inside the root writes to the symlink target. Don't place such symlinks
# unless you want this.
exp $BE/writable/linked/via-symlink -method PUT \
	-reqbody "via-symlink" -status 201
[ -f .symlink-target/via-symlink ] || \
	{ echo "symlink follow broken"; exit 1; }

# DELETE on a symlink removes the link itself, not what it points to.
exp $BE/writable/linked -method DELETE -status 204
[ ! -e .writedir/linked ] || \
	{ echo "symlink should be gone"; exit 1; }
[ -f .symlink-target/via-symlink ] || \
	{ echo "symlink target should still exist"; exit 1; }

# Final invariant: every PUT we issued landed under .writedir/ or
# .symlink-target/. Walk the test directory for stray files that match the
# names we tried to write through traversal.
for f in escaped escaped-encoded bypass double-enc collapsed; do
	for d in . .. /tmp; do
		if [ -e "$d/$f" ]; then
			echo "FAIL: traversal wrote outside root: $d/$f exists"
			exit 1
		fi
	done
done


echo "### Per-user directories"

# Two users PUT the same request path; each lands in its own subdirectory.
exp http://oneuser:onepass@localhost:8450/peruser/f.txt \
    -method PUT -reqbody "one-data" -status 201
exp http://twouser:twopass@localhost:8450/peruser/f.txt \
    -method PUT -reqbody "two-data" -status 201
[ "$(cat .peruserdir/oneuser/f.txt)" = "one-data" ] || \
    { echo "oneuser per-user file wrong"; exit 1; }
[ "$(cat .peruserdir/twouser/f.txt)" = "two-data" ] || \
    { echo "twouser per-user file wrong"; exit 1; }

# Each user reads back their own copy through the same URL.
exp http://oneuser:onepass@localhost:8450/peruser/f.txt -body "one-data"
exp http://twouser:twopass@localhost:8450/peruser/f.txt -body "two-data"

# Without auth -> 401 (auth layer runs first), nothing written.
exp http://localhost:8450/peruser/anon.txt -method PUT -reqbody "x" -status 401
[ ! -e .peruserdir/anon.txt ] || { echo "anon per-user write leaked"; exit 1; }

# Depth: a deeper request path lands under the user's directory, with parent
# directories auto-created.
exp http://oneuser:onepass@localhost:8450/peruser/a/b/c.txt \
    -method PUT -reqbody "deep" -status 201
[ -f .peruserdir/oneuser/a/b/c.txt ] || \
    { echo "deep per-user file missing"; exit 1; }

# DELETE is per-user too: oneuser deleting does not affect twouser's copy.
exp http://oneuser:onepass@localhost:8450/peruser/f.txt -method DELETE -status 204
exp http://oneuser:onepass@localhost:8450/peruser/f.txt -status 404
exp http://twouser:twopass@localhost:8450/peruser/f.txt -body "two-data"

# Per-user PUT through the FE proxy works end-to-end (auth header forwarded).
exp http://oneuser:onepass@localhost:8441/peruser/via-fe.txt \
    -method PUT -reqbody "fe-data" -status 201
[ "$(cat .peruserdir/oneuser/via-fe.txt)" = "fe-data" ] || \
    { echo "per-user via-fe file wrong"; exit 1; }


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
