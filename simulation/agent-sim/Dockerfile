FROM python:3.12-slim

WORKDIR /app

COPY simulation/agent-sim/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY simulation/agent-sim/app/ ./app/

CMD ["python", "app/main.py"]
