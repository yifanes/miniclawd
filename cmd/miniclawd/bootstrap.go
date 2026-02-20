package cmd

import (
	"fmt"

	"github.com/yifanes/miniclawd/internal/config"
	"github.com/yifanes/miniclawd/internal/storage"
)

// bootstrap loads config and opens the database.
func bootstrap() (*config.Config, *storage.Database, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("config validation: %w", err)
	}

	db, err := storage.Open(cfg.DBPath())
	if err != nil {
		return nil, nil, fmt.Errorf("opening database: %w", err)
	}

	return cfg, db, nil
}
