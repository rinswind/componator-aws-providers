#!/usr/bin/env python3
"""
Plot memory and goroutine metrics from time-series CSV data.
Usage: python3 scripts/plot-metrics.py profile-metrics.csv
"""

import sys
import pandas as pd
import matplotlib.pyplot as plt
from pathlib import Path

def plot_metrics(csv_file):
    """Generate plots from metrics CSV file."""
    # Read CSV
    df = pd.read_csv(csv_file)
    
    # Convert timestamp to minutes for readability
    df['time_minutes'] = df['timestamp'] / 60
    
    # Create figure with subplots
    fig, axes = plt.subplots(2, 2, figsize=(15, 10))
    fig.suptitle('Memory and Goroutine Metrics Over Time', fontsize=16)
    
    # Plot 1: Heap Memory
    ax1 = axes[0, 0]
    ax1.plot(df['time_minutes'], df['heap_alloc_mb'], label='HeapAlloc', marker='o')
    ax1.plot(df['time_minutes'], df['heap_inuse_mb'], label='HeapInuse', marker='s')
    ax1.plot(df['time_minutes'], df['heap_sys_mb'], label='HeapSys', marker='^')
    ax1.set_xlabel('Time (minutes)')
    ax1.set_ylabel('Memory (MB)')
    ax1.set_title('Heap Memory Usage')
    ax1.legend()
    ax1.grid(True, alpha=0.3)
    
    # Plot 2: Heap Objects
    ax2 = axes[0, 1]
    ax2.plot(df['time_minutes'], df['heap_objects'], color='green', marker='o')
    ax2.set_xlabel('Time (minutes)')
    ax2.set_ylabel('Object Count')
    ax2.set_title('Heap Objects')
    ax2.grid(True, alpha=0.3)
    
    # Plot 3: Goroutines
    ax3 = axes[1, 0]
    ax3.plot(df['time_minutes'], df['goroutines'], color='red', marker='o')
    ax3.set_xlabel('Time (minutes)')
    ax3.set_ylabel('Goroutine Count')
    ax3.set_title('Active Goroutines')
    ax3.grid(True, alpha=0.3)
    
    # Plot 4: Total Allocations and GC Activity
    ax4 = axes[1, 1]
    ax4.plot(df['time_minutes'], df['total_alloc_mb'], label='Total Alloc MB', marker='o', color='purple')
    ax4_twin = ax4.twinx()
    ax4_twin.plot(df['time_minutes'], df['num_gc'], label='GC Count', marker='s', color='orange')
    ax4.set_xlabel('Time (minutes)')
    ax4.set_ylabel('Total Allocated Memory (MB)', color='purple')
    ax4_twin.set_ylabel('GC Count', color='orange')
    ax4.set_title('Cumulative Allocations & GC Activity')
    ax4.grid(True, alpha=0.3)
    ax4.legend(loc='upper left')
    ax4_twin.legend(loc='upper right')
    
    plt.tight_layout()
    
    # Save plot
    output_file = Path(csv_file).stem + '.png'
    plt.savefig(output_file, dpi=300, bbox_inches='tight')
    print(f"Plot saved to: {output_file}")
    
    # Show plot
    plt.show()

if __name__ == '__main__':
    if len(sys.argv) < 2:
        print("Usage: python3 scripts/plot-metrics.py <csv_file>")
        sys.exit(1)
    
    csv_file = sys.argv[1]
    if not Path(csv_file).exists():
        print(f"Error: File '{csv_file}' not found")
        sys.exit(1)
    
    plot_metrics(csv_file)
