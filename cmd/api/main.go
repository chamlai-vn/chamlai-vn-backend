package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://chamlai:chamlai@localhost:5432/chamlai?sslmode=disable"
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("không connect được DB: %v", err)
	}
	defer conn.Close(ctx)

	// Ping: ép pgvector tính khoảng cách 2 vector → vừa test conn vừa test extension
	var dist float64
	err = conn.QueryRow(ctx, `SELECT '[1,2,3]'::vector <=> '[1,2,4]'::vector`).Scan(&dist)
	if err != nil {
		log.Fatalf("query lỗi (pgvector chưa enable?): %v", err)
	}
	fmt.Printf("✅ DB OK, pgvector sống. cosine distance = %f\n", dist)
}
