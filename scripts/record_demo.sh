#!/bin/bash
set -e

OUTFILE="demo_recording_$(date +%s).cast"

if command -v asciinema &> /dev/null; then
    echo "Starting asciinema recording..."
    asciinema rec "$OUTFILE" -c "bash scripts/demo.sh"
    echo "Recording saved to $OUTFILE"
else
    echo "asciinema not found, falling back to 'script'..."
    OUTFILE="demo_recording_$(date +%s).log"
    script -q -c "bash scripts/demo.sh" "$OUTFILE"
    echo "Recording saved to $OUTFILE"
fi
