import requests
import random
import time
import threading
import os
import base64
from concurrent.futures import ThreadPoolExecutor

# Configuration from environment variables
LB_HOST = os.getenv("LB_HOST", "localhost")
LB_PORT = os.getenv("LB_PORT", "8082")
DASHBOARD_PORT = os.getenv("DASHBOARD_PORT", "8081")

BASE_URL = f"http://{LB_HOST}:{LB_PORT}"
DASHBOARD_URL = f"http://{LB_HOST}:{DASHBOARD_PORT}"

# Backend URLs for chaos testing (internal network names from docker-compose)
BACKENDS = [
    "http://alpha:8001/toggle",
    "http://beta:8002/toggle",
    "http://gamma:8003/toggle",
    "http://delta:8004/toggle"
]

def log(msg):
    print(f"[{time.strftime('%H:%M:%S')}] {msg}")

def test_route(name, url, headers=None, auth=None, expected_status=None):
    try:
        res = requests.get(url, headers=headers, auth=auth, timeout=5)
        # log(f"{name}: {res.status_code}")
        if expected_status and res.status_code != expected_status:
            log(f"WARNING: {name} returned {res.status_code}, expected {expected_status}")
    except Exception as e:
        # log(f"ERROR: {name} failed: {e}")
        pass

# 1. Normal Traffic Flow
def normal_traffic():
    while True:
        test_route("Normal", f"{BASE_URL}/")
        time.sleep(random.uniform(0.1, 0.5))

# 2. Payment Flow (Success)
def payment_traffic():
    while True:
        test_route("Payment", f"{BASE_URL}/api/payment")
        test_route("Checkout", f"{BASE_URL}/api/checkout")
        time.sleep(random.uniform(0.5, 1.5))

# 3. Admin Flow (With correct headers)
def admin_traffic():
    headers = {"X-Admin-Key": "secret"}
    while True:
        test_route("Admin-Auth", f"{BASE_URL}/admin", headers=headers)
        time.sleep(random.uniform(1.0, 3.0))

# 4. Auth Failure (Missing header)
def admin_failure_traffic():
    while True:
        test_route("Admin-Fail", f"{BASE_URL}/admin", expected_status=404) # It should 404 if header doesn't match router rule
        time.sleep(random.uniform(5.0, 10.0))

# 5. Dashboard Auth
def dashboard_traffic():
    # Success
    auth = ("admin", "loadbalancer") # From config.json
    # Failure
    wrong_auth = ("admin", "wrongpassword")
    
    while True:
        test_route("Dash-Success", f"{DASHBOARD_URL}/", auth=auth)
        time.sleep(10)
        test_route("Dash-Fail", f"{DASHBOARD_URL}/api/metrics", auth=wrong_auth, expected_status=401)
        time.sleep(20)

# 6. Rate Limit Test (Bursts)
def burst_traffic():
    while True:
        time.sleep(random.uniform(15, 30))
        log("Triggering Rate Limit Burst...")
        with ThreadPoolExecutor(max_workers=50) as executor:
            for _ in range(120): # Limit is 100/sec, burst 200
                executor.submit(test_route, "Burst", f"{BASE_URL}/", expected_status=None)

# 7. Chaos Monkey (Backend Toggling)
def chaos_monkey():
    while True:
        time.sleep(random.uniform(20, 45))
        target = random.choice(BACKENDS)
        log(f"Chaos Monkey toggling backend: {target}")
        try:
            requests.get(target, timeout=2)
        except:
            pass

# 8. Edge Case: Non-existent paths
def not_found_traffic():
    while True:
        test_route("404-Test", f"{BASE_URL}/unknown/path/here", expected_status=404)
        time.sleep(random.uniform(10, 20))

if __name__ == "__main__":
    log(f"Starting Comprehensive Traffic Generator targetting {BASE_URL}")
    
    threads = [
        threading.Thread(target=normal_traffic, daemon=True),
        threading.Thread(target=payment_traffic, daemon=True),
        threading.Thread(target=admin_traffic, daemon=True),
        threading.Thread(target=admin_failure_traffic, daemon=True),
        threading.Thread(target=dashboard_traffic, daemon=True),
        threading.Thread(target=burst_traffic, daemon=True),
        threading.Thread(target=chaos_monkey, daemon=True),
        threading.Thread(target=not_found_traffic, daemon=True)
    ]
    
    for t in threads:
        t.start()
        
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        log("Stopping...")
