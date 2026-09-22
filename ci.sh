#!/usr/bin/env bash
#
# ci.sh — standalone local CI pipeline for bmw-ista-llm-bridge
# Run this before committing. Exit code is non-zero on any failure.
#

set -euo pipefail

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BOLD='\033[1m'
RESET='\033[0m'

PASS=0
FAIL=0
OVERALL_START=$(date +%s)

step() {
    echo ""
    echo -e "${BOLD}--- $1 ---${RESET}"
}

pass() {
    echo -e "${GREEN}PASS${RESET}: $1"
    PASS=$((PASS + 1))
}

fail() {
    echo -e "${RED}FAIL${RESET}: $1"
    FAIL=$((FAIL + 1))
}

# --- 1. go vet (Windows target — win32.go uses syscall.Handle) ---
step "go vet"
if GOOS=windows go vet ./... 2>&1; then
    pass "go vet"
else
    fail "go vet"
fi

# --- 2. gofmt ---
step "gofmt"
UNFORMATTED=$(gofmt -l . 2>&1 || true)
if [ -z "$UNFORMATTED" ]; then
    pass "gofmt"
else
    echo "Files need formatting:"
    echo "$UNFORMATTED"
    fail "gofmt"
fi

# --- 3. go build (Windows target) ---
step "go build"
if GOOS=windows go build ./... 2>&1; then
    pass "go build"
else
    fail "go build"
fi

# --- 4. go test (native OS — build constraints exclude Windows-only files) ---
step "go test"
if go test -v -count=1 ./... 2>&1; then
    pass "go test"
else
    fail "go test"
fi

# --- 5. Satellite tool checks ---
step "Satellite tools"

if [ -f polyglot/nim/report_gen/ista-report.exe ]; then
    if polyglot/nim/report_gen/ista-report.exe --help > /dev/null 2>&1; then
        pass "nim satellite"
    else
        fail "nim satellite"
    fi
else
    echo -e "${YELLOW}SKIP${RESET}: nim satellite (not built)"
fi

if [ -d polyglot/gleam/fault_lookup/build ]; then
    if (cd polyglot/gleam/fault_lookup && gleam test > /dev/null 2>&1); then
        pass "gleam satellite"
    else
        fail "gleam satellite"
    fi
else
    echo -e "${YELLOW}SKIP${RESET}: gleam satellite (not built)"
fi

# --- Summary ---
OVERALL_END=$(date +%s)
ELAPSED=$((OVERALL_END - OVERALL_START))

echo ""
echo -e "${BOLD}=============================${RESET}"
if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}${BOLD}  LOCAL CI FAILED${RESET}"
    echo -e "  ${GREEN}${PASS} passed${RESET}, ${RED}${FAIL} failed${RESET} (${ELAPSED}s)"
    echo -e "${BOLD}=============================${RESET}"
    exit 1
else
    echo -e "${GREEN}${BOLD}  LOCAL CI PASSED${RESET}"
    echo -e "  ${GREEN}${PASS} passed${RESET}, 0 failed (${ELAPSED}s)"
    echo -e "${BOLD}=============================${RESET}"
    exit 0
fi
