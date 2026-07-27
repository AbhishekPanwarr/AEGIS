import os
import json
import redis
import time
import psycopg2
import asyncio
from app.ewma import on_new_action

REDIS_ADDR = os.getenv("REDIS_ADDR", "redis:6379")
POSTGRES_DSN = os.getenv("POSTGRES_DSN", "postgresql://aegis:aegis@postgres:5432/aegis")

async def start_consuming():
    r = redis.Redis(host=REDIS_ADDR.split(':')[0], port=int(REDIS_ADDR.split(':')[1]), decode_responses=True)
    stream_name = "stream:decisions"
    group_name = "behavior-analytics-group"

    try:
        r.xgroup_create(stream_name, group_name, id="0", mkstream=True)
    except redis.exceptions.ResponseError as e:
        if "BUSYGROUP" not in str(e):
            print(f"Error creating group: {e}")

    last_id = ">"
    conn = psycopg2.connect(POSTGRES_DSN)

    while True:
        try:
            messages = r.xreadgroup(group_name, "consumer-1", {stream_name: last_id}, count=100, block=1000)
            if not messages:
                await asyncio.sleep(0.1)
                continue

            for _, msg_list in messages:
                for msg_id, msg_data in msg_list:
                    try:
                        if 'data' in msg_data:
                            event = json.loads(msg_data['data'])
                            on_new_action(event, r, conn)
                    except Exception as e:
                        print(f"Error processing message {msg_id}: {e}")
                    
                    r.xack(stream_name, group_name, msg_id)
        except Exception as e:
            print(f"Consumer loop error: {e}")
            await asyncio.sleep(2)
