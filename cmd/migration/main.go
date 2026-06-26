// Command migration runs database schema migrations using goose.
//
// Usage:
//
//	go run ./cmd/migration <command> [args]
//
// Commands mirror the goose CLI: up, down, status, version, redo, reset.
// DATABASE_URL is read from .env (via godotenv) then the OS environment,
// falling back to the local docker-compose DSN.
//
// Examples:
//
//	go run ./cmd/migration status
//	go run ./cmd/migration up
//	go run ./cmd/migration down
package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"runtime"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
)

const defaultDSN = "postgres://chamlai:chamlai@localhost:5432/chamlai?sslmode=disable"

// migrationsDir resolves the migrations folder relative to this source file so
// the command works regardless of the working directory it is invoked from.
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: migrate <up|down|status|version|redo|reset>")
	}

	_ = godotenv.Load() // missing .env is fine

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultDSN
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("migration: open db: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("migration: set dialect: %v", err)
	}

	ctx := context.Background()
	if err := goose.RunContext(ctx, os.Args[1], db, migrationsDir(), os.Args[2:]...); err != nil {
		log.Fatalf("migration: %v", err)
	}
}
