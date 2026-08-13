package repo

import (
	"context"
	"time"

	"backend/internal/model"
	"gorm.io/gorm"
)

type APIKeyRepository struct {
	db *gorm.DB
}

func NewAPIKeyRepository(db *gorm.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

func (r *APIKeyRepository) ListByUserID(ctx context.Context, userID string) ([]model.APIKey, error) {
	var keys []model.APIKey
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at asc").Find(&keys).Error; err != nil {
		return nil, err
	}
	return keys, nil
}

func (r *APIKeyRepository) DeleteByUserID(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.APIKey{}).Error
}

func (r *APIKeyRepository) DeleteByID(ctx context.Context, userID, keyID string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND id = ?", userID, keyID).
		Delete(&model.APIKey{}).Error
}

func (r *APIKeyRepository) Create(ctx context.Context, key *model.APIKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

func (r *APIKeyRepository) TouchUsage(ctx context.Context, keyHash string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.APIKey{}).Where("key_hash = ?", keyHash).Update("last_used_at", now).Error
}
