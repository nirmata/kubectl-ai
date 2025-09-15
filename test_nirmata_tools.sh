#!/bin/bash

# Test script for Nirmata tool calling issues
# This script runs all the tests to validate the tool calling problems and fixes

set -e

echo "============================================"
echo "Nirmata Tool Calling Test Suite"
echo "============================================"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo -e "${RED}Error: go.mod not found. Please run from project root.${NC}"
    exit 1
fi

# Function to run a test with formatting
run_test() {
    local test_name=$1
    local test_file=$2
    local test_func=$3

    echo -e "${BLUE}Running: ${test_name}${NC}"
    echo "----------------------------------------"

    if [ -z "$test_func" ]; then
        # Run all tests in the file
        go test -v ./gollm -run "^Test.*" -count=1 -timeout 30s $test_file 2>&1 | grep -E "PASS|FAIL|Error|Issue|Fix" || true
    else
        # Run specific test function
        go test -v ./gollm -run "^${test_func}$" -count=1 -timeout 30s 2>&1 | grep -E "PASS|FAIL|Error|Issue|Fix" || true
    fi

    echo ""
}

# Function to check environment
check_environment() {
    echo -e "${YELLOW}Checking environment...${NC}"

    # Check for API key
    if [ -z "$NIRMATA_API_KEY" ]; then
        echo -e "${YELLOW}Warning: NIRMATA_API_KEY not set. Integration tests will be skipped.${NC}"
        echo "To run integration tests, set: export NIRMATA_API_KEY=your_key"
    else
        echo -e "${GREEN}✓ NIRMATA_API_KEY is set${NC}"
    fi

    # Check for endpoint
    if [ -z "$NIRMATA_ENDPOINT" ]; then
        echo "Info: NIRMATA_ENDPOINT not set. Using default: https://api.nirmata.io"
    else
        echo -e "${GREEN}✓ NIRMATA_ENDPOINT is set: $NIRMATA_ENDPOINT${NC}"
    fi

    echo ""
}

# Main test execution
main() {
    check_environment

    echo -e "${YELLOW}=== UNIT TESTS ===${NC}"
    echo ""

    # Test 1: Basic parsing tests
    echo -e "${BLUE}Test 1: Tool Call Parsing${NC}"
    echo "Testing Issue #1: Backend sends plain text instead of JSON"
    echo "Testing Issue #2: Parse errors hidden at debug level"
    go test -v ./gollm -run "TestNirmataToolCallParsing" -count=1 2>&1 | grep -v "^go:" | head -30
    echo ""

    # Test 2: Error visibility
    echo -e "${BLUE}Test 2: Error Visibility${NC}"
    echo "Testing that parse errors are visible to users"
    go test -v ./gollm -run "TestErrorVisibility" -count=1 2>&1 | grep -v "^go:" | head -20
    echo ""

    # Test 3: Debug tests
    echo -e "${BLUE}Test 3: Debug Output${NC}"
    echo "Detailed debugging of all issues"
    go test -v ./gollm -run "TestDebugToolCallParsing" -count=1 2>&1 | grep -E "Issue|Related|Description|Parse|Tool|Error" | head -40
    echo ""

    # Test 4: Contract documentation
    echo -e "${BLUE}Test 4: Backend Contract${NC}"
    echo "Documentation of expected vs actual format"
    go test -v ./gollm -run "TestDebugBackendContract" -count=1 2>&1 | grep -v "^go:" | head -30
    echo ""

    if [ ! -z "$NIRMATA_API_KEY" ]; then
        echo -e "${YELLOW}=== INTEGRATION TESTS ===${NC}"
        echo ""

        # Test 5: Live streaming test
        echo -e "${BLUE}Test 5: Live Streaming with Tools${NC}"
        echo "Testing actual API calls with tool requests"
        go test -v ./gollm -run "TestNirmataStreamingWithTools" -count=1 -timeout 60s 2>&1 | grep -E "PASS|FAIL|Tool|Error" | head -20
        echo ""

        # Test 6: Tool forwarding
        echo -e "${BLUE}Test 6: Tool Call Forwarding${NC}"
        echo "Testing Issue #4: Backend not forwarding LLM tool calls"
        go test -v ./gollm -run "TestToolCallForwarding" -count=1 -timeout 30s 2>&1 | grep -E "PASS|FAIL|Issue|forward" | head -15
        echo ""
    else
        echo -e "${YELLOW}Skipping integration tests (no API key)${NC}"
        echo ""
    fi

    echo -e "${YELLOW}=== FIX VALIDATION ===${NC}"
    echo ""

    # Show the fixes needed
    echo -e "${BLUE}Required Fixes:${NC}"
    go test -v ./gollm -run "TestDebugFixImplementation" -count=1 2>&1 | grep -E "Fix|File|ADD|REPLACE" | head -50
    echo ""

    # Show workaround
    echo -e "${BLUE}Workaround:${NC}"
    go test -v ./gollm -run "TestWorkaroundValidation" -count=1 2>&1 | grep -E "Workaround|provider" | head -10
    echo ""

    echo -e "${YELLOW}=== SUMMARY ===${NC}"
    echo ""

    # Generate summary
    go test -v ./gollm -run "TestGenerateBugReport" -count=1 2>&1 | grep -v "^go:" | grep -v "^PASS" | head -30
    echo ""

    echo "============================================"
    echo -e "${GREEN}Test suite complete!${NC}"
    echo "============================================"
}

