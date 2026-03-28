#!/bin/bash
# traffic_generator.sh
# This script sends varied traffic to the Load Balancer to test dashboard rendering, metrics, rate limits, and circuit breakers.

echo "=================================================="
echo "🚀 Starting Intelligent Load Balancer Traffic Generator"
echo "=================================================="

# Function to clean up background loops on exit
cleanup() {
  echo ""
  echo "🛑 Stopping Traffic Generator..."
  kill $(jobs -p) 2>/dev/null || true
  
  # Ensure port 8003 (Gamma) is restarted if it was killed during exit
  PIDS=$(lsof -t -i:8003)
  if [ -z "$PIDS" ]; then
    echo "♻️ Restoring Gamma-All backend before exit..."
    nohup go run backend/server.go 8003 100 Gamma-All > /dev/null 2>&1 &
  fi
  exit 0
}
trap cleanup SIGINT SIGTERM EXIT

# 1. Steady Baseline Traffic (Payment Router)
echo "➡️ Starting Payment traffic (10 RPS)..."
(
  while true; do
    curl -s http://localhost:8080/api/payment > /dev/null
    sleep 0.1
  done
) &

# 2. Steady Baseline Traffic (Default Router)
echo "➡️ Starting Default routing traffic (5 RPS)..."
(
  while true; do
    curl -s http://localhost:8080/ > /dev/null
    sleep 0.2
  done
) &

# 3. Valid Admin Traffic
echo "➡️ Starting Admin traffic (2 RPS)..."
(
  while true; do
    curl -s -H "X-Admin-Key: secret" http://localhost:8080/admin > /dev/null
    sleep 0.5
  done
) &

# 4. Injected Spikes for Rate Limit Testing
(
  while true; do
    sleep 10
    echo "[Event] 🌊 Traffic Spike! Sending burst to trigger Rate Limiting (429s)..."
    for i in {1..80}; do
      curl -s http://localhost:8080/api/payment > /dev/null &
    done
    wait
  done
) &

# 5. Circuit Breaker Testing (Process killing)
(
  while true; do
    sleep 20
    echo "[Event] ⚠️ Simulating backend crash! Killing Gamma-All (Port 8003)..."
    PIDS=$(lsof -t -i:8003)
    if [ ! -z "$PIDS" ]; then
      kill -9 $PIDS || true
    fi
    
    echo "        ...waiting 10s. Default Router should show Errors, Circuit should OPEN."
    sleep 10
    
    echo "[Event] 🔌 Restoring Gamma-All (Port 8003). Circuit should enter HALF_OPEN, then CLOSED."
    nohup go run backend/server.go 8003 100 Gamma-All > /dev/null 2>&1 &
    
    # Let it run stable for another 20s
    sleep 20
  done
) &

echo ""
echo "✅ Traffic Generator is running in the background."
echo "👀 Check the Dashboard at http://localhost:8081 to see real-time updates!"
echo "Press [CTRL+C] to stop."
wait
