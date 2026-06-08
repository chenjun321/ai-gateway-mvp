package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
}

type APIKey struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`
	KeyHash   string     `json:"-"`
	KeyPrefix string     `json:"key_prefix"`
	Scopes    []string   `json:"scopes"`
	Enabled   bool       `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type UsageRecord struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	APIKeyID         string    `json:"api_key_id"`
	APIKeyName       string    `json:"api_key_name,omitempty"`
	Model            string    `json:"model"`
	ThreadID         string    `json:"thread_id,omitempty"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CreatedAt        time.Time `json:"created_at"`
}

type UsageFilter struct {
	TenantID string
	APIKeyID string
	Model    string
	ThreadID string
	From     *time.Time
	To       *time.Time
	Limit    int
}

type UsageSummary struct {
	TenantID         string `json:"tenant_id"`
	APIKeyID         string `json:"api_key_id"`
	Model            string `json:"model"`
	RequestCount     int    `json:"request_count"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`create table if not exists tenants (
			id text primary key,
			name text not null,
			scopes text not null,
			created_at text not null
		);`,
		`create table if not exists api_keys (
			id text primary key,
			tenant_id text not null,
			name text not null,
			key_hash text not null unique,
			key_prefix text not null,
			scopes text not null,
			enabled integer not null,
			expires_at text,
			created_at text not null,
			foreign key (tenant_id) references tenants(id)
		);`,
		`create index if not exists idx_api_keys_tenant_id on api_keys(tenant_id);`,
		`create table if not exists usage_records (
			id text primary key,
			tenant_id text not null,
			api_key_id text not null,
			model text not null,
			thread_id text not null default '',
			prompt_tokens integer not null,
			completion_tokens integer not null,
			total_tokens integer not null,
			created_at text not null,
			foreign key (tenant_id) references tenants(id),
			foreign key (api_key_id) references api_keys(id)
		);`,
		`create index if not exists idx_usage_tenant_key_model_created on usage_records(tenant_id, api_key_id, model, created_at);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateTenant(ctx context.Context, tenant Tenant) (*Tenant, error) {
	scopes, err := encodeScopes(tenant.Scopes)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `insert into tenants(id, name, scopes, created_at) values(?, ?, ?, ?)`,
		tenant.ID, tenant.Name, scopes, formatTime(tenant.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &tenant, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, scopes, created_at from tenants order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tenants []Tenant
	for rows.Next() {
		tenant, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}
	return tenants, rows.Err()
}

func (s *Store) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx, `select id, name, scopes, created_at from tenants where id = ?`, id)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &tenant, nil
}

func (s *Store) CreateAPIKey(ctx context.Context, key APIKey) (*APIKey, error) {
	scopes, err := encodeScopes(key.Scopes)
	if err != nil {
		return nil, err
	}
	var expiresAt any
	if key.ExpiresAt != nil {
		expiresAt = formatTime(*key.ExpiresAt)
	}
	enabled := 0
	if key.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `insert into api_keys(id, tenant_id, name, key_hash, key_prefix, scopes, enabled, expires_at, created_at)
		values(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.TenantID, key.Name, key.KeyHash, key.KeyPrefix, scopes, enabled, expiresAt, formatTime(key.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, tenantID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `select id, tenant_id, name, key_hash, key_prefix, scopes, enabled, expires_at, created_at
		from api_keys where tenant_id = ? order by created_at desc`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `select id, tenant_id, name, key_hash, key_prefix, scopes, enabled, expires_at, created_at
		from api_keys where key_hash = ?`, hash)
	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *Store) GetAPIKey(ctx context.Context, tenantID, keyID string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `select id, tenant_id, name, key_hash, key_prefix, scopes, enabled, expires_at, created_at
		from api_keys where tenant_id = ? and id = ?`, tenantID, keyID)
	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *Store) UpdateAPIKey(ctx context.Context, key APIKey) (*APIKey, error) {
	scopes, err := encodeScopes(key.Scopes)
	if err != nil {
		return nil, err
	}
	var expiresAt any
	if key.ExpiresAt != nil {
		expiresAt = formatTime(*key.ExpiresAt)
	}
	enabled := 0
	if key.Enabled {
		enabled = 1
	}
	res, err := s.db.ExecContext(ctx, `update api_keys set name = ?, scopes = ?, enabled = ?, expires_at = ? where tenant_id = ? and id = ?`,
		key.Name, scopes, enabled, expiresAt, key.TenantID, key.ID)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return s.GetAPIKey(ctx, key.TenantID, key.ID)
}

