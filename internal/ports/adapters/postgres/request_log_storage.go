package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chempik1234/room-service-proxy/internal/models"
)

// PostgresRequestLogStorage implements RequestLogStorage using PostgreSQL
type PostgresRequestLogStorage struct {
	db *pgxpool.Pool
}

// NewPostgresRequestLogStorage creates a new PostgreSQL request log storage
func NewPostgresRequestLogStorage(db *pgxpool.Pool) (*PostgresRequestLogStorage, error) {
	if db == nil {
		return nil, fmt.Errorf("database pool cannot be nil")
	}
	return &PostgresRequestLogStorage{db: db}, nil
}

// CreateRequestLog logs a single request for analytics
func (s *PostgresRequestLogStorage) CreateRequestLog(ctx context.Context, log *models.RequestLog) error {
	query := `
		INSERT INTO request_logs (tenant_id, method, request_type, status_code, latency_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`
	_, err := s.db.Exec(ctx, query,
		log.TenantID, log.Method, log.RequestType, log.StatusCode, log.LatencyMs)
	return err
}

// GetRecentRequestCount counts requests within a time window
func (s *PostgresRequestLogStorage) GetRecentRequestCount(ctx context.Context, duration string) (int64, error) {
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM request_logs WHERE created_at > NOW() - INTERVAL '%s'", duration)
	err := s.db.QueryRow(ctx, query).Scan(&count)
	return count, err
}

// GetTotalRequestCount returns total number of logged requests
func (s *PostgresRequestLogStorage) GetTotalRequestCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM request_logs").Scan(&count)
	return count, err
}

// GetRequestCountByTenants counts requests for specific tenants
func (s *PostgresRequestLogStorage) GetRequestCountByTenants(ctx context.Context, tenantIDs []string) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM request_logs WHERE tenant_id = ANY($1)`
	err := s.db.QueryRow(ctx, query, tenantIDs).Scan(&count)
	return count, err
}

// GetRequestLogsByTenant retrieves recent logs for a specific tenant
func (s *PostgresRequestLogStorage) GetRequestLogsByTenant(ctx context.Context, tenantID string, limit int) ([]*models.RequestLog, error) {
	query := `
		SELECT tenant_id, method, request_type, status_code, latency_ms, created_at
		FROM request_logs
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := s.db.Query(ctx, query, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.RequestLog
	for rows.Next() {
		var log models.RequestLog
		if err := rows.Scan(&log.TenantID, &log.Method, &log.RequestType, &log.StatusCode, &log.LatencyMs, &log.Timestamp); err != nil {
			return nil, err
		}
		logs = append(logs, &log)
	}

	return logs, rows.Err()
}

// DeleteOldRequestLogs removes old request logs to manage storage size
func (s *PostgresRequestLogStorage) DeleteOldRequestLogs(ctx context.Context, olderThan string) error {
	query := fmt.Sprintf("DELETE FROM request_logs WHERE created_at < NOW() - INTERVAL '%s'", olderThan)
	_, err := s.db.Exec(ctx, query)
	return err
}
