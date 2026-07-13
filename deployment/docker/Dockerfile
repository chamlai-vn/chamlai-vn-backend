# ---- Build stage ----
FROM golang:1.25 AS build
WORKDIR /src

# Cache deps (only rebuild this layer when go.mod/go.sum changes)
COPY go.mod go.sum ./
RUN go mod download

# Copy code & build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -tags swagger -ldflags="-s -w" -o /out/api ./cmd/api

# ---- Runtime stage ----
FROM gcr.io/distroless/static-debian12 AS runtime
WORKDIR /app
COPY --from=build /out/api /app/api
EXPOSE 8080
ENTRYPOINT ["/app/api"]
