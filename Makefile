APP_NAME=shorty
DB_URL?=postgres://shorty:shorty@localhost:5432/shorty?sslmode=disable

# Postgres (Docker) Settings
PG_IMAGE?=postgres:16
PG_CONTAINER?=shorty-pg
PG_PORT?=5432
PG_USER?=shorty
PG_PASS?=shorty
PG_DB?=shorty

.PHONY: run build fmt migrate-up migrate-down create-migration \
        db-up db-down db-stop db-logs db-psql db-wait goose-install \
        fe-install fe-dev fe-build

run:
	BASE_URL=http://localhost:8080 DATABASE_URL="$(DB_URL)" go run ./cmd/server

# Format code before building
build: fmt
	CGO_ENABLED=0 go build -o bin/$(APP_NAME) ./cmd/server

# go fmt across the whole module
fmt:
	go fmt ./...

migrate-up:
	goose -dir ./migrations postgres "$(DB_URL)" up

migrate-down:
	goose -dir ./migrations postgres "$(DB_URL)" down

create-migration:
	@if [ -z "$$name" ]; then echo "Usage: make create-migration name=add_whatever"; exit 1; fi
	goose -dir ./migrations create $$name sql

# --- Postgres via Docker ---

# Start Postgres (create if missing), then wait until ready
db-up:
	@if docker ps -a --format '{{.Names}}' | grep -wq '$(PG_CONTAINER)'; then \
		echo "Starting existing container: $(PG_CONTAINER)"; \
		docker start $(PG_CONTAINER) >/dev/null; \
	else \
		echo "Creating and starting container: $(PG_CONTAINER) ($(PG_IMAGE))"; \
		docker run -d --name $(PG_CONTAINER) \
			-e POSTGRES_USER=$(PG_USER) \
			-e POSTGRES_PASSWORD=$(PG_PASS) \
			-e POSTGRES_DB=$(PG_DB) \
			-p $(PG_PORT):5432 $(PG_IMAGE) >/dev/null; \
	fi
	$(MAKE) db-wait

# Wait until Postgres responds
db-wait:
	@echo "Waiting for Postgres to become ready..."
	@until docker exec $(PG_CONTAINER) pg_isready -U $(PG_USER) >/dev/null 2>&1; do sleep 0.5; done
	@echo "Postgres is ready on localhost:$(PG_PORT) → db=$(PG_DB)"

# Stop & remove container
db-down:
	-@docker rm -f $(PG_CONTAINER) >/dev/null 2>&1 || true
	@echo "Removed container $(PG_CONTAINER)"

# Stop container (without removing)
db-stop:
	-@docker stop $(PG_CONTAINER) >/dev/null 2>&1 || true
	@echo "Stopped container $(PG_CONTAINER)"

# Tail logs
db-logs:
	docker logs -f $(PG_CONTAINER)

# Interactive psql
db-psql:
	docker exec -it $(PG_CONTAINER) psql -U $(PG_USER) -d $(PG_DB)

# Install goose
goose-install:
	go install github.com/pressly/goose/v3/cmd/goose@latest

fe-install:
	cd web/frontend && npm install

fe-dev:
	cd web/frontend && npm run dev

fe-build:
	cd web/frontend && npm run build
