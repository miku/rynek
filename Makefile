SHELL = /bin/bash
PKGNAME = rynek

.PHONY: all build test race vet clean demo

all: build

build:
	go build ./...
	go build -o rynek ./cmd/rynek

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

# demo runs the bundled example pipeline into ./.data.
demo: build
	./rynek run Report -v
	@echo "---"
	@cat .data/report-static.txt

clean:
	rm -f rynek
	rm -rf .data
