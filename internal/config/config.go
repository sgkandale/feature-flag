package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Postgres PostgresConfig `yaml:"postgres"`
	Redis    RedisConfig    `yaml:"redis"`
	Clients  []ClientConfig `yaml:"clients"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
	MaxConns int    `yaml:"max_conns"`
	MinConns int    `yaml:"min_conns"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type ClientConfig struct {
	Name      string            `yaml:"name" json:"name"`
	APIKey    string            `yaml:"api_key" json:"api_key"`
	RateLimit RateLimiterConfig `yaml:"rate_limit" json:"rate_limit"`
}

type RateLimiterConfig struct {
	Capacity   int     `yaml:"capacity" json:"capacity"`
	RefillRate float64 `yaml:"refill_rate" json:"refill_rate"`
}

func LoadConfig(yamlPath string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(yamlPath)
	if err == nil {
		err = yaml.Unmarshal(data, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse yaml config: %w", err)
		}
		return cfg, nil
	}

	log.Println("Config YAML not found or unreadable, falling back to environment variables")
	cfg.Server.Host = getEnv("SERVER_HOST", "0.0.0.0")
	cfg.Server.Port = getEnvAsInt("SERVER_PORT", 8080)

	cfg.Postgres.Host = getEnv("POSTGRES_HOST", "localhost")
	cfg.Postgres.Port = getEnvAsInt("POSTGRES_PORT", 5432)
	cfg.Postgres.User = getEnv("POSTGRES_USER", "postgres")
	cfg.Postgres.Password = getEnv("POSTGRES_PASSWORD", "password")
	cfg.Postgres.DBName = getEnv("POSTGRES_DBNAME", "feature_flag_db")
	cfg.Postgres.SSLMode = getEnv("POSTGRES_SSLMODE", "disable")
	cfg.Postgres.MaxConns = getEnvAsInt("POSTGRES_MAX_CONNS", 20)
	cfg.Postgres.MinConns = getEnvAsInt("POSTGRES_MIN_CONNS", 2)

	cfg.Redis.Host = getEnv("REDIS_HOST", "localhost")
	cfg.Redis.Port = getEnvAsInt("REDIS_PORT", 6379)
	cfg.Redis.Password = getEnv("REDIS_PASSWORD", "")
	cfg.Redis.DB = getEnvAsInt("REDIS_DB", 0)

	if clientsJSON, exists := os.LookupEnv("CLIENTS_JSON"); exists && clientsJSON != "" {
		var envClients []ClientConfig
		if err := json.Unmarshal([]byte(clientsJSON), &envClients); err == nil {
			cfg.Clients = envClients
		} else {
			log.Printf("Warning: failed to parse CLIENTS_JSON env var: %v\n", err)
		}
	}

	if len(cfg.Clients) == 0 {
		defaultKey := getEnv("SERVER_API_KEY", "admin-secret-key")
		defaultCapacity := getEnvAsInt("RATE_LIMITER_CAPACITY", 10)
		defaultRefill := getEnvAsFloat("RATE_LIMITER_REFILL_RATE", 2.0)

		cfg.Clients = []ClientConfig{
			{
				Name:   "default-admin",
				APIKey: defaultKey,
				RateLimit: RateLimiterConfig{
					Capacity:   defaultCapacity,
					RefillRate: defaultRefill,
				},
			},
		}
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func getEnvAsInt(name string, defaultVal int) int {
	valueStr := getEnv(name, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultVal
}

func getEnvAsFloat(name string, defaultVal float64) float64 {
	valueStr := getEnv(name, "")
	if value, err := strconv.ParseFloat(valueStr, 64); err == nil {
		return value
	}
	return defaultVal
}
