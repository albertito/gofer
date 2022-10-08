#!/bin/bash

set -e

. $(dirname ${0})/lib.sh
init


HEADER=""
EXAMPLE=""
IN_EXAMPLE=n
while IFS="" read LINE; do
	if [ "$LINE" == '```yaml' ]; then
		# begin example
		IN_EXAMPLE=y
	elif [ "$LINE" == '```' ]; then
		# end example
		echo "  $HEADER"
		printf "$EXAMPLE" > /tmp/gofer-example.yaml
		cue vet ../config/gofer.schema.cue /tmp/gofer-example.yaml

		HEADER=""
		EXAMPLE=""
		IN_EXAMPLE=n
	elif [ "$IN_EXAMPLE" == "y" ]; then
		# append to current example
		EXAMPLE="$EXAMPLE\n$LINE"
	elif [[ "$LINE" =~ ^\#+\  ]]; then
		# found a header
		HEADER=$( echo "$LINE" | sed "s/^\#\+ //g" )
	fi
done < ../doc/examples.md
