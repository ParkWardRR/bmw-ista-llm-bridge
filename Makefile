.PHONY: all build clean nim gleam zig go test

all: build

build: go nim

go:
	go build -o ista-bridge.exe .

nim:
	cd polyglot/nim/report_gen && nim c -d:release -o:ista-report.exe report_gen.nim

gleam:
	cd polyglot/gleam/fault_lookup && gleam build

zig:
	cd polyglot/zig/data_processor && zig build -Doptimize=ReleaseSafe

clean:
	rm -f ista-bridge.exe
	rm -f polyglot/nim/report_gen/ista-report.exe
	rm -rf polyglot/nim/report_gen/nimcache
	rm -rf polyglot/gleam/fault_lookup/build
	rm -rf polyglot/zig/data_processor/zig-out

test:
	go vet ./...
	polyglot/nim/report_gen/ista-report.exe --help > /dev/null
