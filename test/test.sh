#!/bin/bash

set -e

. $(dirname ${0})/util/lib.sh
init


echo "## Setup"

build

# Remove old request log files, since we will be checking their contents.
rm -f .01-fe.requests.log .01-be.requests.log

# Launch the backend serving static files and CGI.
gofer_bg -v=3 -logfile=.01-be.log -configfile=01-be.yaml
BE_PID=$PID
wait_until_ready 8450

# Launch the test instance.
generate_certs
gofer_bg -v=3 -logfile=.01-fe.log -configfile=01-fe.yaml
FE_PID=$PID
wait_until_ready 8441  # http
wait_until_ready 8442  # https
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

gofer -configfile=01-be.yaml -configprint > .be-print-conf
if diff -q 01-be.yaml .be-print-conf; then
	echo "Printed and debug configs differ:"
	diff 01-be.yaml .be-print-conf
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
	exp $base/dir/ -bodyre '>withindex/<'
	exp $base/dir/ -bodyre '>withoutindex/<'
	exp $base/dir/ -bodynotre 'ignored'

	exp $base/dir/hola -body 'hola marola\n'
	exp $base/dir/ñaca -body "tracañaca\n"
	exp $base/dir/ignored.file -status 404

	exp $base/dir/withindex -status 301 -redir withindex/
	exp $base/dir/withindex/ -bodyre 'This is the index.'
	exp $base/dir/withoutindex -status 404
	exp $base/dir/withoutindex/ -status 404
	exp $base/dir/withoutindex/chau -body 'chau\n'

	exp $base/cgi/ -bodyre '"param 1" "param 2"'
	exp $base/cgi/lala -bodyre '"param 1" "param 2"'
	exp "$base/cgi/?cucu=melo&a;b" -bodyre 'QUERY_STRING=cucu=melo&a;b\n'

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


echo "### Request log"
function logtest() {
	exp http://localhost:8441/cgi/logtest
	for f in .01-be.requests.log .01-fe.requests.log; do
		EXPECT='localhost:8441 GET /cgi/logtest "" "Go-http-client/1.1" = 200'
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

# Check that the debug / handler only serves /.
exp "http://127.0.0.1:8459/notexists" -status 404


echo "### Raw proxying"
exp http://localhost:8445/file -body "ñaca\n"
exp https://localhost:8446/file -body "ñaca\n"

echo "## Success"
snoop
