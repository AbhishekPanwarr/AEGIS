#!/usr/bin/env python3
"""AEGIS seed script: populates agents, mandates, budget nodes, Cedar policies."""

import json
import os
import subprocess
import sys
import time
import hashlib
import requests

IDENTITY_URL = os.getenv("IDENTITY_URL", "http://localhost:8081")
BUDGET_URL = os.getenv("BUDGET_URL", "http://localhost:8082")
CEDAR_URL = os.getenv("CEDAR_URL", "http://localhost:8180")
POSTGRES_CONTAINER = os.getenv("POSTGRES_CONTAINER", "aegis-postgres-1")

GROUP_ID = "00000000-0000-0000-0000-000000000001"

AGENTS = [
    {"id": "00000000-0000-0000-0000-000000000010", "persona": "procurement"},
    {"id": "00000000-0000-0000-0000-000000000020", "persona": "treasury"},
    {"id": "00000000-0000-0000-0000-000000000030", "persona": "refunds"},
    {"id": "00000000-0000-0000-0000-000000000040", "persona": "collections"},
]

COUNTERPARTIES = ["vendor_x", "vendor_y", "vendor_z", "acct_internal", "cust_001", "cust_002"]

def wait_for(url, name, timeout=60):
    start = time.time()
    while time.time() - start < timeout:
        try:
            # cedar-agent uses /health, others use /healthz
            health_url = url.rstrip("/") + "/health"
            r = requests.get(health_url, timeout=3)
            if r.status_code == 200:
                print(f"[seed] {name} is healthy")
                return True
            # Try /healthz as fallback
            r = requests.get(url.rstrip("/") + "/healthz", timeout=3)
            if r.status_code == 200:
                print(f"[seed] {name} is healthy")
                return True
        except:
            pass
        time.sleep(2)
    print(f"[seed] ERROR: {name} not healthy after {timeout}s")
    return False

def seed_cedar():
    print("[seed] Seeding Cedar schema...")
    with open("policies/cedar/schema.cedarschema.json") as f:
        schema = json.load(f)
    r = requests.put(f"{CEDAR_URL}/v1/schema", json=schema, timeout=10)
    if r.status_code != 200:
        print(f"[seed] ERROR: schema PUT failed: {r.status_code} {r.text}")
        sys.exit(1)
    print("[seed] Cedar schema seeded")

    print("[seed] Seeding Cedar policies...")
    with open("policies/cedar/policies/personas.cedar.json") as f:
        policies = json.load(f)
    r = requests.put(f"{CEDAR_URL}/v1/policies", json=policies, timeout=10)
    if r.status_code != 200:
        print(f"[seed] ERROR: policies PUT failed: {r.status_code} {r.text}")
        sys.exit(1)
    print("[seed] Cedar policies seeded")

    print("[seed] Seeding Cedar entity data...")
    entities = []
    for agent in AGENTS:
        entities.append({
            "attrs": {"persona": agent["persona"], "mandate_id": ""},
            "parents": [],
            "uid": {"id": agent["id"], "type": "Agent"}
        })
    for cp in COUNTERPARTIES:
        entities.append({
            "attrs": {},
            "parents": [],
            "uid": {"id": cp, "type": "Counterparty"}
        })
    entities.append({"attrs": {}, "parents": [], "uid": {"id": "SendPayment", "type": "Action"}})
    entities.append({"attrs": {}, "parents": [], "uid": {"id": "ReadInvoice", "type": "Action"}})

    r = requests.put(f"{CEDAR_URL}/v1/data", json=entities, timeout=10)
    if r.status_code != 200:
        print(f"[seed] ERROR: data PUT failed: {r.status_code} {r.text}")
        sys.exit(1)
    print("[seed] Cedar entity data seeded")

def get_pubkey():
    r = requests.get(f"{IDENTITY_URL}/v1/pubkey", timeout=5)
    data = r.json()
    return data["pubkey"]

