package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"backend/internal/model"
	"backend/internal/repo"
)

type APIKeyService struct {
	keys *repo.APIKeyRepository
}

func NewAPIKeyService(keys *repo.APIKeyRepository) *APIKeyService {
	return &APIKeyService{keys: keys}
}

func (s *APIKeyService) Current(ctx context.Context, userID string) (map[string]any, error) {
	keys, err := s.keys.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		items = append(items, apiKeyData(key))
	}
	return map[string]any{"keys": items}, nil
}

func (s *APIKeyService) Mint(ctx context.Context, userID string) (map[string]any, error) {
	plain, err := generatePlainAPIKey()
	if err != nil {
		return nil, err
	}
	key := &model.APIKey{
		ID:         newAPIKeyID(),
		UserID:     userID,
		Name:       "API Key",
		Plaintext:  plain,
		KeyPreview: previewAPIKey(plain),
		KeyHash:    hashAPIKey(plain),
		CreatedAt:  time.Now(),
	}
	if err := s.keys.Create(ctx, key); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":   true,
		"data": apiKeyData(*key),
	}, nil
}

func (s *APIKeyService) Revoke(ctx context.Context, userID string) error {
	return s.keys.DeleteByUserID(ctx, userID)
}

func (s *APIKeyService) DeleteOne(ctx context.Context, userID, keyID string) error {
	if strings.TrimSpace(keyID) == "" {
		return errors.New("key id required")
	}
	return s.keys.DeleteByID(ctx, userID, keyID)
}

func apiKeyData(key model.APIKey) map[string]any {
	return map[string]any{
		"id":           key.ID,
		"name":         key.Name,
		"key":          key.Plaintext,
		"created_at":   key.CreatedAt,
		"last_used_at": key.LastUsedAt,
	}
}

func newAPIKeyID() string {
	return "k-" + time.Now().Format("150405") + randomSuffix(10)
}

func generatePlainAPIKey() (string, error) {
	return "sk-" + randomUpper(38), nil
}

func previewAPIKey(plain string) string {
	if len(plain) <= 4 {
		return strings.Repeat("•", len(plain))
	}
	return "…" + plain[len(plain)-4:]
}

func hashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func randomSuffix(n int) string {
	return randomUpper(n)
}
