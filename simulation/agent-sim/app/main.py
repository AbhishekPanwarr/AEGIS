#!/usr/bin/env python3
"""AEGIS agent-sim Phase 0: sends a single hardcoded action to the gateway."""

import json
import os
import sys

import requests

GATEWAY_URL = os.getenv("GATEWAY_URL", "http://gateway:8080")


def main():
    payload = {
        "token": "dummy-phase0-token",
        "action_type": "SendPayment",
        "counterparty_id": "vendor_x",
        "amount_minor": 100,
        "idempotency_key": "phase0-abc-123",
        "budget_path": ["root"],
    }

    print(f"[agent-sim] Sending action to {GATEWAY_URL}/v1/act ...")
    print(f"[agent-sim] Payload: {json.dumps(payload, indent=2)}")

    try:
        resp = requests.post(
            f"{GATEWAY_URL}/v1/act",
            json=payload,
            timeout=15,
        )
    except requests.exceptions.RequestException as e:
        print(f"[agent-sim] ERROR: could not reach gateway: {e}")
        sys.exit(1)

    print(f"[agent-sim] HTTP Status: {resp.status_code}")

    try:
        body = resp.json()
        print(f"[agent-sim] Response: {json.dumps(body, indent=2)}")
    except Exception:
        print(f"[agent-sim] Response (raw): {resp.text}")

    if resp.status_code != 200:
        print("[agent-sim] FAILED: non-200 response")
        sys.exit(1)

    if "decision_id" not in resp.json():
        print("[agent-sim] FAILED: no decision_id in response")
        sys.exit(1)

    print("[agent-sim] Success.")


if __name__ == "__main__":
    main()
