#!/usr/bin/env python3
"""Measure revocation propagation latency: time from emergency-stop to first denial."""

import requests
import json
import subprocess
import time
import sys
import os

IDENTITY='http://localhost:8081'
GATEWAY='http://localhost:8080'
CONTAINMENT='http://localhost:8083'
BUDGET='http://localhost:8082'

def get_mandate():
    return subprocess.check_output(['docker','compose','exec','-T','postgres','psql','-U','aegis','-d','aegis','-t','-A','-c',
        "SELECT id FROM mandates WHERE status='live' LIMIT 1"]).decode().strip()

def get_budget():
    return requests.get(BUDGET+'/v1/budget-node-by-label/root').json()['id']

def mint(mandate_id):
    r = requests.post(IDENTITY+'/v1/tokens/mint', json={
        'agent_id': '00000000-0000-0000-0000-000000000010',
        'mandate_id': mandate_id, 'caveats': []
    })
    return json.dumps(r.json())

def main():
    print("[revocation-bench] Measuring click-to-first-deny latency...")
    
    mid = get_mandate()
    bid = get_budget()
    tok = mint(mid)
    
    # Verify the action works first
    r = requests.post(GATEWAY+'/v1/act', json={
        'token': tok, 'action_type': 'SendPayment',
        'counterparty_id': 'vendor_x', 'amount_minor': 100,
        'idempotency_key': 'rev-bench-warmup', 'budget_path': [bid]
    })
    if r.status_code != 200:
        print(f"[revocation-bench] Warmup failed: {r.status_code} {r.text}")
        sys.exit(1)
    print("[revocation-bench] Warmup action succeeded")
    
    # Mint a fresh token (the old one will be stale after epoch bump)
    tok2 = mint(mid)
    
    # Start a timer and fire emergency stop
    t0 = time.time()
    
    r = requests.post(CONTAINMENT+'/v1/emergency-stop', json={
        'scope_type': 'agent',
        'scope_id': '00000000-0000-0000-0000-000000000010',
        'reason': 'revocation_bench',
        'actor': 'bench'
    })
    
    if r.status_code != 200:
        print(f"[revocation-bench] Emergency stop failed: {r.status_code} {r.text}")
        sys.exit(1)
    
    stop_result = r.json()
    print(f"[revocation-bench] Emergency stop: epoch={stop_result.get('epoch_bumped_to')}, sagas_swept={stop_result.get('sagas_swept')}")
    
    # Now immediately try to send an action with the pre-stop token
    # The gateway should deny it with EPOCH_STALE or CONTAINMENT_HALT
    t1 = time.time()
    
    r = requests.post(GATEWAY+'/v1/act', json={
        'token': tok2, 'action_type': 'SendPayment',
        'counterparty_id': 'vendor_x', 'amount_minor': 100,
        'idempotency_key': 'rev-bench-test', 'budget_path': [bid]
    })
    t2 = time.time()
    
    latency_ms = (t2 - t0) * 1000
    deny_latency_ms = (t2 - t1) * 1000
    
    code = r.json().get('error', {}).get('code', r.json().get('status', ''))
    
    print(f"[revocation-bench] Response: {r.status_code} code={code}")
    print(f"[revocation-bench] Total latency (stop → deny): {latency_ms:.0f}ms")
    print(f"[revocation-bench] Deny latency (action → response): {deny_latency_ms:.0f}ms")
    
    if r.status_code == 403:
        if latency_ms < 1000:
            print(f"[revocation-bench] PASS: click-to-first-deny = {latency_ms:.0f}ms (< 1000ms)")
        else:
            print(f"[revocation-bench] FAIL: click-to-first-deny = {latency_ms:.0f}ms (>= 1000ms)")
    else:
        print(f"[revocation-bench] FAIL: expected denial, got {r.status_code}")
    
    # Resume the agent
    requests.post(CONTAINMENT+'/v1/resume', json={
        'scope_type': 'agent',
        'scope_id': '00000000-0000-0000-0000-000000000010',
        'actor': 'bench'
    })
    print("[revocation-bench] Agent resumed to THROTTLE")

if __name__ == "__main__":
    main()
