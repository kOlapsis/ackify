// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

type ConfigRepository struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

func NewConfigRepository(db *sql.DB, tenants providers.TenantProvider) *ConfigRepository {
	return &ConfigRepository{db: db, tenants: tenants}
}

// GetByCategory retrieves a configuration section by category
func (r *ConfigRepository) GetByCategory(ctx context.Context, category models.ConfigCategory) (*models.TenantConfig, error) {
	query := `
		SELECT id, tenant_id, category, config, secrets_encrypted, version, created_at, updated_at, updated_by
		FROM tenant_config
		WHERE category = $1
	`
	cfg := &models.TenantConfig{}
	var updatedBy sql.NullString
	var secretsEncrypted []byte

	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, string(category)).Scan(
		&cfg.ID, &cfg.TenantID, &cfg.Category, &cfg.Config, &secretsEncrypted,
		&cfg.Version, &cfg.CreatedAt, &cfg.UpdatedAt, &updatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get config by category: %w", err)
	}

	cfg.SecretsEncrypted = secretsEncrypted
	if updatedBy.Valid {
		cfg.UpdatedBy = &updatedBy.String
	}
	return cfg, nil
}

// GetAll retrieves all configuration sections for the current tenant
func (r *ConfigRepository) GetAll(ctx context.Context) ([]*models.TenantConfig, error) {
	query := `
		SELECT id, tenant_id, category, config, secrets_encrypted, version, created_at, updated_at, updated_by
		FROM tenant_config
		ORDER BY category
	`
	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list configs: %w", err)
	}
	defer rows.Close()

	var configs []*models.TenantConfig
	for rows.Next() {
		cfg := &models.TenantConfig{}
		var updatedBy sql.NullString
		var secretsEncrypted []byte

		if err := rows.Scan(
			&cfg.ID, &cfg.TenantID, &cfg.Category, &cfg.Config, &secretsEncrypted,
			&cfg.Version, &cfg.CreatedAt, &cfg.UpdatedAt, &updatedBy,
		); err != nil {
			return nil, fmt.Errorf("failed to scan config row: %w", err)
		}

		cfg.SecretsEncrypted = secretsEncrypted
		if updatedBy.Valid {
			cfg.UpdatedBy = &updatedBy.String
		}
		configs = append(configs, cfg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating config rows: %w", err)
	}

	return configs, nil
}

// Upsert creates or updates a configuration section
func (r *ConfigRepository) Upsert(ctx context.Context, category models.ConfigCategory, config json.RawMessage, secrets []byte, updatedBy string) error {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `
		INSERT INTO tenant_config (tenant_id, category, config, secrets_encrypted, updated_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, category)
		DO UPDATE SET
			config = EXCLUDED.config,
			secrets_encrypted = COALESCE(EXCLUDED.secrets_encrypted, tenant_config.secrets_encrypted),
			updated_by = EXCLUDED.updated_by
	`

	var secretsArg interface{}
	if len(secrets) > 0 {
		secretsArg = secrets
	}

	_, err = dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query,
		tenantID, string(category), config, secretsArg, updatedBy,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert config: %w", err)
	}

	return nil
}

// IsSeeded checks if configuration has been seeded from environment variables
func (r *ConfigRepository) IsSeeded(ctx context.Context) (bool, error) {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `SELECT config_seeded_at FROM instance_metadata WHERE id = $1`
	var seededAt sql.NullTime
	err = r.db.QueryRowContext(ctx, query, tenantID).Scan(&seededAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check seeded status: %w", err)
	}

	return seededAt.Valid, nil
}

// MarkSeeded marks configuration as seeded from environment variables
func (r *ConfigRepository) MarkSeeded(ctx context.Context) error {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `UPDATE instance_metadata SET config_seeded_at = NOW() WHERE id = $1`
	_, err = r.db.ExecContext(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("failed to mark config as seeded: %w", err)
	}

	return nil
}

// ClearSeeded clears the seeded flag (for reset functionality)
func (r *ConfigRepository) ClearSeeded(ctx context.Context) error {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `UPDATE instance_metadata SET config_seeded_at = NULL WHERE id = $1`
	_, err = r.db.ExecContext(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("failed to clear seeded status: %w", err)
	}

	return nil
}

// DeleteAll removes all configuration for the current tenant (for reset)
func (r *ConfigRepository) DeleteAll(ctx context.Context) error {
	query := `DELETE FROM tenant_config`
	_, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to delete all configs: %w", err)
	}
	return nil
}

// GetTenantID returns the current tenant ID
func (r *ConfigRepository) GetTenantID(ctx context.Context) (uuid.UUID, error) {
	return r.tenants.CurrentTenant(ctx)
}

// GetLatestUpdatedAt returns the most recent updated_at across all config sections
func (r *ConfigRepository) GetLatestUpdatedAt(ctx context.Context) (time.Time, error) {
	query := `SELECT COALESCE(MAX(updated_at), NOW()) FROM tenant_config`
	var updatedAt time.Time
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query).Scan(&updatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get latest updated_at: %w", err)
	}
	return updatedAt, nil
}
