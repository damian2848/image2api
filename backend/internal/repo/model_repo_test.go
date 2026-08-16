package repo

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestModelRepositoryGetResolvesCanonicalIDAndAlias(t *testing.T) {
	var output bytes.Buffer
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "host=localhost user=test dbname=test sslmode=disable",
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger: logger.New(log.New(&output, "", 0), logger.Config{
			LogLevel:             logger.Info,
			Colorful:             false,
			ParameterizedQueries: false,
		}),
	})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}

	repository := NewModelRepository(db)
	_, _ = repository.Get(context.Background(), "firefly-gpt-image-2")

	query := output.String()
	if !strings.Contains(query, `id = 'firefly-gpt-image-2'`) {
		t.Fatalf("canonical model ID is not queryable: %s", query)
	}
	if !strings.Contains(query, `alias = 'firefly-gpt-image-2'`) {
		t.Fatalf("model alias is not queryable: %s", query)
	}
}
