#!/bin/bash

set -e

. $(dirname ${0})/../util/lib.sh
init

# Merge all CSVs into one.
echo -n "server,size,duration,requests," > .perf-out/all.csv
echo -n "bytes,errors,reqps,byteps," >> .perf-out/all.csv
echo -n "latmean,lat50,lat90,lat99,lat99.9," >> .perf-out/all.csv
echo "lat99.99,lat99.999," >> .perf-out/all.csv
for d in gofer nginx; do
	for s in 1k 10k 100k 250k 500k 1M 10M; do
		echo "$d,$s,`tail -n 1 .perf-out/$d-$s.csv`" \
			>> .perf-out/all.csv
	done
done

# Graph.
python3 perf/graph.py

echo "file://$PWD/.perf-out/results.html"
