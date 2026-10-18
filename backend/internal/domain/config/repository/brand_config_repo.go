package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
)

// BrandConfigRepo 对应 brand_configs 表（单例 id=1）。
type BrandConfigRepo struct {
	pool *pgxpool.Pool
}

// NewBrandConfigRepo 创建 BrandConfigRepo。
func NewBrandConfigRepo(pool *pgxpool.Pool) *BrandConfigRepo {
	return &BrandConfigRepo{pool: pool}
}

// Get 读取单例配置。未找到返回 ErrNotFound（service 层用默认值兜底/自愈）。
func (r *BrandConfigRepo) Get(ctx context.Context) (*entity.BrandConfig, error) {
	q := `SELECT id, name, wordmark, org_name, slogan, site_url, logo_url, site_title, favicon_url, updated_at
		FROM brand_configs WHERE id = 1`
	c := &entity.BrandConfig{}
	err := r.pool.QueryRow(ctx, q).Scan(
		&c.ID, &c.Name, &c.Wordmark, &c.OrgName, &c.Slogan, &c.SiteURL,
		&c.LogoURL, &c.SiteTitle, &c.FaviconURL, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan brand_config: %w", err)
	}
	return c, nil
}

// Upsert 插入或更新单例配置（id=1）。
func (r *BrandConfigRepo) Upsert(ctx context.Context, c *entity.BrandConfig) error {
	q := `INSERT INTO brand_configs (id, name, wordmark, org_name, slogan, site_url, logo_url, site_title, favicon_url, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			wordmark = EXCLUDED.wordmark,
			org_name = EXCLUDED.org_name,
			slogan = EXCLUDED.slogan,
			site_url = EXCLUDED.site_url,
			logo_url = EXCLUDED.logo_url,
			site_title = EXCLUDED.site_title,
			favicon_url = EXCLUDED.favicon_url,
			updated_at = now()
		RETURNING updated_at`
	return r.pool.QueryRow(ctx, q,
		c.Name, c.Wordmark, c.OrgName, c.Slogan, c.SiteURL,
		c.LogoURL, c.SiteTitle, c.FaviconURL,
	).Scan(&c.UpdatedAt)
}
