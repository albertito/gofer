
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
		-X blitiri.com.ar/go/gofer/debug.Version=${VERSION} \
		-X blitiri.com.ar/go/gofer/debug.SourceDateTs=${SOURCE_DATE_EPOCH} \
		" ${GOFLAGS}

vet: config/gofer.yaml test/01-be.yaml test/01-fe.yaml
	go vet ./...
	cue vet config/gofer.schema.cue $^

test: vet
	go test ./...
	setsid -w ./test/test.sh

cover:
	rm -rf .cover/
	mkdir .cover/
	go test -tags coverage \
		-covermode=count \
		-coverprofile=".cover/pkg-tests.out"\
		-coverpkg=./... ./...
	COVER_DIR=$$PWD/.cover/ setsid -w ./test/test.sh
	COVER_DIR=$$PWD/.cover/ setsid -w ./test/util/cover-report.sh

.PHONY: gofer vet test cover
