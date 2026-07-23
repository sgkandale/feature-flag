# Feature Flag Service (Go + Postgres + Redis)

A production-ready, high-performance Feature Flag Service built in Go using **Standard Go Clean Architecture**. It supports dynamic feature toggles, environment-specific flag settings, consistent gradual rollouts, token bucket rate limiting per client API key, and write-through caching.

---

## 🚀 Key Architectural & Design Features

1. **Standard Clean Architecture**:
   - Distinct layers for **Domain** (interfaces and models), **Database/Cache** (implementations), **Usecases** (business logic), and **Handlers** (HTTP endpoints).
   - Strict dependency rules: outer layers depend on inner layers, not vice-versa, making it fully mockable and testable.
2. **API Layer Decoupling (DTOs)**:
   - Added request/response DTOs ([dto.go](file:///home/shantanu/Projects/sgkandale/feature-flag/internal/flag/dto.go)) to separate API schema representations from internal domain structures.
   - Request DTOs (`CreateFlagRequest`, `UpdateFlagRequest`) implement explicit validation via `Validate() error`, returning structured Bad Request responses to clients on failure.
   - Removed all web/validation binding tags from the core `domain.Flag` struct to isolate domain entities from external web frameworks.
3. **High-Performance Write-Through Cache & Singleflight**:
   - **Write-Through Caching**: Feature flag creations and updates write directly to the Redis cache in addition to PostgreSQL, minimizing cold-start database queries.
   - **Singleflight De-duplication**: Uses `golang.org/x/sync/singleflight` to coalesce concurrent reads during cache misses, shielding PostgreSQL from read stampedes.
   - **Double-Checked Locking**: Inside the singleflight block, the cache is queried *again* before falling back to PostgreSQL, checking if a concurrent thread just populated the cache a microsecond ago.
4. **Centralized Error & Cache Handling**:
   - Replaced raw string-based error handling with package-level error constants (`domain.ErrFlagNotFound` and `domain.ErrCacheNotFound`).
   - The Cache `Get` interface returns `domain.ErrCacheNotFound` on key miss rather than an empty string, allowing clean and type-safe cache hit logic (`err == nil`) in use cases.
   - HTTP status codes are mapped cleanly based on errors (`404` for missing flags, `500` for connection or pool faults).
5. **Dynamic Lua-Based Rate Limiting**:
   - Enforces configurable rate limits dynamically based on the client's API Key.
   - Evaluated atomically inside Redis via a **Lua Script**, preventing concurrent race conditions (like double-spends) without optimistic lock retries.
6. **Observability & Graceful Operations**:
   - Dedicated [health.Handler](file:///home/shantanu/Projects/sgkandale/feature-flag/internal/health/handler.go) performs concurrent PostgreSQL and Redis pings with a 2-second timeout, returning structured JSON health reports (`503 Service Unavailable` if downstream connections are offline).
   - Structured JSON request logging logs all requests, response latencies, and route execution errors collected via `c.Error(err)`.
   - Removed `log.Fatalf` calls from critical startup and shutdown sequences to guarantee registered `defer` statements (closing Postgres connection pools and Redis clients) always execute on startup or shutdown errors.

---

## 🛠️ Tech Stack
- **Language**: Go (v1.25+ preferred)
- **HTTP Router**: Gin
- **Database**: PostgreSQL / CockroachDB (Driver: `jackc/pgx/v5`)
- **Cache / Rate Limiter**: Redis (Client: `go-redis/v9`)

---

## 📂 Project Directory Structure

```text
feature-flag/
├── cmd/
│   └── server/
│       └── main.go             # Application entry point, dependency injection, graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go           # YAML & Environment variable configuration loader (Client specific keys/rates)
│   ├── database/
│   │   ├── postgres.go         # pgxpool-backed implementation of domain.DB
│   │   └── redis.go            # go-redis-backed implementation of domain.Cache (with Rate Limiting Lua script)
│   ├── domain/
│   │   ├── constants.go        # Context and request constants
│   │   ├── errors.go           # Centralized domain error variables
│   │   ├── flag.go             # Feature flag domain entities (Decoupled from tags)
│   │   ├── interfaces.go       # Contracts for DB, Cache, and health checks
│   │   └── mocks.go            # Thread-safe in-memory database and cache mocks for testing
│   ├── flag/
│   │   ├── dto.go              # Request / Response DTOs and explicit validation rules
│   │   ├── handler.go          # HTTP API routes mapping DTOs to use cases
│   │   └── usecase.go          # Business logic (rollout evaluations, write-through cache, singleflight)
│   ├── health/
│   │   └── handler.go          # Decoupled concurrent service health check
│   └── middleware/
│       ├── auth.go             # Dynamic API key authentication & client limit context propagation
│       ├── logger.go           # Structured JSON request logger
│       └── rate_limit.go       # Token Bucket throttling using client contexts and Lua scripts
├── migrations/
│   └── 000001_create_flags_table.up.sql  # SQL schema migrations
├── tests/
│   └── integration_test.go     # End-to-end integration tests using cloud databases, step logging, and schema migration
├── config.yaml                 # Configuration file supporting multiple client profiles
├── docker-compose.yml          # Container configuration for local Postgres and Redis
└── design.md                   # Comprehensive High-Level and Low-Level Design Document
```

---

## ⚙️ Running Locally

### 1. Start Infrastructure
Run the following command to start PostgreSQL and Redis using Docker Compose:
```bash
docker-compose up -d
```

### 2. Run Database Migrations
Create the flags table in PostgreSQL by executing the contents of [migrations/000001_create_flags_table.up.sql](file:///home/shantanu/Projects/sgkandale/feature-flag/migrations/000001_create_flags_table.up.sql) on your database client.

### 3. Run the Server
```bash
go run cmd/server/main.go
```

---

## 🧪 Testing

### Running Unit & Integration Tests
Unit tests use thread-safe mock repositories. The integration test spins up a local HTTP server and executes end-to-end requests against real PostgreSQL and Redis databases to verify the entire system.

**Before running integration tests**, ensure the configuration in `config.yaml` points to valid database and cache instances. The test suite automatically reads and applies the production schema migration SQL file ([migrations/000001_create_flags_table.up.sql](file:///home/shantanu/Projects/sgkandale/feature-flag/migrations/000001_create_flags_table.up.sql)) before executing the tests.

Run all tests:
```bash
go test -v ./...
```
