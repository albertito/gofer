#!/bin/bash

set -e

. $(dirname ${0})/../util/lib.sh
init

function nginx_bg() {
	nginx -c perf/nginx.conf -p $PWD &
	PID=$!
}

export DURATION=${DURATION:-5s}

function runwrk() {
	wrk -t 1 -c 1 -d $DURATION -s perf/report.lua "$@"
}

echo "## Performance"

echo "### Setup"

GOMAXPROCS=2 gofer_bg -logfile=.perf.log -configfile=perf/gofer.yaml
GOFER_PID=$PID
wait_until_ready 8450

nginx_bg
NGINX_PID=$PID
wait_until_ready 8077

rm -rf .perf-out/
mkdir -p .perf-out/

snoop

for s in 1k 10k 100k 250k 500k 1M 10M; do
	echo "### Size: $s"
	truncate -s $s testdata/dir/perf-$s

	echo "#### gofer"
	runwrk "http://localhost:8450/perf-$s"
	mv wrkout.csv .perf-out/gofer-$s.csv
	echo
	snoop

	echo "#### nginx"
	runwrk "http://localhost:8077/perf-$s"
	mv wrkout.csv .perf-out/nginx-$s.csv
	echo
	snoop
done

echo "### Analysis"
perf/analysis.sh