# Function to run a specific issue test
test_issue() {
    local issue=$1

    case $issue in
        1)
            echo -e "${YELLOW}Testing Issue #1: Backend sends plain text instead of JSON${NC}"
            go test -v ./gollm -run "TestNirmataToolCallParsing|TestDebugBackendContract" -count=1
            ;;
        2)
            echo -e "${YELLOW}Testing Issue #2: Parse errors hidden at debug level${NC}"
            go test -v ./gollm -run "TestErrorVisibility|TestDebugErrorVisibility" -count=1
            ;;
        3)
            echo -e "${YELLOW}Testing Issue #3: Arguments parsing${NC}"
            go test -v ./gollm -run "TestArgumentsParsing|TestDebugArgumentFormats" -count=1
            ;;
        4)
            echo -e "${YELLOW}Testing Issue #4: LLM tool calls not forwarded${NC}"
            go test -v ./gollm -run "TestToolCallForwarding|TestDebugLLMToolCallFlow" -count=1
            ;;
        5)
            echo -e "${YELLOW}Testing Issue #5: Provider parameter forcing${NC}"
            go test -v ./gollm -run "TestProviderRouting|TestProviderParameterIssue" -count=1
            ;;
        *)
            echo -e "${RED}Unknown issue number: $issue${NC}"
            echo "Valid issues: 1, 2, 3, 4, 5"
            exit 1
            ;;
    esac
}

# Function to run quick validation
quick_test() {
    echo -e "${YELLOW}Running quick validation...${NC}"
    echo ""

    # Just run the parsing tests
    go test -v ./gollm -run "TestNirmataToolCallParsing" -count=1 2>&1 | grep -E "PASS|FAIL|Issue"

    echo ""
    echo -e "${YELLOW}Quick validation complete${NC}"
}

# Parse command line arguments
case "${1:-}" in
    issue)
        if [ -z "$2" ]; then
            echo "Usage: $0 issue <number>"
            echo "Example: $0 issue 1"
            exit 1
        fi
        test_issue $2
        ;;
    quick)
        quick_test
        ;;
    help|--help|-h)
        echo "Usage: $0 [command]"
        echo ""
        echo "Commands:"
        echo "  (no args)    Run full test suite"
        echo "  quick        Run quick validation tests"
        echo "  issue <n>    Test specific issue (1-5)"
        echo "  help         Show this help message"
        echo ""
        echo "Environment variables:"
        echo "  NIRMATA_API_KEY    API key for integration tests"
        echo "  NIRMATA_ENDPOINT   API endpoint (default: https://api.nirmata.io)"
        echo ""
        echo "Examples:"
        echo "  $0                    # Run all tests"
        echo "  $0 quick              # Run quick tests"
        echo "  $0 issue 1            # Test issue #1"
        echo ""
        ;;
    *)
        main
        ;;
esac