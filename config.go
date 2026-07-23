package main

import (
	"net"
	"os"
)

type Config struct {
	Port            string
	BindAddr        string
	DBPath          string
	ImagesDir       string
	LocalWriteToken string
}

func LoadConfig() Config {
	return Config{
		Port:            envOrDefault("PORT", "8080"),
		BindAddr:        envOrDefault("BIND_ADDR", "127.0.0.1"),
		DBPath:          envOrDefault("DB_PATH", "./data/cocktail.db"),
		ImagesDir:       envOrDefault("IMAGES_DIR", "./data/images"),
		LocalWriteToken: os.Getenv("LOCAL_WRITE_TOKEN"),
	}
}

func (c Config) ListenAddr() string {
	return net.JoinHostPort(c.BindAddr, c.Port)
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
