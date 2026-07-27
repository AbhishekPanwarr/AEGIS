.PHONY: up down seed test logs race-bench revocation-bench demo chaos reset archive

up:
	docker compose up -d --build

down:
	docker compose down

seed:
	python3 scripts/seed_demo_data.py

test:
	go test ./pkg/...
	go test ./services/identity-service/...
	go test ./services/budget-ledger/...
	go test ./services/gateway/...
	go test ./services/audit-chain/...
	go test ./services/mock-rails/...
	go test ./services/containment-controller/...

race-bench:
	go test ./tests/race/... -v -run TestRaceBenchmark -timeout 600s

revocation-bench:
	python3 scripts/measure_revocation_latency.py

logs:
	docker compose logs -f

demo:
	bash scripts/demo.sh

chaos:
	bash scripts/chaos.sh

reset:
	docker compose down -v
	docker compose rm -f

archive:
	make reset
	zip -r aegis-submission.zip . -x "*.git*" "*node_modules*" "*__pycache__*" "*.env" "*.zip"
