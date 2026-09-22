.PHONY: all build clean nim nim-report nim-enet nim-import nim-context gleam zig go test vet lint fmt ci check

all: build

build: go nim

go:
	GOOS=windows go build -o ista-bridge.exe .

nim: nim-report nim-enet nim-import nim-context

nim-report:
	cd tools/nim/report_gen && nim c -d:release -o:ista-report.exe report_gen.nim

nim-enet:
	cd tools/nim/enet_client && nim c -d:release -d:ssl -o:ista-enet.exe enet_client.nim

nim-import:
	cd tools/nim/data_import && nim c -d:release -o:ista-import.exe data_import.nim

nim-context:
	cd tools/nim/context_builder && nim c -d:release -o:ista-context.exe context_builder.nim

gleam:
	cd tools/gleam/fault_lookup && gleam build

zig:
	cd tools/zig/data_processor && zig build -Doptimize=ReleaseSafe

clean:
	rm -f ista-bridge.exe
	rm -f tools/nim/report_gen/ista-report.exe
	rm -f tools/nim/enet_client/ista-enet.exe
	rm -f tools/nim/data_import/ista-import.exe
	rm -f tools/nim/context_builder/ista-context.exe
	rm -rf tools/nim/report_gen/nimcache
	rm -rf tools/nim/enet_client/nimcache
	rm -rf tools/nim/data_import/nimcache
	rm -rf tools/nim/context_builder/nimcache
	rm -rf tools/gleam/fault_lookup/build
	rm -rf tools/zig/data_processor/zig-out

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
	@if [ -f tools/nim/report_gen/ista-report.exe ]; then \
		tools/nim/report_gen/ista-report.exe --help > /dev/null && echo "  nim:  OK" || echo "  nim:  FAIL"; \
	else echo "  nim:  not built (skipped)"; fi
	@if [ -f tools/nim/enet_client/ista-enet.exe ]; then \
		tools/nim/enet_client/ista-enet.exe --help > /dev/null && echo "  nim-enet: OK" || echo "  nim-enet: FAIL"; \
	else echo "  nim-enet: not built (skipped)"; fi
	@if [ -f tools/nim/data_import/ista-import.exe ]; then \
		tools/nim/data_import/ista-import.exe --help > /dev/null && echo "  nim-import: OK" || echo "  nim-import: FAIL"; \
	else echo "  nim-import: not built (skipped)"; fi
	@if [ -f tools/nim/context_builder/ista-context.exe ]; then \
		tools/nim/context_builder/ista-context.exe --help > /dev/null && echo "  nim-context: OK" || echo "  nim-context: FAIL"; \
	else echo "  nim-context: not built (skipped)"; fi
	@if [ -d tools/gleam/fault_lookup/build ]; then \
		cd tools/gleam/fault_lookup && gleam test > /dev/null 2>&1 && echo "  gleam: OK" || echo "  gleam: FAIL"; \
	else echo "  gleam: not built (skipped)"; fi

# CI: full local CI pipeline — run this before committing
ci: lint test check
	@echo ""
	@echo "=== Local CI passed ==="
