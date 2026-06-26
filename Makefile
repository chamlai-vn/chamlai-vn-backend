.PHONY: help build run test

# Accept a seeds_YYYYMMDD.txt argument as a pseudo-target (e.g. make crawl.local seeds_20260626.txt).
# Filter it from MAKECMDGOALS, register a no-op rule so Make doesn't error, then expose as SEEDS.
_seeds_arg   := $(filter seeds_%.txt, $(MAKECMDGOALS))
$(if $(_seeds_arg), $(eval $(_seeds_arg):;@:))
SEEDS        ?= $(or $(_seeds_arg), seeds_$(shell date +%Y%m%d).txt)

help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_%.-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

switch.%: ## switch to a specific environment (e.g., make switch.local)
	cp .env.$* .env

_voyage_key = $(or $(shell grep -m1 '^VOYAGE_API_KEY=' .env.$(1) 2>/dev/null | cut -d= -f2-),$(error VOYAGE_API_KEY not found in .env.$(1)))

crawl.%: ## Run crawler for env (e.g., make crawl.local seeds_20260626.txt)
	VOYAGE_API_KEY="$(call _voyage_key,$*)" go run ./cmd/crawler -seeds cmd/crawler/data/$(SEEDS)

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
