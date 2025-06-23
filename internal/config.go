package internal

import (
	"fmt"
	"regexp"
	"runtime"
	"time"
)

var PoolIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateConfig validates the configuration and applies defaults
func ValidateConfig(config *Configuration) error {
	if config.RootConnection == nil {
		return fmt.Errorf("RootConnection must not be nil")
	}

	if config.PoolID == "" {
		return fmt.Errorf("PoolID must not be empty")
	}

	if len(config.PoolID) > 50 {
		return fmt.Errorf("PoolID must be 50 characters or less")
	}

	if !PoolIDRegex.MatchString(config.PoolID) {
		return fmt.Errorf("PoolID must contain only alphanumeric characters and underscores")
	}

	if config.TemplateCreator == nil {
		return fmt.Errorf("TemplateCreator must not be nil")
	}

	if config.ResetFunc == nil {
		return fmt.Errorf("ResetFunc must not be nil")
	}

	// Apply defaults
	if config.StateDatabase == "" {
		config.StateDatabase = "postgres"
	}

	if config.MaxPoolSize <= 0 {
		config.MaxPoolSize = runtime.GOMAXPROCS(0) * 2
	}

	if config.AcquireTimeout <= 0 {
		config.AcquireTimeout = 30 * time.Second
	}

	return nil
}