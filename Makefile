
ifndef VERSION
    VERSION = `git describe --always --long --dirty`
endif

# https://wiki.debian.org/ReproducibleBuilds/TimestampsProposal
ifndef SOURCE_DATE_EPOCH
    SOURCE_DATE_EPOCH = `git log -1 --format=%ct`
endif

default: gofer

gofer:
	go build -ldflags="\
		-X main.version=${VERSION} \
		-X main.sourceDateTs=${SOURCE_DATE_EPOCH} \
		" ${GOFLAGS}

test:
	go test ./...
	setsid -w ./test/test.sh

.PHONY: gofer test
