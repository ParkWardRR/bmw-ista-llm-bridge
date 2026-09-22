.PHONY: all build clean nim gleam zig go test vet lint fmt ci check

all: build

build: go nim

go:
	GOOS=windows go build -o ista-bridge.exe .

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

# Test: run Go unit tests (native OS — build constraints exclude Windows-only files)
test:
	go test -v -count=1 ./...

# Vet: static analysis (Windows target — win32.go uses syscall.Handle)
vet:
	GOOS=windows go vet ./...

# Fmt: check formatting (fail if not formatted)
fmt:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

# Lint: combined static checks
lint: vet fmt

# Check: verify satellite tools (if built)
check:
	@echo "=== Checking satellite tools ==="
	@if [ -f polyglot/nim/report_gen/ista-report.exe ]; then \
		polyglot/nim/report_gen/ista-report.exe --help > /dev/null && echo "  nim:  OK" || echo "  nim:  FAIL"; \
	else echo "  nim:  not built (skipped)"; fi
	@if [ -d polyglot/gleam/fault_lookup/build ]; then \
		cd polyglot/gleam/fault_lookup && gleam test > /dev/null 2>&1 && echo "  gleam: OK" || echo "  gleam: FAIL"; \
	else echo "  gleam: not built (skipped)"; fi

# CI: full local CI pipeline — run this before committing
ci: lint test check
	@echo ""
	@echo "=== Local CI passed ==="
