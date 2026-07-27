import os
import asyncio
from fastapi import FastAPI
from prometheus_client import make_asgi_app
import app.consumer as consumer
import app.correlation as correlation

app = FastAPI()
metrics_app = make_asgi_app()
app.mount("/metrics", metrics_app)

from prometheus_client import Gauge
aegis_fleet_correlation_index = Gauge('aegis_fleet_correlation_index', 'Fleet correlation index')
aegis_drift_score = Gauge('aegis_drift_score', 'Agent drift score', ['agent_id', 'feature'])

@app.on_event("startup")
async def startup_event():
    asyncio.create_task(consumer.start_consuming())
    asyncio.create_task(correlation.start_correlation_loop())

@app.get("/healthz")
def healthz():
    return {"status": "ok"}
