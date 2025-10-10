#!/bin/bash

# Automated spike detection during integration test
# Usage: ./scripts/test-with-spike-detection.sh "your test command"

set -e

TEST_COMMAND="${1}"
OUTPUT_DIR="./spike-analysis-$(date +%Y%m%d-%H%M%S)"
PPROF_HOST="localhost:6060"

if [ -z "$TEST_COMMAND" ]; then
    echo "Usage: $0 \"test command\""
    echo "Example: $0 \"go test -v ./test/e2e/...\""
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

echo "=== Automated Memory Spike Detection ==="
echo "Test command: $TEST_COMMAND"
echo "Output directory: $OUTPUT_DIR"
echo ""

# Check if pprof endpoint is available
if ! curl -s "http://$PPROF_HOST/debug/pprof/" > /dev/null 2>&1; then
    echo "ERROR: pprof endpoint not available at http://$PPROF_HOST"
    echo "Make sure your application is running with pprof enabled"
    exit 1
fi

# Start background monitoring
echo "Starting background monitoring..."
MONITOR_PID=""

monitor_spikes() {
    local metrics_file="$OUTPUT_DIR/metrics-timeseries.csv"
    echo "timestamp,heap_alloc_mb,heap_sys_mb,heap_inuse_mb,heap_objects,goroutines,total_alloc_mb,num_gc" > "$metrics_file"
    
    local start_time=$(date +%s)
    local peak_alloc=0
    
    while true; do
        local timestamp=$(date +%s)
        local elapsed=$((timestamp - start_time))
        
        # Fetch memory stats
        local memstats=$(curl -s "http://$PPROF_HOST/debug/pprof/heap?debug=1" 2>/dev/null | head -30)
        
        local heap_alloc=$(echo "$memstats" | grep "HeapAlloc = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
        local heap_sys=$(echo "$memstats" | grep "HeapSys = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
        local heap_inuse=$(echo "$memstats" | grep "HeapInuse = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
        local heap_objects=$(echo "$memstats" | grep "HeapObjects = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
        local total_alloc=$(echo "$memstats" | grep "TotalAlloc = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
        local num_gc=$(echo "$memstats" | grep "NumGC = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
        
        local goroutines=$(curl -s "http://$PPROF_HOST/debug/pprof/goroutine?debug=1" 2>/dev/null | grep "^goroutine profile:" | awk '{print $4}' || echo "0")
        
        # Ensure numeric values
        heap_alloc=${heap_alloc:-0}
        heap_sys=${heap_sys:-0}
        heap_inuse=${heap_inuse:-0}
        heap_objects=${heap_objects:-0}
        total_alloc=${total_alloc:-0}
        num_gc=${num_gc:-0}
        goroutines=${goroutines:-0}
        
        # Convert to MB
        local heap_alloc_mb=$(echo "scale=2; $heap_alloc / 1048576" | bc 2>/dev/null || echo "0")
        local heap_sys_mb=$(echo "scale=2; $heap_sys / 1048576" | bc 2>/dev/null || echo "0")
        local heap_inuse_mb=$(echo "scale=2; $heap_inuse / 1048576" | bc 2>/dev/null || echo "0")
        local total_alloc_mb=$(echo "scale=2; $total_alloc / 1048576" | bc 2>/dev/null || echo "0")
        
        # Log to CSV
        echo "${elapsed},${heap_alloc_mb},${heap_sys_mb},${heap_inuse_mb},${heap_objects},${goroutines},${total_alloc_mb},${num_gc}" >> "$metrics_file"
        
        # Detect spikes and capture profile
        local alloc_int=$(echo "$heap_alloc_mb" | cut -d. -f1)
        alloc_int=${alloc_int:-0}
        if [ "$alloc_int" -gt "$peak_alloc" ] 2>/dev/null; then
            peak_alloc=$alloc_int
            echo "  [Monitor] 🔥 Spike detected: ${heap_alloc_mb}MB at +${elapsed}s"
            curl -s "http://$PPROF_HOST/debug/pprof/allocs" > "$OUTPUT_DIR/allocs-spike-${elapsed}s.prof" 2>/dev/null
            curl -s "http://$PPROF_HOST/debug/pprof/heap" > "$OUTPUT_DIR/heap-spike-${elapsed}s.prof" 2>/dev/null
        fi
        
        sleep 5
    done
}

# Start monitoring in background
monitor_spikes &
MONITOR_PID=$!

# Cleanup function
cleanup() {
    if [ -n "$MONITOR_PID" ]; then
        kill $MONITOR_PID 2>/dev/null || true
    fi
}
trap cleanup EXIT INT TERM

# Capture baseline
echo "Capturing baseline..."
curl -s "http://$PPROF_HOST/debug/pprof/allocs" > "$OUTPUT_DIR/allocs-baseline.prof"
curl -s "http://$PPROF_HOST/debug/pprof/heap" > "$OUTPUT_DIR/heap-baseline.prof"
echo "✓ Baseline captured"
echo ""

# Run the test
echo "Running test..."
echo "---"
eval "$TEST_COMMAND"
TEST_EXIT_CODE=$?
echo "---"
echo "✓ Test completed (exit code: $TEST_EXIT_CODE)"
echo ""

# Give monitoring a moment to catch final state
sleep 3

# Capture final state
echo "Capturing final state..."
curl -s "http://$PPROF_HOST/debug/pprof/allocs" > "$OUTPUT_DIR/allocs-final.prof"
curl -s "http://$PPROF_HOST/debug/pprof/heap" > "$OUTPUT_DIR/heap-final.prof"
echo "✓ Final state captured"

# Stop monitoring
cleanup

# Generate analysis
echo ""
echo "=== Spike Analysis Report ==="
echo ""

# Find peak from CSV
METRICS_FILE="$OUTPUT_DIR/metrics-timeseries.csv"
if [ -f "$METRICS_FILE" ]; then
    echo "Peak Memory Usage:"
    awk -F',' 'NR>1 {if ($2 > max) {max=$2; time=$1}} END {printf "  %s MB at +%ss\n", max, time}' "$METRICS_FILE"
    
    echo ""
    echo "Peak Object Count:"
    awk -F',' 'NR>1 {if ($5 > max) {max=$5; time=$1}} END {printf "  %s objects at +%ss\n", max, time}' "$METRICS_FILE"
    
    echo ""
    echo "Peak Goroutines:"
    awk -F',' 'NR>1 {if ($6 > max) {max=$6; time=$1}} END {printf "  %s goroutines at +%ss\n", max, time}' "$METRICS_FILE"
fi

echo ""
echo "=== Top Allocation Sources ==="
echo ""
# Analyze the biggest spike profile if it exists
SPIKE_PROF=$(ls -t "$OUTPUT_DIR"/allocs-spike-*.prof 2>/dev/null | head -1)
if [ -n "$SPIKE_PROF" ]; then
    echo "From spike profile: $(basename $SPIKE_PROF)"
    go tool pprof -top -alloc_space "$SPIKE_PROF" 2>/dev/null | head -15
else
    echo "From final profile:"
    go tool pprof -top -alloc_space "$OUTPUT_DIR/allocs-final.prof" 2>/dev/null | head -15
fi

echo ""
echo "=== Files Generated ==="
ls -lh "$OUTPUT_DIR/" | grep -v "^total"

echo ""
echo "=== Next Steps ==="
echo ""
echo "# 1. Visualize memory spikes over time:"
echo "python3 scripts/plot-metrics.py $METRICS_FILE"
echo ""
echo "# 2. Find what's causing allocations (web UI):"
SPIKE_PROF=$(ls -t "$OUTPUT_DIR"/allocs-spike-*.prof 2>/dev/null | head -1)
if [ -n "$SPIKE_PROF" ]; then
    echo "go tool pprof -http=:8080 -alloc_space $SPIKE_PROF"
else
    echo "go tool pprof -http=:8080 -alloc_space $OUTPUT_DIR/allocs-final.prof"
fi
echo ""
echo "# 3. Compare baseline vs spike:"
if [ -n "$SPIKE_PROF" ]; then
    echo "go tool pprof -base $OUTPUT_DIR/allocs-baseline.prof $SPIKE_PROF"
else
    echo "go tool pprof -base $OUTPUT_DIR/allocs-baseline.prof $OUTPUT_DIR/allocs-final.prof"
fi
echo ""
echo "In pprof, use: 'top', 'list <function>', 'web' to investigate"
echo ""
