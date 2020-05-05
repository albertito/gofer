#!/bin/bash

set -e

if [ "$V" == "1" ]; then
	set -v
fi

UTILDIR="$( realpath `dirname "${0}"` )/util"

# Set traps to kill our subprocesses when we exit (for any reason).
trap ":" TERM      # Avoid the EXIT handler from killing bash.
trap "exit 2" INT  # Ctrl-C, make sure we fail in that case.
trap "kill 0" EXIT # Kill children on exit.

# The tests are run from the test root.
cd "$(realpath `dirname ${0}`)/"

# Build the binaries.
if [ "$COVER_DIR" != "" ]; then
	(
		cd ..
		go test -covermode=count -coverpkg=./... -c -tags coveragebin
		mv gofer.test gofer
	)
else
	( cd ..; go build )
fi
( cd util; go build exp.go )


# Run gofer in the background (sets $PID to its process id).
function gofer() {
	# Set the coverage arguments each time, as we don't want the different
	# runs to override the generated profile.
	if [ "$COVER_DIR" != "" ]; then
		COVER_ARGS="-test.run=^TestRunMain$ \
			-test.coverprofile=$COVER_DIR/it-`date +%s.%N`.out"
	fi

	$SYSTEMD_ACTIVATE ../gofer $COVER_ARGS \
		-v=3 \
		"$@" >> .out.log 2>&1 &
	PID=$!
}

# Wait until there's something listening on the given port.
function wait_until_ready() {
	PORT=$1

	while ! bash -c "true < /dev/tcp/localhost/$PORT" 2>/dev/null ; do
		sleep 0.01
	done
}

function generate_certs() {
	mkdir -p .certs/localhost
	(
		cd .certs/localhost
		go run ${UTILDIR}/generate_cert.go \
			-ca -duration=1h --host=localhost
	)
}

function curl() {
	curl --cacert ".certs/localhost/fullchain.pem" "$@"
}

function exp() {
	if [ "$V" == "1" ]; then
		VF="-v"
	fi
	echo "  $@"
	${UTILDIR}/exp "$@" \
		$VF \
		-cacert=".certs/localhost/fullchain.pem"
}

function snoop() {
	if [ "$SNOOP" == "1" ]; then
		read -p"Press enter to continue"
	fi
}

echo "## Setup"

# Launch the backend serving static files and CGI.
gofer -logfile=.01-be.log -configfile=01-be.conf
DIR_PID=$PID
wait_until_ready 8450

# Launch the test instance.
generate_certs
gofer -logfile=.01-fe.log -configfile=01-fe.conf
wait_until_ready 8441  # http
wait_until_ready 8442  # https
wait_until_ready 8445  # raw

snoop

#
# Test cases.
#
echo "## Tests"

# Common tests, for both servers.
for base in \
	http://localhost:8441 \
	https://localhost:8442 ;
do
	exp $base/file -body "ñaca\n"

	exp $base/dir -status 301 -redir /dir/
	exp $base/dir/ -bodyre '<a href="%C3%B1aca">ñaca</a>'
	exp $base/dir/hola -body 'hola marola\n'
	exp $base/dir/ñaca -body "tracañaca\n"

	exp $base/cgi/ -bodyre '"param 1" "param 2"'
	exp "$base/cgi/?cucu=melo&a;b" -bodyre 'QUERY_STRING=cucu=melo&a;b\n'

	exp $base/gogo/ -status 307 -redir https://google.com/
	exp $base/gogo/gaga -status 307 -redir https://google.com/gaga
	exp $base/gogo/a/b/ -status 307 -redir https://google.com/a/b/

	exp $base/bad/unreacheable -status 502
	exp $base/bad/empty -status 502

	# Test that the FE doesn't forward this - it exists on the BE, but the
	# route doesn't end in a / so it shouldn't be forwarded.
	exp $base/file/second -status 404

	# Interesting case because neither has a trailing "/", so check that
	# the striping is done correctly.
	exp $base/file/ -status 404
done

# HTTPS-only tests.
exp https://localhost:8442/dar/ -bodyre '<a href="%C3%B1aca">ñaca</a>'

# We rely on the BE having this, so check to avoid false positives due to
# misconfiguration.
exp http://localhost:8450/file/second -body "tracañaca\n"

# Raw proxying.
exp http://localhost:8445/file -body "ñaca\n"
exp https://localhost:8446/file -body "ñaca\n"

snoop
