
default: gofer

# Pass version and source date info if available on the $VERSION and
# $SOURCE_DATE_EPOCH environment variables; we will get them from Go's build
# info infrastructure otherwise.
# https://wiki.debian.org/ReproducibleBuilds/TimestampsProposal

gofer:
	go build ${GOFLAGS}

vet: config/gofer.yaml etc/gofer.yaml test/01-be.yaml test/01-fe.yaml
	go vet ./...
	cue vet config/gofer.schema.cue $^

test: vet
	go test ./...
	setsid -w ./test/test.sh

cover:
	rm -rf .cover/
	mkdir -p .cover/go .cover/sh .cover/all
	go test -tags coverage \
		-covermode=count \
		-coverpkg=./... ./... \
		-args -test.gocoverdir=$$PWD/.cover/go/
	GOCOVERDIR=$$PWD/.cover/sh/ setsid -w ./test/test.sh
	setsid -w ./test/util/cover-report.sh $$PWD/.cover/

install: gofer
	install -D -b -p gofer /usr/local/bin
	install -d /etc /etc/systemd/system/ /etc/logrotate.d/
	cp -n etc/gofer.yaml /etc/
	cp -n etc/systemd/system/gofer.service /etc/systemd/system/
	cp -n etc/logrotate.d/gofer /etc/logrotate.d/

.PHONY: gofer vet test cover install
