package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"feature-flag/internal/config"
	"feature-flag/internal/database"
	"feature-flag/internal/domain"
	"feature-flag/internal/flag"
	"feature-flag/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type IntegrationTestSuite struct {
	suite.Suite
	server      *httptest.Server
	client      *http.Client
	apiKey      string
	otherAPIKey string
	db          domain.DB
	cache       domain.Cache
}

func (suite *IntegrationTestSuite) SetupSuite() {
	gin.SetMode(gin.ReleaseMode)

	// Load configuration
	cfg, err := config.LoadConfig("../config.yaml")
	if err != nil {
		suite.T().Fatalf("Failed to load test config: %v", err)
	}

	ctx := context.Background()

	// Read the production migration SQL file
	migrationSQL, err := os.ReadFile("../migrations/000001_create_flags_table.up.sql")
	if err != nil {
		suite.T().Fatalf("Failed to read production migration file: %v", err)
	}

	// Establish connection directly to run the production migration on the database
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.Postgres.User, cfg.Postgres.Password, cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.DBName, cfg.Postgres.SSLMode)
	
	initPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		suite.T().Fatalf("Failed to connect for schema initialization: %v", err)
	}
	
	_, _ = initPool.Exec(ctx, "DROP TABLE IF EXISTS flags CASCADE;")
	_, err = initPool.Exec(ctx, string(migrationSQL))
	initPool.Close()
	if err != nil {
		suite.T().Fatalf("Failed to initialize database schema using production migration: %v", err)
	}

	// Connect to real PostgreSQL pool
	db, err := database.NewPostgresDB(ctx, cfg.Postgres)
	if err != nil {
		suite.T().Fatalf("Failed to connect to PostgreSQL connection pool: %v", err)
	}
	suite.db = db

	// Connect to real Redis
	cache, err := database.NewRedisCache(cfg.Redis)
	if err != nil {
		suite.T().Fatalf("Failed to connect to real Redis: %v", err)
	}
	suite.cache = cache

	// Clean up previous run data from DB & Redis cache to make test idempotent
	_ = db.DeleteFlag(ctx, "new-checkout-flow", "prod")
	_ = cache.Delete(ctx, "flag:prod:new-checkout-flow")
	_ = cache.Delete(ctx, "rate_limit:integration-test-api-key")

	// Setup Clean Architecture layers
	usecase := flag.NewUsecase(db, cache)
	handler := flag.NewHandler(usecase)

	router := gin.New()
	router.Use(gin.Recovery())

	suite.apiKey = "integration-test-api-key"
	suite.otherAPIKey = "unauthorized-test-api-key"
	clients := []config.ClientConfig{
		{
			Name:   "integration-test-client",
			APIKey: suite.apiKey,
			RateLimit: config.RateLimiterConfig{
				Capacity:   5,
				RefillRate: 1.0,
			},
		},
		{
			Name:   "unauthorized-client",
			APIKey: suite.otherAPIKey,
			RateLimit: config.RateLimiterConfig{
				Capacity:   5,
				RefillRate: 1.0,
			},
		},
	}
	authMiddleware := middleware.AuthMiddleware(clients)
	rateLimitMiddleware := middleware.RateLimitMiddleware(cache)

	// Register Routes
	handler.RegisterRoutes(router, authMiddleware, rateLimitMiddleware)

	// Health Check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	// Start the actual local TCP server
	suite.server = httptest.NewServer(router)
	suite.client = suite.server.Client()
}