def seed_agents(pubkey_b64):
    print("[seed] Seeding agents into Postgres...")
    for agent in AGENTS:
        sql = f"""
        INSERT INTO agents (id, group_id, persona, pubkey, status, created_by)
        VALUES ('{agent["id"]}', '{GROUP_ID}', '{agent["persona"]}', decode('{pubkey_b64}', 'base64'), 'active', 'seed')
        ON CONFLICT (id) DO NOTHING;
        """
        result = subprocess.run(
            ["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "aegis", "-d", "aegis", "-c", sql],
            capture_output=True, text=True, timeout=10
        )
        if result.returncode != 0:
            print(f"[seed] WARNING: agent insert for {agent['persona']}: {result.stderr.strip()}")
    print(f"[seed] {len(AGENTS)} agents seeded")

def seed_mandates():
    print("[seed] Creating mandates via identity-service...")
    mandates = [
        {
            "agent_id": AGENTS[0]["id"],
            "issuer": "seed",
            "purpose_code": "VENDOR_PAYMENT",
            "counterparty_scope": {"type": "allowlist", "values": ["vendor_x", "vendor_y"]},
            "ceiling_minor": 2500000,
            "valid_from": "2026-01-01T00:00:00.000Z",
            "valid_to": "2027-01-01T00:00:00.000Z",
            "max_depth": 2,
        },
        {
            "agent_id": AGENTS[1]["id"],
            "issuer": "seed",
            "purpose_code": "TREASURY_REBALANCE",
            "counterparty_scope": {"type": "allowlist", "values": ["acct_internal"]},
            "ceiling_minor": 10000000,
            "valid_from": "2026-01-01T00:00:00.000Z",
            "valid_to": "2027-01-01T00:00:00.000Z",
            "max_depth": 2,
        },
        {
            "agent_id": AGENTS[2]["id"],
            "issuer": "seed",
            "purpose_code": "CUSTOMER_REFUND",
            "counterparty_scope": {"type": "allowlist", "values": ["cust_001", "cust_002"]},
            "ceiling_minor": 500000,
            "valid_from": "2026-01-01T00:00:00.000Z",
            "valid_to": "2027-01-01T00:00:00.000Z",
            "max_depth": 2,
        },
        {
            "agent_id": AGENTS[3]["id"],
            "issuer": "seed",
            "purpose_code": "COLLECTION_NOTICE",
            "counterparty_scope": {"type": "allowlist", "values": ["cust_001", "cust_002"]},
            "ceiling_minor": 100000,
            "valid_from": "2026-01-01T00:00:00.000Z",
            "valid_to": "2027-01-01T00:00:00.000Z",
            "max_depth": 2,
        },
    ]

    mandate_ids = []
    for m in mandates:
        r = requests.post(f"{IDENTITY_URL}/v1/mandates", json=m, timeout=10)
        if r.status_code != 201:
            print(f"[seed] WARNING: mandate create failed: {r.status_code} {r.text}")
            continue
        data = r.json()
        mandate_ids.append(data["id"])
        print(f"[seed] Mandate created: {data['id']} for {m['purpose_code']}")

    return mandate_ids

def seed_budget():
    print("[seed] Creating budget nodes via budget-ledger...")
    r = requests.post(f"{BUDGET_URL}/v1/budget-nodes", json={
        "label": "root",
        "cap_minor": 10000000,
        "currency": "USD",
    }, timeout=10)
    if r.status_code != 201:
        print(f"[seed] WARNING: root budget node create failed: {r.status_code} {r.text}")
        return None
    root = r.json()
    print(f"[seed] Root budget node: {root['id']}")

    r = requests.post(f"{BUDGET_URL}/v1/budget-nodes", json={
        "parent_id": root["id"],
        "label": "procurement",
        "cap_minor": 5000000,
        "currency": "USD",
    }, timeout=10)
    if r.status_code != 201:
        print(f"[seed] WARNING: child budget node create failed: {r.status_code} {r.text}")
    else:
        child = r.json()
        print(f"[seed] Child budget node: {child['id']}")

    return root["id"]

def seed_policy_versions():
    print("[seed] Inserting policy_versions row...")
    with open("policies/cedar/policies/personas.cedar.json") as f:
        policy_src = f.read()
    policy_hash = hashlib.sha256(policy_src.encode()).hexdigest()

    sql = f"""
    INSERT INTO policy_versions (version_hash, cedar_source, state, author)
    VALUES ('{policy_hash}', '{policy_src.replace("'", "''")}', 'ACTIVE', 'seed')
    ON CONFLICT (version_hash) DO UPDATE SET state = 'ACTIVE';
    """
    result = subprocess.run(
        ["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "aegis", "-d", "aegis", "-c", sql],
        capture_output=True, text=True, timeout=10
    )
    if result.returncode != 0:
        print(f"[seed] WARNING: policy_versions insert: {result.stderr.strip()}")
    else:
        print(f"[seed] policy_versions seeded with hash {policy_hash[:16]}...")

CONTAINMENT_URL = os.getenv("CONTAINMENT_URL", "http://localhost:8083")

def seed_containment():
    print("[seed] Seeding initial containment states (all OBSERVE)...")
    # Set fleet to OBSERVE
    try:
        r = requests.put(f"{CONTAINMENT_URL}/v1/containment/fleet/ALL",
            json={"level": "OBSERVE", "reason": "seed", "actor": "seed"}, timeout=10)
        if r.status_code == 200:
            print("[seed] Fleet: OBSERVE")
    except Exception as e:
        print(f"[seed] WARNING: containment fleet: {e}")

    # Set group to OBSERVE
    try:
        r = requests.put(f"{CONTAINMENT_URL}/v1/containment/group/{GROUP_ID}",
            json={"level": "OBSERVE", "reason": "seed", "actor": "seed"}, timeout=10)
        if r.status_code == 200:
            print(f"[seed] Group {GROUP_ID[:8]}...: OBSERVE")
    except Exception as e:
        print(f"[seed] WARNING: containment group: {e}")

    # Set each agent to OBSERVE
    for agent in AGENTS:
        try:
            r = requests.put(f"{CONTAINMENT_URL}/v1/containment/agent/{agent['id']}",
                json={"level": "OBSERVE", "reason": "seed", "actor": "seed"}, timeout=10)
            if r.status_code == 200:
                print(f"[seed] Agent {agent['persona']}: OBSERVE")
        except Exception as e:
            print(f"[seed] WARNING: containment agent {agent['persona']}: {e}")

    # Initialize epoch keys in Redis
    subprocess.run(["docker", "compose", "exec", "-T", "redis", "redis-cli", "SETNX", "epoch:fleet:ALL", "0"],
        capture_output=True, timeout=5)

def main():
    print("=" * 60)
    print("[seed] AEGIS Demo Data Seeding")
    print("=" * 60)

    for url, name in [(IDENTITY_URL, "identity-service"), (BUDGET_URL, "budget-ledger"), (CEDAR_URL, "cedar-agent"), (CONTAINMENT_URL, "containment-controller")]:
        if not wait_for(url, name):
            sys.exit(1)

    seed_cedar()

    pubkey = get_pubkey()
    print(f"[seed] Identity pubkey: {pubkey[:32]}...")

    seed_agents(pubkey)
    mandate_ids = seed_mandates()
    root_budget_id = seed_budget()
    seed_policy_versions()
    seed_containment()

    print()
    print("=" * 60)
    print("[seed] Seed complete. Summary:")
    print(f"  Agents: {len(AGENTS)}")
    print(f"  Mandates: {len(mandate_ids)}")
    print(f"  Budget root node: {root_budget_id}")
    print(f"  Cedar: schema + 4 policies + {len(AGENTS) + len(COUNTERPARTIES) + 2} entities")
    print()
    print("  Mandate IDs (for token minting):")
    for mid in mandate_ids:
        print(f"    {mid}")
    print("=" * 60)

if __name__ == "__main__":
    main()
