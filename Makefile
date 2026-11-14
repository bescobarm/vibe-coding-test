DATABASE_URL ?= postgres://auth:auth@localhost:55432/auth?sslmode=disable
COVER_DIR    ?= $(or $(TMPDIR),/tmp)/card-auth-engine-cover
export DATABASE_URL

.PHONY: up down reset run test cover vet lint simulate load dashboard

up: ## Start Postgres and Redis and wait until they are healthy
	docker compose up -d --wait

down: ## Stop the containers (data is kept)
	docker compose down

reset: ## Stop the containers and delete all data
	docker compose down -v

run: up ## Apply migrations, seed demo data and serve on :8080
	go run ./cmd/engine

test: up ## Unit + integration tests; a missing Postgres or Redis fails instead of skipping (I-028)
	TEST_REQUIRE_SERVICES=true go test -race -count=1 ./...

cover: up ## Coverage per function
	mkdir -p $(COVER_DIR)
	TEST_REQUIRE_SERVICES=true go test -count=1 -coverprofile=$(COVER_DIR)/cover.out ./...
	go tool cover -func=$(COVER_DIR)/cover.out

vet:
	go vet ./...

INIT_IN_IF := ^[[:space:]]*(if|switch) [^{]*:= [^{]*;

lint: ## golangci-lint (.golangci.yml), the style checker, and no init statement in if/switch
	golangci-lint run ./...
	go run ./tools/stylecheck .
	@! grep -rnE '$(INIT_IN_IF)' --include='*.go' . || { echo "assign first, then check: no init statement in if/switch (D-043)"; false; }

simulate: ## Play the labeled scenarios against the running engine and print the report (needs `make run`)
	go run ./cmd/simulator -mode report

load: ## Load test the running engine at a fixed rate; fails if p99 >= 100 ms (needs `make run`)
	go run ./cmd/simulator -mode load

dashboard: ## Web dashboard on http://localhost:8090: send transactions, watch metrics live (needs `make run`)
	go run ./cmd/dashboard

