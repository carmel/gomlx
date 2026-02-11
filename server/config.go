package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTPPort     int           `yaml:"http_port"`
	GRPCAddress  string        `yaml:"grpc_address"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	LogLevel     string        `yaml:"log_level"`
	MetricsPath  string        `yaml:"metrics_path"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}
	if cfg.GRPCAddress == "" {
		cfg.GRPCAddress = "127.0.0.1:50051"
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 5 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 120 * time.Second
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.MetricsPath == "" {
		cfg.MetricsPath = "/metrics"
	}

	return cfg, nil
}