func (suite *IntegrationTestSuite) TearDownSuite() {
	suite.server.Close()
	if suite.db != nil {
		suite.db.Close()
	}
	if suite.cache != nil {
		_ = suite.cache.Close()
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

func (suite *IntegrationTestSuite) TestEndToEndFlow() {
	serverURL := suite.server.URL

	// 1. Health check (unauthenticated)
	resp, err := suite.client.Get(serverURL + "/health")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(suite.T(), string(body), "healthy")
	resp.Body.Close()
	log.Println("[STEP 1 SUCCESS] Health check endpoint verified successfully")

	// 2. Unauthorized request to CRUD flags
	req, _ := http.NewRequest("GET", serverURL+"/flags", nil)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()
	log.Println("[STEP 2 SUCCESS] Unauthorized access block verified successfully")

	// 3. Create a feature flag (Authenticated)
	newFlag := domain.Flag{
		Key:               "new-checkout-flow",
		Environment:       "prod",
		Enabled:           true,
		RolloutPercentage: 100,
	}
	payload, _ := json.Marshal(newFlag)

	req, _ = http.NewRequest("POST", serverURL+"/flags", bytes.NewBuffer(payload))
	req.Header.Set("X-API-Key", suite.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusCreated, resp.StatusCode)

	var createdFlag domain.Flag
	_ = json.NewDecoder(resp.Body).Decode(&createdFlag)
	assert.Equal(suite.T(), "new-checkout-flow", createdFlag.Key)
	assert.Equal(suite.T(), "prod", createdFlag.Environment)
	assert.True(suite.T(), createdFlag.Enabled)
	assert.Equal(suite.T(), 100, createdFlag.RolloutPercentage)
	resp.Body.Close()
	log.Println("[STEP 3 SUCCESS] Feature flag creation verified successfully")

	// 3b. Unauthorized attempt to modify/delete another client's flag
	// Try updating the flag using otherAPIKey (owned by unauthorized-client, not integration-test-client)
	unauthFlag := domain.Flag{
		Key:               "new-checkout-flow",
		Environment:       "prod",
		Enabled:           false,
		RolloutPercentage: 50,
	}
	unauthPayload, _ := json.Marshal(unauthFlag)

	req, _ = http.NewRequest("PUT", serverURL+"/flags/new-checkout-flow", bytes.NewBuffer(unauthPayload))
	req.Header.Set("X-API-Key", suite.otherAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusForbidden, resp.StatusCode)
	
	var errResponse map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&errResponse)
	assert.Contains(suite.T(), errResponse["error"], "unauthorized: you do not own this flag")
	resp.Body.Close()

	// Try deleting the flag using otherAPIKey
	req, _ = http.NewRequest("DELETE", serverURL+"/flags/new-checkout-flow?env=prod", nil)
	req.Header.Set("X-API-Key", suite.otherAPIKey)

	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusForbidden, resp.StatusCode)
	
	_ = json.NewDecoder(resp.Body).Decode(&errResponse)
	assert.Contains(suite.T(), errResponse["error"], "unauthorized: you do not own this flag")
	resp.Body.Close()
	log.Println("[STEP 3b SUCCESS] Flag ownership protection verified successfully")

	// 4. Get the single flag
	req, _ = http.NewRequest("GET", serverURL+"/flags/new-checkout-flow?env=prod", nil)
	req.Header.Set("X-API-Key", suite.apiKey)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	var fetchedFlag domain.Flag
	_ = json.NewDecoder(resp.Body).Decode(&fetchedFlag)
	assert.Equal(suite.T(), "new-checkout-flow", fetchedFlag.Key)
	resp.Body.Close()
	log.Println("[STEP 4 SUCCESS] Fetching single flag verified successfully")

	// 5. Get missing flag -> 404
	req, _ = http.NewRequest("GET", serverURL+"/flags/missing-flag?env=prod", nil)
	req.Header.Set("X-API-Key", suite.apiKey)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
	log.Println("[STEP 5 SUCCESS] Fetching non-existent flag correctly returns 404 StatusNotFound")

	// 6. Evaluate flag -> should be true (rollout 100%, enabled)
	req, _ = http.NewRequest("GET", serverURL+"/evaluate/new-checkout-flow?env=prod&caller_id=user1", nil)
	req.Header.Set("X-API-Key", suite.apiKey)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	var evalRes map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&evalRes)
	assert.Equal(suite.T(), true, evalRes["enabled"])
	resp.Body.Close()
	log.Println("[STEP 6 SUCCESS] Flag evaluation (enabled: true) verified successfully")

	// 7. Update flag (Set Enabled to False)
	updatedFlag := domain.Flag{
		Key:               "new-checkout-flow",
		Environment:       "prod",
		Enabled:           false,
		RolloutPercentage: 100,
	}
	updatePayload, _ := json.Marshal(updatedFlag)

	req, _ = http.NewRequest("PUT", serverURL+"/flags/new-checkout-flow", bytes.NewBuffer(updatePayload))
	req.Header.Set("X-API-Key", suite.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	resp.Body.Close()
	log.Println("[STEP 7 SUCCESS] Updating feature flag status verified successfully")

	// 8. Re-evaluate flag -> should now be false
	req, _ = http.NewRequest("GET", serverURL+"/evaluate/new-checkout-flow?env=prod&caller_id=user1", nil)
	req.Header.Set("X-API-Key", suite.apiKey)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	_ = json.NewDecoder(resp.Body).Decode(&evalRes)
	assert.Equal(suite.T(), false, evalRes["enabled"])
	resp.Body.Close()
	log.Println("[STEP 8 SUCCESS] Evaluating updated flag (enabled: false) verified successfully")

	// 9. List all flags
	req, _ = http.NewRequest("GET", serverURL+"/flags?env=prod", nil)
	req.Header.Set("X-API-Key", suite.apiKey)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	var flagsList []domain.Flag
	_ = json.NewDecoder(resp.Body).Decode(&flagsList)
	
	// Verify that the created flag is present in the list (making the test robust against pre-existing flags)
	var found bool
	for _, f := range flagsList {
		if f.Key == "new-checkout-flow" {
			found = true
			break
		}
	}
	assert.True(suite.T(), found, "Expected created flag 'new-checkout-flow' to be in the list")
	resp.Body.Close()
	log.Println("[STEP 9 SUCCESS] Listing all flags verified successfully")

	// 10. Delete flag
	req, _ = http.NewRequest("DELETE", serverURL+"/flags/new-checkout-flow?env=prod", nil)
	req.Header.Set("X-API-Key", suite.apiKey)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusNoContent, resp.StatusCode)
	resp.Body.Close()
	log.Println("[STEP 10 SUCCESS] Deleting feature flag verified successfully")

	// 11. Re-evaluate deleted flag -> should return 404
	req, _ = http.NewRequest("GET", serverURL+"/evaluate/new-checkout-flow?env=prod&caller_id=user1", nil)
	req.Header.Set("X-API-Key", suite.apiKey)
	resp, err = suite.client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
	log.Println("[STEP 11 SUCCESS] Evaluating deleted flag correctly returns 404 StatusNotFound")
}
