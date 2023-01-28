#!/bin/bash

set -e

. $(dirname ${0})/lib.sh
init

# Run from the repo root.
cd ../

# Cover dir is given as the only arg.
COVERDIR="${1}"

echo "## Coverage"

# Merge the reports.
go tool covdata merge -i "${COVERDIR}/go,${COVERDIR}/sh" -o "${COVERDIR}/all"

# Export to the old format.
go tool covdata textfmt -i "${COVERDIR}/all" -o "${COVERDIR}/merged.out"

# Generate reports based on the merged output.
go tool cover -func="${COVERDIR}/merged.out" | sort -k 3 -n \
	> "${COVERDIR}/func.txt"
go tool cover -html="${COVERDIR}/merged.out" -o "${COVERDIR}/coverage.html"

TOTAL=$(cat ${COVERDIR}/func.txt | grep "total:" | awk '{print $3}')
echo
echo "Total:" $TOTAL
echo
echo "Coverage report can be found in:"
echo file://${COVERDIR}/coverage.html
