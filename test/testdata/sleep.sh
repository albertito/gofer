#!/bin/bash

set -e

sleep $1

echo "Content-type: text/plain"
echo
echo "Slept $1"

