
function init() {
	if [ "$V" == "1" ]; then
		set -v
	fi

	export UTILDIR=$( realpath `dirname "${BASH_SOURCE[0]}"` )
	export TESTDIR=$( realpath "$UTILDIR"/../ )
	cd ${TESTDIR}

	# Set traps to kill our subprocesses when we exit (for any reason).
	trap ":" TERM      # Avoid the EXIT handler from killing bash.
	trap "exit 2" INT  # Ctrl-C, make sure we fail in that case.
	trap "kill 0" EXIT # Kill children on exit.
}

function build() {
	if [ "$COVER_DIR" != "" ]; then
		(
			cd ..
			go test -covermode=count -coverpkg=./... \
				-c -tags coveragebin
			mv gofer.test gofer
		)
	else
		( cd ..; make )
	fi
	( cd util; go build exp.go )
}

function set_cover() {
	# Set the coverage arguments each time, as we don't want the different
	# runs to override the generated profile.
	if [ "$COVER_DIR" != "" ]; then
		COVER_ARGS="-test.run=^TestRunMain$ \
			-test.coverprofile=$COVER_DIR/it-`date +%s.%N`.out"
	fi
}

function gofer() {
	set_cover
	../gofer $COVER_ARGS -v=3  "$@"  >> .out.log 2>&1
}

# Run gofer in the background (sets $PID to its process id).
function gofer_bg() {
	# Duplicate gofer() because if we put the function in the background,
	# the pid will be of bash, not the subprocess.
	set_cover
	../gofer $COVER_ARGS -v=3  "$@"  >> .out.log 2>&1 &
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

function waitgrep() {
	for i in 0.01 0.02 0.05 0.1 0.2; do
		if grep "$@"; then
				return 0
		fi
		sleep $i
	done
	return 1	
}

