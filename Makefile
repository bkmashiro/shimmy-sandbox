BINARY  := shimmy-sandbox
BINDIR  := bin
CMDPKG  := ./cmd/$(BINARY)

.PHONY: all build build-host test vet clean lambda-layer

all: build

## build — cross-compile static linux/amd64 binary (default target)
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags="-s -w" -o $(BINDIR)/$(BINARY) $(CMDPKG)

## build-host — build for the current host OS (for local testing)
build-host:
	go build -o $(BINDIR)/$(BINARY)-host $(CMDPKG)

## test — run all tests
test:
	go test ./...

## vet — run go vet
vet:
	go vet ./...

## clean — remove build artifacts
clean:
	rm -rf $(BINDIR) lambda-layer.zip

## lambda-layer — package the static binary into a Lambda layer zip
##   Place DynamoRIO at /opt/dynamorio/ and filter.so at /opt/sandbox/syscall_filter.so
##   on the Lambda filesystem (see README for full instructions).
lambda-layer: build
	@mkdir -p lambda-layer/bin
	cp $(BINDIR)/$(BINARY) lambda-layer/bin/$(BINARY)
	@printf 'shimmy-sandbox Lambda Layer\n\nContents:\n  bin/shimmy-sandbox  — sandbox executor (linux/amd64 static)\n\nTo include DynamoRIO:\n  Add /opt/dynamorio/ (full DynamoRIO distribution) and\n  /opt/sandbox/syscall_filter.so (your DynamoRIO client) to a second layer,\n  then set:\n    DYNAMORIO_HOME=/opt/dynamorio\n    SHIMMY_SANDBOX_FILTER_SO=/opt/sandbox/syscall_filter.so\n  in your Lambda environment.\n' \
		> lambda-layer/README.txt
	cd lambda-layer && zip -r ../lambda-layer.zip .
	rm -rf lambda-layer
	@echo "Created lambda-layer.zip"
