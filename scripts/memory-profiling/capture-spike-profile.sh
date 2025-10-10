#!/bin/bash

# Memory spike detection script - captures allocation patterns causing OOM
# For issues where allocations spike faster than GC can clean up
# Usage: ./scripts/capture-spike-profile.sh [output_dir]

set -e

OUTPUT_DIR="${1:-./spike-analysis-$(date +%Y%m%d-%H%M%S)}"
PPROF_HOST="localhost:6060"
SAMPLE_INTERVAL=5  # Fast sampling to catch spikes

mkdir -p "$OUTPUT_DIR"

echo "=== Memory Spike Detection ==="
echo "Output directory: $OUTPUT_DIR"
echo "Sample interval: ${SAMPLE_INTERVAL}s"
echo ""
echo "This captures:"
echo "  - Time-series metrics (CSV for graphing)"
echo "  - Allocation profiles (to find culprit)"
echo "  - Heap snapshots at key moments"
echo ""
echo "Press Ctrl+C to stop"
echo ""

# Create CSV header for metrics
METRICS_FILE="$OUTPUT_DIR/metrics-timeseries.csv"
echo "timestamp,heap_alloc_mb,heap_sys_mb,heap_inuse_mb,heap_objects,goroutines,total_alloc_mb,num_gc,pause_ns" > "$METRICS_FILE"

# Trap Ctrl+C
trap 'echo ""; echo "Stopping capture..."; kill $(jobs -p) 2>/dev/null; generate_report; exit 0' INT TERM

START_TIME=$(date +%s)

# Function to generate final report
generate_report() {
    echo ""
    echo "=== Analysis Report ==="
    echo ""
    echo "Files generated in: $OUTPUT_DIR"
    ls -lh "$OUTPUT_DIR/" | grep -v "^total"
    echo ""
    echo "=== Quick Analysis Commands ==="
    echo ""
    echo "# 1. View memory spike graph:"
    echo "python3 scripts/plot-metrics.py $METRICS_FILE"
    echo ""
    echo "# 2. Find allocation hotspots (what's allocating the most):"
    echo "go tool pprof -top -alloc_space $OUTPUT_DIR/allocs-*.prof | head -20"
    echo ""
    echo "# 3. Interactive web UI to explore allocations:"
    echo "go tool pprof -http=:8080 -alloc_space $OUTPUT_DIR/allocs-*.prof"
    echo ""
    echo "# 4. Compare allocations at different points:"
    echo "go tool pprof -base $OUTPUT_DIR/allocs-baseline.prof $OUTPUT_DIR/allocs-*.prof"
    echo ""
    echo "# Key metrics to look for in CSV:"
    echo "grep 'Peak' $OUTPUT_DIR/analysis-summary.txt"
    echo ""
}

# Capture baseline profiles
echo "[Baseline] Capturing initial profiles..."
curl -s "http://$PPROF_HOST/debug/pprof/allocs" > "$OUTPUT_DIR/allocs-baseline.prof"
curl -s "http://$PPROF_HOST/debug/pprof/heap" > "$OUTPUT_DIR/heap-baseline.prof"
echo "✓ Baseline captured"
echo ""

# Track peak values
PEAK_ALLOC=0
PEAK_OBJECTS=0
PEAK_GOROUTINES=0
SAMPLE_COUNT=0

