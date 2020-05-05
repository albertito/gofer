#!/bin/bash

set -e

echo "Content-type: text/plain"
echo

echo -n ARGS:
for i in "$@"; do
	echo -n " \"$i\""
done
echo

echo
env | sort

