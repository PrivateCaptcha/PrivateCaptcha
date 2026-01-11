#!/bin/bash

# Generate HTML coverage report from Go coverage profiles
# This script merges unit and integration test coverage, filters excluded paths,
# and generates an annotated source code HTML report.
#
# Usage: ./docker/generate-coverage-html.sh [output_dir]
#
# Inputs (expected in repository root):
#   - coverage_unit.cov: Unit test coverage
#   - coverage_reports/*.cov: Integration test coverage files
#
# Output:
#   - coverage_html/coverage.html: Single HTML file with annotated source code
#
# Exclusions (matching sonar.coverage.exclusions):
#   - pkg/db/migrations/**
#   - pkg/db/queries/**
#   - widget/**
#   - web/**
#   - pkg/db/generated/**
#   - cmd/**
#   - pkg/db/tests/**
#   - pkg/portal/tests/**

set -e

OUTPUT_DIR="${1:-coverage_html}"
MERGED_COV="$OUTPUT_DIR/merged.cov"
FILTERED_COV="$OUTPUT_DIR/filtered.cov"
OUTPUT_HTML="$OUTPUT_DIR/coverage.html"

mkdir -p "$OUTPUT_DIR"

echo "=== Collecting coverage files ==="
coverage_files=()

if [ -f "coverage_unit.cov" ]; then
    echo "Found: coverage_unit.cov"
    coverage_files+=("coverage_unit.cov")
fi

for cov_dir in "coverage_integration" "coverage_reports"; do
    if [ -d "$cov_dir" ]; then
        any_found=false
        for f in ${cov_dir}/*.cov; do
            if [ -f "$f" ]; then
                echo "Found: $f"
                coverage_files+=("$f")
                any_found=true
            fi
        done

        $any_found && break || echo "$cov_dir does not contain files"
    fi
done

if [ ${#coverage_files[@]} -eq 0 ]; then
    echo "Error: No coverage files found"
    exit 1
fi

echo ""
echo "=== Merging ${#coverage_files[@]} coverage file(s) ==="

# Extract mode from first file
mode=$(head -1 "${coverage_files[0]}" | grep "^mode:")
if [ -z "$mode" ]; then
    echo "Error: No mode line found in ${coverage_files[0]}"
    exit 1
fi

# Output mode line
echo "$mode" > "$MERGED_COV"

# Merge all coverage data (excluding mode lines)
# For duplicate entries (same location and statement count), take max hit count
grep -h -v "^mode:" "${coverage_files[@]}" | sort | awk '
{
    # Format: file:startLine.startCol,endLine.endCol stmtCount hitCount
    location = $1
    stmtCount = $2
    hitCount = $3
    
    # Key is location + stmtCount
    key = location " " stmtCount
    
    if (key in counts) {
        # Take max hit count for duplicate entries
        if (hitCount > counts[key]) {
            counts[key] = hitCount
        }
    } else {
        counts[key] = hitCount
        order[++n] = key
    }
}
END {
    for (i = 1; i <= n; i++) {
        print order[i] " " counts[order[i]]
    }
}
' >> "$MERGED_COV"

merged_lines=$(wc -l < "$MERGED_COV")
echo "Merged coverage: $merged_lines lines"

echo ""
echo "=== Filtering excluded paths ==="

# Keep mode line
head -1 "$MERGED_COV" > "$FILTERED_COV"

# Filter out excluded paths (matching sonar.coverage.exclusions)
tail -n +2 "$MERGED_COV" | grep -v \
    -e '/pkg/db/migrations/' \
    -e '/pkg/db/queries/' \
    -e '/widget/' \
    -e '/web/' \
    -e '/pkg/db/generated/' \
    -e '/cmd/' \
    -e '/pkg/db/tests/' \
    -e '/pkg/portal/tests/' \
    >> "$FILTERED_COV"

filtered_lines=$(wc -l < "$FILTERED_COV")
echo "Filtered coverage: $filtered_lines lines"

echo ""
echo "=== Generating HTML report ==="
go tool cover -html="$FILTERED_COV" -o "$OUTPUT_HTML"

echo ""
echo "=== Coverage report generated ==="
echo "Output: $OUTPUT_HTML"
echo ""
echo "To view locally: open $OUTPUT_HTML in a browser"
echo ""
echo "Note: This report shows annotated source code with coverage highlighting."
echo "Green = covered, Red = not covered, Gray = not tracked"