# Main monitoring loop
while true; do
    TIMESTAMP=$(date +%s)
    ELAPSED=$((TIMESTAMP - START_TIME))
    SAMPLE_COUNT=$((SAMPLE_COUNT + 1))
    
    # Fetch memory stats
    MEMSTATS=$(curl -s "http://$PPROF_HOST/debug/pprof/heap?debug=1" 2>/dev/null | head -30)
    
    # Parse key metrics (format: "# HeapAlloc = 1234")
    HEAP_ALLOC=$(echo "$MEMSTATS" | grep "HeapAlloc = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
    HEAP_SYS=$(echo "$MEMSTATS" | grep "HeapSys = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
    HEAP_INUSE=$(echo "$MEMSTATS" | grep "HeapInuse = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
    HEAP_OBJECTS=$(echo "$MEMSTATS" | grep "HeapObjects = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
    TOTAL_ALLOC=$(echo "$MEMSTATS" | grep "TotalAlloc = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
    NUM_GC=$(echo "$MEMSTATS" | grep "NumGC = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
    PAUSE_NS=$(echo "$MEMSTATS" | grep "PauseNs = " | head -1 | sed 's/.*= //' | tr -d '\r' || echo "0")
    
    # Get goroutine count
    GOROUTINES=$(curl -s "http://$PPROF_HOST/debug/pprof/goroutine?debug=1" 2>/dev/null | grep "^goroutine profile:" | awk '{print $4}' || echo "0")
    
    # Ensure we have numeric values
    HEAP_ALLOC=${HEAP_ALLOC:-0}
    HEAP_SYS=${HEAP_SYS:-0}
    HEAP_INUSE=${HEAP_INUSE:-0}
    TOTAL_ALLOC=${TOTAL_ALLOC:-0}
    HEAP_OBJECTS=${HEAP_OBJECTS:-0}
    GOROUTINES=${GOROUTINES:-0}
    NUM_GC=${NUM_GC:-0}
    PAUSE_NS=${PAUSE_NS:-0}
    
    # Convert to MB
    HEAP_ALLOC_MB=$(echo "scale=2; $HEAP_ALLOC / 1048576" | bc 2>/dev/null || echo "0")
    HEAP_SYS_MB=$(echo "scale=2; $HEAP_SYS / 1048576" | bc 2>/dev/null || echo "0")
    HEAP_INUSE_MB=$(echo "scale=2; $HEAP_INUSE / 1048576" | bc 2>/dev/null || echo "0")
    TOTAL_ALLOC_MB=$(echo "scale=2; $TOTAL_ALLOC / 1048576" | bc 2>/dev/null || echo "0")
    
    # Write to CSV
    echo "${ELAPSED},${HEAP_ALLOC_MB},${HEAP_SYS_MB},${HEAP_INUSE_MB},${HEAP_OBJECTS},${GOROUTINES},${TOTAL_ALLOC_MB},${NUM_GC},${PAUSE_NS}" >> "$METRICS_FILE"
    
    # Check for spikes and capture profiles
    ALLOC_INT=$(echo "$HEAP_ALLOC_MB" | cut -d. -f1)
    ALLOC_INT=${ALLOC_INT:-0}
    if [ "$ALLOC_INT" -gt "$PEAK_ALLOC" ] 2>/dev/null; then
        PEAK_ALLOC=$ALLOC_INT
        echo "[+${ELAPSED}s] 🔥 NEW PEAK: ${HEAP_ALLOC_MB}MB (objects: ${HEAP_OBJECTS}, goroutines: ${GOROUTINES})"
        
        # Capture profile at peak
        curl -s "http://$PPROF_HOST/debug/pprof/allocs" > "$OUTPUT_DIR/allocs-peak-${ELAPSED}s.prof" 2>/dev/null
        curl -s "http://$PPROF_HOST/debug/pprof/heap" > "$OUTPUT_DIR/heap-peak-${ELAPSED}s.prof" 2>/dev/null
        echo "   📸 Captured spike profile"
    else
        echo "[+${ELAPSED}s] Alloc: ${HEAP_ALLOC_MB}MB, Objects: ${HEAP_OBJECTS}, Goroutines: ${GOROUTINES}, GCs: ${NUM_GC}"
    fi
    
    # Capture allocs profile every 30 seconds for comparison
    if [ $((SAMPLE_COUNT % 6)) -eq 0 ]; then
        curl -s "http://$PPROF_HOST/debug/pprof/allocs" > "$OUTPUT_DIR/allocs-${ELAPSED}s.prof" 2>/dev/null
    fi
    
    sleep $SAMPLE_INTERVAL
done
