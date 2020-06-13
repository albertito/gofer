#!/bin/bash

set -e

. $(dirname ${0})/lib.sh
init

# Run from the repo root.
cd ../

echo "## Coverage"

# Merge all coverage output into a single file.
# Ignore protocol buffer-generated files, as they are not relevant.
go run "${UTILDIR}/gocovcat.go" "${COVER_DIR}"/*.out \
> "${COVER_DIR}/all.out"

# Generate reports based on the merged output.
go tool cover -func="$COVER_DIR/all.out" | sort -k 3 -n > "$COVER_DIR/func.txt"
go tool cover -html="$COVER_DIR/all.out" -o "$COVER_DIR/coverage.html"

TOTAL=$(cat .cover/func.txt | grep "total:" | awk '{print $3}')
echo
echo "Total:" $TOTAL
echo
echo "Coverage report can be found in:"
echo file://$COVER_DIR/coverage.html
