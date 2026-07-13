.PHONY: help build run test

help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_%.-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

switch.%: ## switch to a specific environment (e.g., make switch.local)
	cp .env.$* .env

# Load DATABASE_URL from .env.<env>, falling back to the local docker-compose DSN.
# $(or ...) handles the case where grep finds nothing: cut exits 0 on empty input
# so shell || never fires — $(or) catches the empty string instead.
_db_url = $(or $(shell grep -m1 '^DATABASE_URL=' .env.$(1) 2>/dev/null | cut -d= -f2-),postgres://chamlai:chamlai@localhost:5432/chamlai?sslmode=disable)

migrate.%: ## Apply all pending migrations for env (e.g., make migrate.local)
	DATABASE_URL="$(call _db_url,$*)" go run ./cmd/migration up

migrate.%.down: ## Roll back the latest migration for env (e.g., make migrate.local.down)
	DATABASE_URL="$(call _db_url,$*)" go run ./cmd/migration down

migrate.%.status: ## Show migration status for env (e.g., make migrate.local.status)
	DATABASE_URL="$(call _db_url,$*)" go run ./cmd/migration status

swagger: ## Regenerate the OpenAPI spec + swagger package from handler annotations
	go tool swag init -g cmd/api/main.go -o internal/api/swagger
