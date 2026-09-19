# Quire — development entry points.
#
# The Go toolchain is found the same way the build scripts find it (mise
# installs are not on PATH for non-interactive shells).
GO := $(shell build/go-path.sh)

.PHONY: all check test vet fmt qml rmpp pc install icon clean

all: check rmpp

## check: everything CI would run
check: fmt vet test qml

## qml: lint ui/ and instantiate every screen offscreen (skips without Qt 6)
qml:
	build/qml-check.sh

# The `quiretest` tag compiles the fixture server's integration tests, which
# need fetch's test-only loopback exemption (backend/fetch/loopback_quiretest.go).
# No production target passes it, so the exemption is not in a shipped binary.
# The untagged run below is what proves that: it builds the production shape.
test:
	$(GO) test -tags quiretest ./...
	$(GO) build ./...

vet:
	$(GO) vet ./...
	$(GO) vet -tags quiretest ./...

## fmt: fail if anything is unformatted, rather than silently rewriting
fmt:
	@out=$$($(dir $(GO))gofmt -l backend build); \
	  if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

## rmpp: build the device bundle into output-rmpp/
rmpp:
	build/build-rmpp.sh

## pc: build a host bundle into output/ for development
pc:
	build/build-pc.sh

## install: deploy output-rmpp/ to the device (override with DEVICE=...)
install: rmpp
	build/install-device.sh $(DEVICE)

## icon: regenerate icon.png
icon:
	$(GO) run ./build/icon -o icon.png

clean:
	rm -rf output output-rmpp