func (s *Store) InsertUsage(ctx context.Context, usage UsageRecord) (*UsageRecord, error) {
	_, err := s.db.ExecContext(ctx, `insert into usage_records(id, tenant_id, api_key_id, model, thread_id, prompt_tokens, completion_tokens, total_tokens, created_at)
		values(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		usage.ID, usage.TenantID, usage.APIKeyID, usage.Model, usage.ThreadID, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, formatTime(usage.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

func (s *Store) QueryUsage(ctx context.Context, filter UsageFilter) ([]UsageRecord, error) {
	query := `select u.id, u.tenant_id, u.api_key_id, k.name, u.model, u.thread_id, u.prompt_tokens, u.completion_tokens, u.total_tokens, u.created_at
		from usage_records u left join api_keys k on k.id = u.api_key_id where 1=1`
	args := []any{}
	query, args = appendUsageFilters(query, args, filter)
	query += ` order by u.created_at desc`
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += ` limit ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []UsageRecord
	for rows.Next() {
		var r UsageRecord
		var createdAt string
		if err := rows.Scan(&r.ID, &r.TenantID, &r.APIKeyID, &r.APIKeyName, &r.Model, &r.ThreadID, &r.PromptTokens, &r.CompletionTokens, &r.TotalTokens, &createdAt); err != nil {
			return nil, err
		}
		t, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = t
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *Store) SummarizeUsage(ctx context.Context, filter UsageFilter) ([]UsageSummary, error) {
	query := `select tenant_id, api_key_id, model, count(*), coalesce(sum(prompt_tokens), 0), coalesce(sum(completion_tokens), 0), coalesce(sum(total_tokens), 0)
		from usage_records u where 1=1`
	args := []any{}
	query, args = appendUsageFilters(query, args, filter)
	query += ` group by tenant_id, api_key_id, model order by tenant_id, api_key_id, model`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var summaries []UsageSummary
	for rows.Next() {
		var s UsageSummary
		if err := rows.Scan(&s.TenantID, &s.APIKeyID, &s.Model, &s.RequestCount, &s.PromptTokens, &s.CompletionTokens, &s.TotalTokens); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTenant(row scanner) (Tenant, error) {
	var tenant Tenant
	var scopes string
	var createdAt string
	if err := row.Scan(&tenant.ID, &tenant.Name, &scopes, &createdAt); err != nil {
		return tenant, err
	}
	decoded, err := decodeScopes(scopes)
	if err != nil {
		return tenant, err
	}
	tenant.Scopes = decoded
	tenant.CreatedAt, err = parseTime(createdAt)
	return tenant, err
}

func scanAPIKey(row scanner) (APIKey, error) {
	var key APIKey
	var scopes string
	var enabled int
	var expiresAt sql.NullString
	var createdAt string
	if err := row.Scan(&key.ID, &key.TenantID, &key.Name, &key.KeyHash, &key.KeyPrefix, &scopes, &enabled, &expiresAt, &createdAt); err != nil {
		return key, err
	}
	decoded, err := decodeScopes(scopes)
	if err != nil {
		return key, err
	}
	key.Scopes = decoded
	key.Enabled = enabled == 1
	if expiresAt.Valid {
		t, err := parseTime(expiresAt.String)
		if err != nil {
			return key, err
		}
		key.ExpiresAt = &t
	}
	key.CreatedAt, err = parseTime(createdAt)
	return key, err
}

func appendUsageFilters(query string, args []any, filter UsageFilter) (string, []any) {
	if filter.TenantID != "" {
		query += ` and u.tenant_id = ?`
		args = append(args, filter.TenantID)
	}
	if filter.APIKeyID != "" {
		query += ` and u.api_key_id = ?`
		args = append(args, filter.APIKeyID)
	}
	if filter.Model != "" {
		query += ` and u.model = ?`
		args = append(args, filter.Model)
	}
	if filter.ThreadID != "" {
		query += ` and u.thread_id = ?`
		args = append(args, filter.ThreadID)
	}
	if filter.From != nil {
		query += ` and u.created_at >= ?`
		args = append(args, formatTime(*filter.From))
	}
	if filter.To != nil {
		query += ` and u.created_at <= ?`
		args = append(args, formatTime(*filter.To))
	}
	return query, args
}

func encodeScopes(scopes []string) (string, error) {
	b, err := json.Marshal(scopes)
	if err != nil {
		return "", fmt.Errorf("encode scopes: %w", err)
	}
	return string(b), nil
}

func decodeScopes(raw string) ([]string, error) {
	var scopes []string
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return nil, fmt.Errorf("decode scopes: %w", err)
	}
	return scopes, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, raw)
}
