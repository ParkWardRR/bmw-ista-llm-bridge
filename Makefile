.PHONY: all build clean nim nim-report nim-enet nim-import nim-context gleam zig go test vet lint fmt ci check

all: build

build: go nim

go:
	GOOS=windows go build -o ista-bridge.exe .

nim: nim-report nim-enet nim-import nim-context

nim-report:
	cd polyglot/nim/report_gen && nim c -d:release -o:ista-report.exe report_gen.nim

nim-enet:
	cd polyglot/nim/enet_client && nim c -d:release -d:ssl -o:ista-enet.exe enet_client.nim

nim-import:
	cd polyglot/nim/data_import && nim c -d:release -o:ista-import.exe data_import.nim

nim-context:
	cd polyglot/nim/context_builder && nim c -d:release -o:ista-context.exe context_builder.nim

gleam:
	cd polyglot/gleam/fault_lookup && gleam build

zig:
	cd polyglot/zig/data_processor && zig build -Doptimize=ReleaseSafe

clean:
	rm -f ista-bridge.exe
	rm -f polyglot/nim/report_gen/ista-report.exe
	rm -f polyglot/nim/enet_client/ista-enet.exe
	rm -f polyglot/nim/data_import/ista-import.exe
	rm -f polyglot/nim/context_builder/ista-context.exe
	rm -rf polyglot/nim/report_gen/nimcache
	rm -rf polyglot/nim/enet_client/nimcache
	rm -rf polyglot/nim/data_import/nimcache
	rm -rf polyglot/nim/context_builder/nimcache
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
	@if [ -f polyglot/nim/enet_client/ista-enet.exe ]; then \
		polyglot/nim/enet_client/ista-enet.exe --help > /dev/null && echo "  nim-enet: OK" || echo "  nim-enet: FAIL"; \
	else echo "  nim-enet: not built (skipped)"; fi
	@if [ -f polyglot/nim/data_import/ista-import.exe ]; then \
		polyglot/nim/data_import/ista-import.exe --help > /dev/null && echo "  nim-import: OK" || echo "  nim-import: FAIL"; \
	else echo "  nim-import: not built (skipped)"; fi
	@if [ -f polyglot/nim/context_builder/ista-context.exe ]; then \
		polyglot/nim/context_builder/ista-context.exe --help > /dev/null && echo "  nim-context: OK" || echo "  nim-context: FAIL"; \
	else echo "  nim-context: not built (skipped)"; fi
	@if [ -d polyglot/gleam/fault_lookup/build ]; then \
		cd polyglot/gleam/fault_lookup && gleam test > /dev/null 2>&1 && echo "  gleam: OK" || echo "  gleam: FAIL"; \
	else echo "  gleam: not built (skipped)"; fi

# CI: full local CI pipeline — run this before committing
ci: lint test check
	@echo ""
	@echo "=== Local CI passed ==="
