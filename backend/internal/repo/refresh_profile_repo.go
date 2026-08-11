package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"backend/internal/model"
	"gorm.io/gorm"
)

type RefreshProfileRepository struct {
	db *gorm.DB
}

func NewRefreshProfileRepository(db *gorm.DB) *RefreshProfileRepository {
	return &RefreshProfileRepository{db: db}
}

func (r *RefreshProfileRepository) List(ctx context.Context) ([]model.RefreshProfile, error) {
	var items []model.RefreshProfile
	if err := r.db.WithContext(ctx).
		Order("created_at desc").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *RefreshProfileRepository) Get(ctx context.Context, id string) (*model.RefreshProfile, error) {
	var item model.RefreshProfile
	if err := r.db.WithContext(ctx).First(&item, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetByPoolCookie finds an existing refresh profile for the exact cookie. Adobe
// account tokens are minted asynchronously, so this blocks repeated cookies
// before a second pending token row can be created.
func (r *RefreshProfileRepository) GetByPoolCookie(ctx context.Context, pool, cookie string) (*model.RefreshProfile, error) {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return nil, nil
	}
	var item model.RefreshProfile
	err := r.db.WithContext(ctx).
		Where("pool = ? AND cookie = ?", pool, cookie).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *RefreshProfileRepository) Create(ctx context.Context, item *model.RefreshProfile) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *RefreshProfileRepository) Update(ctx context.Context, id string, patch map[string]any) (*model.RefreshProfile, error) {
	patch["updated_at"] = time.Now()
	if err := r.db.WithContext(ctx).
		Model(&model.RefreshProfile{}).
		Where("id = ?", id).
		Updates(patch).Error; err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *RefreshProfileRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.RefreshProfile{}, "id = ?", id).Error
}

func (r *RefreshProfileRepository) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Delete(&model.RefreshProfile{}, "id IN ?", ids).Error
}

// ListDue returns enabled profiles whose next_retry_at has passed (or is unset,
// e.g. freshly imported). The background maintenance loop refreshes these.
func (r *RefreshProfileRepository) ListDue(ctx context.Context, now time.Time) ([]model.RefreshProfile, error) {
	var items []model.RefreshProfile
	if err := r.db.WithContext(ctx).
		Where("enabled = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)", true, now).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
