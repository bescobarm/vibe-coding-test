DATABASE_URL ?= postgres://auth:auth@localhost:55432/auth?sslmode=disable
COVER_DIR    ?= $(or $(TMPDIR),/tmp)/card-auth-engine-cover
export DATABASE_URL

.PHONY: up down reset run test cover vet lint simulate load

up: ## Start Postgres and Redis and wait until they are healthy
	docker compose up -d --wait

down: ## Stop the containers (data is kept)
	docker compose down

reset: ## Stop the containers and delete all data
	docker compose down -v

run: up ## Apply migrations, seed demo data and serve on :8080
	go run ./cmd/engine

test: up ## Unit + integration tests (integration tests use the compose Postgres)
	go test -race -count=1 ./...

cover: up ## Coverage per function
	mkdir -p $(COVER_DIR)
	go test -count=1 -coverprofile=$(COVER_DIR)/cover.out ./...
	go tool cover -func=$(COVER_DIR)/cover.out

vet:
	go vet ./...

INIT_IN_IF := ^[[:space:]]*(if|switch) [^{]*:= [^{]*;

lint: ## golangci-lint (.golangci.yml) plus a check that no if/switch has an init statement
	golangci-lint run ./...
	@! grep -rnE '$(INIT_IN_IF)' --include='*.go' . || { echo "assign first, then check: no init statement in if/switch (D-043)"; false; }

simulate:
	@echo "Not implemented yet: arrives with sub-project 06 (briefs/06-simulator-and-report-brief.md)"

load:
	@echo "Not implemented yet: arrives with sub-project 06 (briefs/06-simulator-and-report-brief.md)"
