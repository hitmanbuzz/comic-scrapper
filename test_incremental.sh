#!/bin/bash

echo "=== Testing Scraping Modes ==="
echo

# Test 1: Show help with new mode flag
echo "1. Testing help output with new -mode flag:"
./comicrawl -help | grep -E "(mode|full|incremental|single)"
echo

# Test 2: Test full mode (default behavior)
echo "2. Testing full mode (dry-run):"
timeout 10s ./comicrawl -config configs/config.yaml -mode full -dry-run -sources asurascans -limit-series 1 2>&1 | grep -E "(mode=full|total_chapters|metadata_updates)"
echo

# Test 3: Test incremental mode (should skip new series)
echo "3. Testing incremental mode (dry-run):"
timeout 10s ./comicrawl -config configs/config.yaml -mode incremental -dry-run -sources asurascans -limit-series 1 2>&1 | grep -E "(mode=incremental|total_chapters|metadata_updates|skipping)"
echo

# Test 4: Test single mode (should only process specified series)
echo "4. Testing single mode with specific series (dry-run):"
timeout 10s ./comicrawl -config configs/config.yaml -mode single -include-series lookism -dry-run -sources asurascans 2>&1 | grep -E "(mode=single|include_series|total_chapters|skipping)"
echo

# Test 5: Test invalid mode
echo "5. Testing invalid mode:"
./comicrawl -config configs/config.yaml -mode invalid 2>&1 | grep "Invalid mode" || echo "No error message for invalid mode"
echo

# Test 6: Test incremental mode with no local data (should skip entirely)
echo "6. Testing incremental mode with no local data:"
timeout 15s ./comicrawl -config configs/config.yaml -mode incremental -dry-run -sources asurascans 2>&1 | grep -E "(skipping new series in incremental mode|no new chapters to process)"
echo

echo "=== Test Complete ==="