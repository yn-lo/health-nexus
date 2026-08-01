package service

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/domain/config/repository"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
)

// ============================================================================
// Test helpers
// ============================================================================

var testAESKey = bytes.Repeat([]byte{0x01}, 32)

func stringPtr(v string) *string { return &v }
func int64Ptr(v int64) *int64    { return &v }

// assertAppErrCodeConflict asserts err is *AppError with given code and HTTP status.
func assertAppErrCodeWithHTTP(t *testing.T, err error, wantCode string, wantHTTP int) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望 error，实际 nil")
	}
	appErr, ok := err.(*apperrors.AppError)
	if !ok {
		t.Fatalf("期望 *AppError，实际 %T: %v", err, err)
	}
	if appErr.Code != wantCode {
		t.Errorf("期望 Code=%s，实际 %s", wantCode, appErr.Code)
	}
	if appErr.HTTP != wantHTTP {
		t.Errorf("期望 HTTP=%d，实际 %d", wantHTTP, appErr.HTTP)
	}
}

// assertAppErrCodeConflict is a shorthand for 409 Conflict assertions.
func assertAppErrCodeConflict(t *testing.T, err error, wantCode string) {
	t.Helper()
	assertAppErrCodeWithHTTP(t, err, wantCode, 409)
}

// assertAppErrCodeNotFound is a shorthand for 404 Not Found assertions.
func assertAppErrCodeNotFound(t *testing.T, err error, wantCode string) {
	t.Helper()
	assertAppErrCodeWithHTTP(t, err, wantCode, 404)
}

// ctxWithOperator returns a context with user_id and user_role set (simulating JWT middleware).
func ctxWithOperator() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, contextkeys.UserID, int64(1))
	ctx = context.WithValue(ctx, contextkeys.UserRole, "admin")
	return ctx
}

// newTestService creates a ConfigService with all mock repos, nil tx/redis, and test AES key.
func newTestService(
	aiRepo AIProviderPort,
	swRepo SensitiveWordPort,
	srRepo SafetyRulePort,
	ragRepo RAGConfigPort,
	ptRepo PromptTemplatePort,
	smRepo SafetyMessagePort,
	auditRepo AuditLogPort,
) *ConfigService {
	return NewConfigService(aiRepo, swRepo, srRepo, ragRepo, ptRepo, smRepo, auditRepo, nil, testAESKey, nil)
}

// newTestServiceWithLLM creates a ConfigService with LLMConfig for GetConfigStatus tests.
func newTestServiceWithLLM(aiRepo AIProviderPort, llmCfg config.LLMConfig) *ConfigService {
	return NewConfigServiceWithLLM(aiRepo, nil, nil, nil, nil, nil, nil, nil, testAESKey, nil, llmCfg)
}

// ============================================================================
// Mock: AIProviderPort
// ============================================================================

type mockAIProviderRepo struct {
	mu         sync.Mutex
	items      map[int64]*entity.AIProvider
	nextID     int64
	getErr     error
	createErr  error
	updateErr  error
	deleteErr  error
	listErr    error
	currentDim int
	hasVectors bool
	alignErr   error
}

func newMockAIProviderRepo() *mockAIProviderRepo {
	return &mockAIProviderRepo{
		items:  make(map[int64]*entity.AIProvider),
		nextID: 1,
	}
}

func (m *mockAIProviderRepo) List(ctx context.Context, providerType string, isActive *bool) ([]*entity.AIProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]*entity.AIProvider, 0, len(m.items))
	for _, p := range m.items {
		if providerType != "" && p.ProviderType != providerType {
			continue
		}
		if isActive != nil && p.IsActive != *isActive {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

func (m *mockAIProviderRepo) Get(ctx context.Context, id int64) (*entity.AIProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	p, ok := m.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return p, nil
}

func (m *mockAIProviderRepo) Create(ctx context.Context, p *entity.AIProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	p.ID = m.nextID
	m.nextID++
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	m.items[p.ID] = p
	return nil
}

func (m *mockAIProviderRepo) Update(ctx context.Context, p *entity.AIProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.items[p.ID]; !ok {
		return repository.ErrNotFound
	}
	p.UpdatedAt = time.Now()
	m.items[p.ID] = p
	return nil
}

func (m *mockAIProviderRepo) Delete(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.items[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.items, id)
	return nil
}

func (m *mockAIProviderRepo) CurrentEmbeddingDimension(ctx context.Context) (int, error) {
	return m.currentDim, nil
}

func (m *mockAIProviderRepo) HasVectorizedChunks(ctx context.Context) (bool, error) {
	return m.hasVectors, nil
}

func (m *mockAIProviderRepo) AlignEmbeddingDimension(ctx context.Context, dim int) error {
	return m.alignErr
}

// ============================================================================
// Mock: SensitiveWordPort
// ============================================================================

type mockSensitiveWordRepo struct {
	mu        sync.Mutex
	items     map[int64]*entity.SensitiveWord
	nextID    int64
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

func newMockSensitiveWordRepo() *mockSensitiveWordRepo {
	return &mockSensitiveWordRepo{
		items:  make(map[int64]*entity.SensitiveWord),
		nextID: 1,
	}
}

func (m *mockSensitiveWordRepo) List(ctx context.Context, category string, p pagination.Params) ([]*entity.SensitiveWord, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	result := make([]*entity.SensitiveWord, 0, len(m.items))
	for _, w := range m.items {
		if category != "" && w.Category != category {
			continue
		}
		result = append(result, w)
	}
	return result, int64(len(result)), nil
}

func (m *mockSensitiveWordRepo) Get(ctx context.Context, id int64) (*entity.SensitiveWord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	w, ok := m.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return w, nil
}

func (m *mockSensitiveWordRepo) Create(ctx context.Context, w *entity.SensitiveWord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	w.ID = m.nextID
	m.nextID++
	w.CreatedAt = time.Now()
	m.items[w.ID] = w
	return nil
}

func (m *mockSensitiveWordRepo) Update(ctx context.Context, w *entity.SensitiveWord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.items[w.ID]; !ok {
		return repository.ErrNotFound
	}
	m.items[w.ID] = w
	return nil
}

func (m *mockSensitiveWordRepo) Delete(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.items[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.items, id)
	return nil
}

// ============================================================================
// Mock: SafetyRulePort
// ============================================================================

type mockSafetyRuleRepo struct {
	mu        sync.Mutex
	items     map[int64]*entity.SafetyRule
	nextID    int64
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

func newMockSafetyRuleRepo() *mockSafetyRuleRepo {
	return &mockSafetyRuleRepo{
		items:  make(map[int64]*entity.SafetyRule),
		nextID: 1,
	}
}

func (m *mockSafetyRuleRepo) List(ctx context.Context, category string, p pagination.Params) ([]*entity.SafetyRule, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	result := make([]*entity.SafetyRule, 0, len(m.items))
	for _, r := range m.items {
		if category != "" && r.Category != category {
			continue
		}
		result = append(result, r)
	}
	return result, int64(len(result)), nil
}

func (m *mockSafetyRuleRepo) Get(ctx context.Context, id int64) (*entity.SafetyRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	r, ok := m.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return r, nil
}

func (m *mockSafetyRuleRepo) Create(ctx context.Context, r *entity.SafetyRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	r.ID = m.nextID
	m.nextID++
	r.CreatedAt = time.Now()
	r.UpdatedAt = time.Now()
	m.items[r.ID] = r
	return nil
}

func (m *mockSafetyRuleRepo) Update(ctx context.Context, r *entity.SafetyRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.items[r.ID]; !ok {
		return repository.ErrNotFound
	}
	r.UpdatedAt = time.Now()
	m.items[r.ID] = r
	return nil
}

func (m *mockSafetyRuleRepo) Delete(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.items[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.items, id)
	return nil
}

// ============================================================================
// Mock: RAGConfigPort
// ============================================================================

type mockRAGConfigRepo struct {
	mu        sync.Mutex
	config    *entity.RAGConfig
	getErr    error
	upsertErr error
}

func newMockRAGConfigRepo() *mockRAGConfigRepo {
	return &mockRAGConfigRepo{}
}

func (m *mockRAGConfigRepo) Get(ctx context.Context) (*entity.RAGConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.config == nil {
		return nil, repository.ErrNotFound
	}
	return m.config, nil
}

func (m *mockRAGConfigRepo) Upsert(ctx context.Context, c *entity.RAGConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upsertErr != nil {
		return m.upsertErr
	}
	c.UpdatedAt = time.Now()
	m.config = c
	return nil
}

// ============================================================================
// Mock: PromptTemplatePort
// ============================================================================

type mockPromptTemplateRepo struct {
	mu        sync.Mutex
	items     map[int64]*entity.PromptTemplate
	nextID    int64
	listErr   error
	createErr error
	deleteErr error
	// updateResult is returned by UpdateContentAndActive; if nil, ErrNotFound is returned.
	updateResult *entity.PromptTemplate
	updateErr    error
}

func newMockPromptTemplateRepo() *mockPromptTemplateRepo {
	return &mockPromptTemplateRepo{
		items:  make(map[int64]*entity.PromptTemplate),
		nextID: 1,
	}
}

func (m *mockPromptTemplateRepo) List(ctx context.Context, promptType string, isActive *bool, p pagination.Params) ([]*entity.PromptTemplate, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	result := make([]*entity.PromptTemplate, 0, len(m.items))
	for _, t := range m.items {
		if promptType != "" && t.Type != promptType {
			continue
		}
		if isActive != nil && t.IsActive != *isActive {
			continue
		}
		result = append(result, t)
	}
	return result, int64(len(result)), nil
}

func (m *mockPromptTemplateRepo) Create(ctx context.Context, t *entity.PromptTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	t.ID = m.nextID
	m.nextID++
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	m.items[t.ID] = t
	return nil
}

func (m *mockPromptTemplateRepo) UpdateContentAndActive(ctx context.Context, id int64, content *string, isActive *bool) (*entity.PromptTemplate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	if m.updateResult != nil {
		return m.updateResult, nil
	}
	t, ok := m.items[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if content != nil {
		t.Content = *content
	}
	if isActive != nil {
		t.IsActive = *isActive
	}
	t.UpdatedAt = time.Now()
	return t, nil
}

func (m *mockPromptTemplateRepo) Delete(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.items[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.items, id)
	return nil
}

// ============================================================================
// Mock: SafetyMessagePort
// ============================================================================

type mockSafetyMessageRepo struct {
	mu        sync.Mutex
	items     []*entity.SafetyMessage
	listErr   error
	upsertErr error
}

func newMockSafetyMessageRepo() *mockSafetyMessageRepo {
	return &mockSafetyMessageRepo{}
}

func (m *mockSafetyMessageRepo) ListAll(ctx context.Context) ([]*entity.SafetyMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.items, nil
}

func (m *mockSafetyMessageRepo) Upsert(ctx context.Context, msgType, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upsertErr != nil {
		return m.upsertErr
	}
	for i, msg := range m.items {
		if msg.Type == msgType {
			m.items[i].Content = content
			m.items[i].UpdatedAt = time.Now()
			return nil
		}
	}
	m.items = append(m.items, &entity.SafetyMessage{
		ID:        int64(len(m.items) + 1),
		Type:      msgType,
		Content:   content,
		UpdatedAt: time.Now(),
	})
	return nil
}

// ============================================================================
// Mock: AuditLogPort
// ============================================================================

type mockAuditLogRepo struct {
	mu      sync.Mutex
	logs    []*entity.ConfigAuditLog
	listErr error
}

func newMockAuditLogRepo() *mockAuditLogRepo {
	return &mockAuditLogRepo{}
}

func (m *mockAuditLogRepo) Create(ctx context.Context, l *entity.ConfigAuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l.ID = int64(len(m.logs) + 1)
	l.CreatedAt = time.Now()
	m.logs = append(m.logs, l)
	return nil
}

func (m *mockAuditLogRepo) ListByEntity(ctx context.Context, entityType string, entityID int64, page, pageSize int) ([]*entity.ConfigAuditLog, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	result := make([]*entity.ConfigAuditLog, 0, len(m.logs))
	for _, l := range m.logs {
		if entityType != "" && l.EntityType != entityType {
			continue
		}
		result = append(result, l)
	}
	return result, len(result), nil
}

// ============================================================================
// AIProvider Tests
// ============================================================================

func TestListAIProviders(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM, IsActive: true}
		repo.items[2] = &entity.AIProvider{ID: 2, Name: "bge", ProviderType: constants.ProviderTypeEmbedding, IsActive: true}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		result, err := svc.ListAIProviders(ctxWithOperator(), "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("expected 2 providers, got %d", len(result))
		}
	})

	t.Run("filter_by_provider_type", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM, IsActive: true}
		repo.items[2] = &entity.AIProvider{ID: 2, Name: "bge", ProviderType: constants.ProviderTypeEmbedding, IsActive: true}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		result, err := svc.ListAIProviders(ctxWithOperator(), constants.ProviderTypeLLM, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 provider, got %d", len(result))
		}
		if result[0].ProviderType != constants.ProviderTypeLLM {
			t.Errorf("expected provider_type=llm, got %s", result[0].ProviderType)
		}
	})

	t.Run("invalid_provider_type", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.ListAIProviders(ctxWithOperator(), "invalid_type", nil)
		assertAppErrCode(t, err, "CONFIG_INVALID_PROVIDER_TYPE")
	})

	t.Run("empty_result", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		result, err := svc.ListAIProviders(ctxWithOperator(), "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("expected 0 providers, got %d", len(result))
		}
	})
}

func TestGetAIProvider(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM,
			APIURL: "https://api.openai.com", APIKeyMasked: "sk-****abcd",
			ModelName: "gpt-4", IsActive: true,
		}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		resp, err := svc.GetAIProvider(ctxWithOperator(), 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID != 1 {
			t.Errorf("expected ID=1, got %d", resp.ID)
		}
		if resp.Name != "gpt4" {
			t.Errorf("expected Name=gpt4, got %s", resp.Name)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.GetAIProvider(ctxWithOperator(), 999)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// GetAIProvider returns raw repo error (no translateRepoErr)
		if err.Error() != repository.ErrNotFound.Error() {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestCreateAIProvider(t *testing.T) {
	t.Run("happy_path_llm", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		resp, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			Name:         "gpt4",
			APIBase:      "https://api.openai.com",
			APIKey:       "sk-1234567890abcdef",
			ModelName:    "gpt-4",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID == 0 {
			t.Error("expected non-zero ID after create")
		}
		if resp.Name != "gpt4" {
			t.Errorf("expected Name=gpt4, got %s", resp.Name)
		}
		if resp.ProviderType != constants.ProviderTypeLLM {
			t.Errorf("expected provider_type=llm, got %s", resp.ProviderType)
		}
		if !resp.IsActive {
			t.Error("expected IsActive=true (default)")
		}
		// API key should be masked in response
		if resp.APIKey == "sk-1234567890abcdef" {
			t.Error("API key should be masked in response")
		}
	})

	t.Run("happy_path_with_is_active_false", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		resp, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			Name:         "gpt4",
			APIBase:      "https://api.openai.com",
			APIKey:       "sk-1234567890abcdef",
			ModelName:    "gpt-4",
			IsActive:     boolPtr(false),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.IsActive {
			t.Error("expected IsActive=false")
		}
	})

	t.Run("missing_name", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			APIBase:      "https://api.openai.com",
			APIKey:       "sk-1234567890abcdef",
			ModelName:    "gpt-4",
		})
		assertAppErrCode(t, err, "CONFIG_NAME_REQUIRED")
	})

	t.Run("missing_api_base", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			Name:         "gpt4",
			APIKey:       "sk-1234567890abcdef",
			ModelName:    "gpt-4",
		})
		assertAppErrCode(t, err, "CONFIG_API_URL_REQUIRED")
	})

	t.Run("missing_api_key", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			Name:         "gpt4",
			APIBase:      "https://api.openai.com",
			ModelName:    "gpt-4",
		})
		assertAppErrCode(t, err, "CONFIG_API_KEY_REQUIRED")
	})

	t.Run("missing_model_name", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			Name:         "gpt4",
			APIBase:      "https://api.openai.com",
			APIKey:       "sk-1234567890abcdef",
		})
		assertAppErrCode(t, err, "CONFIG_MODEL_NAME_REQUIRED")
	})

	t.Run("invalid_provider_type", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: "invalid",
			Name:         "test",
			APIBase:      "https://api.example.com",
			APIKey:       "sk-test",
			ModelName:    "model",
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_PROVIDER_TYPE")
	})

	t.Run("embedding_without_dimension", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeEmbedding,
			Name:         "bge",
			APIBase:      "https://api.example.com",
			APIKey:       "sk-test",
			ModelName:    "bge-m3",
		})
		assertAppErrCode(t, err, "CONFIG_EMBEDDING_DIM_REQUIRED")
	})

	t.Run("embedding_with_zero_dimension", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeEmbedding,
			Name:         "bge",
			APIBase:      "https://api.example.com",
			APIKey:       "sk-test",
			ModelName:    "bge-m3",
			Dimension:    intPtr(0),
		})
		assertAppErrCode(t, err, "CONFIG_EMBEDDING_DIM_REQUIRED")
	})

	t.Run("embedding_with_dimension_alignment", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.currentDim = 0 // no existing embedding dimension
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		resp, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeEmbedding,
			Name:         "bge",
			APIBase:      "https://api.example.com",
			APIKey:       "sk-test",
			ModelName:    "bge-m3",
			Dimension:    intPtr(1024),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Dimension == nil || *resp.Dimension != 1024 {
			t.Errorf("expected dimension=1024, got %v", resp.Dimension)
		}
	})

	t.Run("embedding_dimension_same_as_current_noop", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.currentDim = 1024
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		resp, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeEmbedding,
			Name:         "bge",
			APIBase:      "https://api.example.com",
			APIKey:       "sk-test",
			ModelName:    "bge-m3",
			Dimension:    intPtr(1024),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Dimension == nil || *resp.Dimension != 1024 {
			t.Errorf("expected dimension=1024, got %v", resp.Dimension)
		}
	})

	t.Run("embedding_dimension_change_blocked_by_vectors", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.currentDim = 768
		repo.hasVectors = true
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeEmbedding,
			Name:         "bge",
			APIBase:      "https://api.example.com",
			APIKey:       "sk-test",
			ModelName:    "bge-m3",
			Dimension:    intPtr(1024),
		})
		assertAppErrCodeConflict(t, err, "CONFIG_EMBEDDING_DIM_CHANGE_BLOCKED")
	})

	t.Run("duplicate_name", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.createErr = &pgconn.PgError{Code: "23505"}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			Name:         "gpt4",
			APIBase:      "https://api.openai.com",
			APIKey:       "sk-test",
			ModelName:    "gpt-4",
		})
		assertAppErrCodeConflict(t, err, "CONFIG_AI_PROVIDER_DUPLICATE")
	})
}

func TestUpdateAIProvider(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM,
			APIURL: "https://api.openai.com", APIKeyEncrypted: []byte("enc"),
			APIKeyMasked: "sk-****abcd", ModelName: "gpt-4", IsActive: true,
		}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		newName := "gpt4-turbo"
		resp, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			Name: &newName,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Name != "gpt4-turbo" {
			t.Errorf("expected Name=gpt4-turbo, got %s", resp.Name)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.UpdateAIProvider(ctxWithOperator(), 999, UpdateAIProviderRequest{
			Name: stringPtr("test"),
		})
		assertAppErrCodeNotFound(t, err, "CONFIG_AI_PROVIDER_NOT_FOUND")
	})

	t.Run("masked_api_key_preserved", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM,
			APIURL: "https://api.openai.com", APIKeyEncrypted: []byte("old-encrypted"),
			APIKeyMasked: "sk-****abcd", ModelName: "gpt-4", IsActive: true,
		}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		// Pass the masked key back — should NOT re-encrypt
		maskedKey := "sk-****abcd"
		resp, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			APIKey: &maskedKey,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The masked key should remain unchanged
		if resp.APIKey != "sk-****abcd" {
			t.Errorf("masked API key should be preserved, got %s", resp.APIKey)
		}
		// The encrypted value should NOT have changed
		stored := repo.items[1]
		if string(stored.APIKeyEncrypted) != "old-encrypted" {
			t.Error("APIKeyEncrypted should not change when masked key is passed back")
		}
	})

	t.Run("new_api_key_encrypted", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM,
			APIURL: "https://api.openai.com", APIKeyEncrypted: []byte("old-encrypted"),
			APIKeyMasked: "sk-****abcd", ModelName: "gpt-4", IsActive: true,
		}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		newKey := "sk-newkey1234567890"
		resp, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			APIKey: &newKey,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The response should show the new masked key
		if resp.APIKey == "sk-****abcd" {
			t.Error("API key mask should have changed after update with new key")
		}
		// The encrypted value should have changed
		stored := repo.items[1]
		if string(stored.APIKeyEncrypted) == "old-encrypted" {
			t.Error("APIKeyEncrypted should have changed after update with new key")
		}
	})

	t.Run("dimension_change_with_vectorized_chunks_blocked", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		dim := 768
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "bge", ProviderType: constants.ProviderTypeEmbedding,
			APIURL: "https://api.example.com", APIKeyEncrypted: []byte("enc"),
			APIKeyMasked: "sk-****test", ModelName: "bge-m3", Dimension: &dim, IsActive: true,
		}
		repo.currentDim = 768
		repo.hasVectors = true
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			Dimension: intPtr(1024),
		})
		assertAppErrCodeConflict(t, err, "CONFIG_EMBEDDING_DIM_CHANGE_BLOCKED")
	})

	t.Run("dimension_change_no_vectors_allowed", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		dim := 768
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "bge", ProviderType: constants.ProviderTypeEmbedding,
			APIURL: "https://api.example.com", APIKeyEncrypted: []byte("enc"),
			APIKeyMasked: "sk-****test", ModelName: "bge-m3", Dimension: &dim, IsActive: true,
		}
		repo.currentDim = 768
		repo.hasVectors = false
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		resp, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			Dimension: intPtr(1024),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Dimension == nil || *resp.Dimension != 1024 {
			t.Errorf("expected dimension=1024, got %v", resp.Dimension)
		}
	})

	t.Run("dimension_same_no_change", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		dim := 1024
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "bge", ProviderType: constants.ProviderTypeEmbedding,
			APIURL: "https://api.example.com", APIKeyEncrypted: []byte("enc"),
			APIKeyMasked: "sk-****test", ModelName: "bge-m3", Dimension: &dim, IsActive: true,
		}
		repo.currentDim = 1024
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		resp, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			Dimension: intPtr(1024),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Dimension == nil || *resp.Dimension != 1024 {
			t.Errorf("expected dimension=1024, got %v", resp.Dimension)
		}
	})

	t.Run("embedding_dimension_zero_rejected", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		dim := 768
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "bge", ProviderType: constants.ProviderTypeEmbedding,
			APIURL: "https://api.example.com", APIKeyEncrypted: []byte("enc"),
			APIKeyMasked: "sk-****test", ModelName: "bge-m3", Dimension: &dim, IsActive: true,
		}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			Dimension: intPtr(0),
		})
		assertAppErrCode(t, err, "CONFIG_EMBEDDING_DIM_REQUIRED")
	})

	t.Run("duplicate_name_on_update", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{
			ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM,
			APIURL: "https://api.openai.com", APIKeyEncrypted: []byte("enc"),
			APIKeyMasked: "sk-****abcd", ModelName: "gpt-4", IsActive: true,
		}
		repo.updateErr = &pgconn.PgError{Code: "23505"}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		_, err := svc.UpdateAIProvider(ctxWithOperator(), 1, UpdateAIProviderRequest{
			Name: stringPtr("duplicate-name"),
		})
		assertAppErrCodeConflict(t, err, "CONFIG_AI_PROVIDER_DUPLICATE")
	})
}

func TestDeleteAIProvider(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{ID: 1, Name: "gpt4", ProviderType: constants.ProviderTypeLLM}
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		err := svc.DeleteAIProvider(ctxWithOperator(), 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := repo.items[1]; ok {
			t.Error("expected item to be deleted")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestService(repo, nil, nil, nil, nil, nil, nil)

		err := svc.DeleteAIProvider(ctxWithOperator(), 999)
		assertAppErrCodeNotFound(t, err, "CONFIG_AI_PROVIDER_NOT_FOUND")
	})
}

// ============================================================================
// SensitiveWord Tests
// ============================================================================

func TestCreateSensitiveWord(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		resp, err := svc.CreateSensitiveWord(ctxWithOperator(), CreateSensitiveWordRequest{
			Word:     "自杀",
			Category: constants.SensitiveCategorySuicide,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID == 0 {
			t.Error("expected non-zero ID")
		}
		if resp.Word != "自杀" {
			t.Errorf("expected Word=自杀, got %s", resp.Word)
		}
		if !resp.IsActive {
			t.Error("expected IsActive=true (default)")
		}
	})

	t.Run("happy_path_with_is_active_false", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		resp, err := svc.CreateSensitiveWord(ctxWithOperator(), CreateSensitiveWordRequest{
			Word:     "自杀",
			Category: constants.SensitiveCategorySuicide,
			IsActive: boolPtr(false),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.IsActive {
			t.Error("expected IsActive=false")
		}
	})

	t.Run("empty_word", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		_, err := svc.CreateSensitiveWord(ctxWithOperator(), CreateSensitiveWordRequest{
			Word:     "",
			Category: constants.SensitiveCategorySuicide,
		})
		assertAppErrCode(t, err, "CONFIG_WORD_REQUIRED")
	})

	t.Run("invalid_category", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		_, err := svc.CreateSensitiveWord(ctxWithOperator(), CreateSensitiveWordRequest{
			Word:     "test",
			Category: "invalid",
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_CATEGORY")
	})

	t.Run("duplicate_word", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		repo.createErr = &pgconn.PgError{Code: "23505"}
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		_, err := svc.CreateSensitiveWord(ctxWithOperator(), CreateSensitiveWordRequest{
			Word:     "自杀",
			Category: constants.SensitiveCategorySuicide,
		})
		assertAppErrCodeConflict(t, err, "CONFIG_SENSITIVE_WORD_DUPLICATE")
	})
}

func TestUpdateSensitiveWord(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		repo.items[1] = &entity.SensitiveWord{ID: 1, Word: "自杀", Category: constants.SensitiveCategorySuicide, IsActive: true}
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		resp, err := svc.UpdateSensitiveWord(ctxWithOperator(), 1, UpdateSensitiveWordRequest{
			Word: stringPtr("自伤"),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Word != "自伤" {
			t.Errorf("expected Word=自伤, got %s", resp.Word)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		_, err := svc.UpdateSensitiveWord(ctxWithOperator(), 999, UpdateSensitiveWordRequest{
			Word: stringPtr("test"),
		})
		assertAppErrCodeNotFound(t, err, "CONFIG_SENSITIVE_WORD_NOT_FOUND")
	})

	t.Run("invalid_category", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		repo.items[1] = &entity.SensitiveWord{ID: 1, Word: "自杀", Category: constants.SensitiveCategorySuicide, IsActive: true}
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		_, err := svc.UpdateSensitiveWord(ctxWithOperator(), 1, UpdateSensitiveWordRequest{
			Category: stringPtr("invalid"),
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_CATEGORY")
	})

	t.Run("duplicate_on_update", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		repo.items[1] = &entity.SensitiveWord{ID: 1, Word: "自杀", Category: constants.SensitiveCategorySuicide, IsActive: true}
		repo.updateErr = &pgconn.PgError{Code: "23505"}
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		_, err := svc.UpdateSensitiveWord(ctxWithOperator(), 1, UpdateSensitiveWordRequest{
			Word: stringPtr("duplicate"),
		})
		assertAppErrCodeConflict(t, err, "CONFIG_SENSITIVE_WORD_DUPLICATE")
	})
}

func TestDeleteSensitiveWord(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		repo.items[1] = &entity.SensitiveWord{ID: 1, Word: "自杀", Category: constants.SensitiveCategorySuicide}
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		err := svc.DeleteSensitiveWord(ctxWithOperator(), 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := repo.items[1]; ok {
			t.Error("expected item to be deleted")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		err := svc.DeleteSensitiveWord(ctxWithOperator(), 999)
		assertAppErrCodeNotFound(t, err, "CONFIG_SENSITIVE_WORD_NOT_FOUND")
	})
}

// ============================================================================
// SafetyRule Tests
// ============================================================================

func TestCreateSafetyRule(t *testing.T) {
	t.Run("happy_path_replace", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		resp, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:        "诊断替换",
			Category:    constants.SafetyCategoryDiagnosis,
			Pattern:     `诊断\s*为`,
			Action:      entity.SafetyActionReplace,
			Replacement: "建议进一步检查",
			Description: "替换诊断表述",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID == 0 {
			t.Error("expected non-zero ID")
		}
		if resp.Action != entity.SafetyActionReplace {
			t.Errorf("expected action=replace, got %s", resp.Action)
		}
		if !resp.IsActive {
			t.Error("expected IsActive=true (default)")
		}
	})

	t.Run("happy_path_block", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		resp, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "处方拦截",
			Category: constants.SafetyCategoryPrescription,
			Pattern:  `开.*处方`,
			Action:   entity.SafetyActionBlock,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Action != entity.SafetyActionBlock {
			t.Errorf("expected action=block, got %s", resp.Action)
		}
	})

	t.Run("invalid_pattern", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "bad pattern",
			Category: constants.SafetyCategoryDiagnosis,
			Pattern:  `[invalid regex`,
			Action:   entity.SafetyActionBlock,
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_PATTERN")
	})

	t.Run("replace_without_replacement", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "诊断替换",
			Category: constants.SafetyCategoryDiagnosis,
			Pattern:  `诊断\s*为`,
			Action:   entity.SafetyActionReplace,
		})
		assertAppErrCode(t, err, "CONFIG_REPLACEMENT_REQUIRED")
	})

	t.Run("invalid_action", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "test",
			Category: constants.SafetyCategoryDiagnosis,
			Pattern:  `test`,
			Action:   "invalid",
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_ACTION")
	})

	t.Run("empty_name", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "",
			Category: constants.SafetyCategoryDiagnosis,
			Pattern:  `test`,
			Action:   entity.SafetyActionBlock,
		})
		assertAppErrCode(t, err, "CONFIG_NAME_REQUIRED")
	})

	t.Run("empty_pattern", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "test",
			Category: constants.SafetyCategoryDiagnosis,
			Pattern:  "",
			Action:   entity.SafetyActionBlock,
		})
		assertAppErrCode(t, err, "CONFIG_PATTERN_REQUIRED")
	})

	t.Run("invalid_category", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "test",
			Category: "invalid",
			Pattern:  `test`,
			Action:   entity.SafetyActionBlock,
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_CATEGORY")
	})

	t.Run("duplicate_name", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		repo.createErr = &pgconn.PgError{Code: "23505"}
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.CreateSafetyRule(ctxWithOperator(), CreateSafetyRuleRequest{
			Name:     "duplicate",
			Category: constants.SafetyCategoryDiagnosis,
			Pattern:  `test`,
			Action:   entity.SafetyActionBlock,
		})
		assertAppErrCodeConflict(t, err, "CONFIG_SAFETY_RULE_DUPLICATE")
	})
}

func TestUpdateSafetyRule(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		repo.items[1] = &entity.SafetyRule{
			ID: 1, Name: "诊断替换", Category: constants.SafetyCategoryDiagnosis,
			Pattern: `诊断\s*为`, Action: entity.SafetyActionReplace,
			Replacement: "建议进一步检查", IsActive: true,
		}
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		resp, err := svc.UpdateSafetyRule(ctxWithOperator(), 1, UpdateSafetyRuleRequest{
			Name: stringPtr("诊断替换v2"),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Name != "诊断替换v2" {
			t.Errorf("expected Name=诊断替换v2, got %s", resp.Name)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.UpdateSafetyRule(ctxWithOperator(), 999, UpdateSafetyRuleRequest{
			Name: stringPtr("test"),
		})
		assertAppErrCodeNotFound(t, err, "CONFIG_SAFETY_RULE_NOT_FOUND")
	})

	t.Run("invalid_merged_category", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		repo.items[1] = &entity.SafetyRule{
			ID: 1, Name: "诊断替换", Category: constants.SafetyCategoryDiagnosis,
			Pattern: `诊断\s*为`, Action: entity.SafetyActionReplace,
			Replacement: "建议进一步检查", IsActive: true,
		}
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.UpdateSafetyRule(ctxWithOperator(), 1, UpdateSafetyRuleRequest{
			Category: stringPtr("invalid"),
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_CATEGORY")
	})

	t.Run("invalid_merged_pattern", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		repo.items[1] = &entity.SafetyRule{
			ID: 1, Name: "诊断替换", Category: constants.SafetyCategoryDiagnosis,
			Pattern: `诊断\s*为`, Action: entity.SafetyActionReplace,
			Replacement: "建议进一步检查", IsActive: true,
		}
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.UpdateSafetyRule(ctxWithOperator(), 1, UpdateSafetyRuleRequest{
			Pattern: stringPtr(`[invalid regex`),
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_PATTERN")
	})

	t.Run("change_action_to_replace_without_replacement", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		repo.items[1] = &entity.SafetyRule{
			ID: 1, Name: "拦截规则", Category: constants.SafetyCategoryDiagnosis,
			Pattern: `test`, Action: entity.SafetyActionBlock,
			Replacement: "", IsActive: true,
		}
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		// Change action from block to replace, but replacement is still empty
		_, err := svc.UpdateSafetyRule(ctxWithOperator(), 1, UpdateSafetyRuleRequest{
			Action: stringPtr(entity.SafetyActionReplace),
		})
		assertAppErrCode(t, err, "CONFIG_REPLACEMENT_REQUIRED")
	})

	t.Run("duplicate_on_update", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		repo.items[1] = &entity.SafetyRule{
			ID: 1, Name: "诊断替换", Category: constants.SafetyCategoryDiagnosis,
			Pattern: `诊断\s*为`, Action: entity.SafetyActionReplace,
			Replacement: "建议进一步检查", IsActive: true,
		}
		repo.updateErr = &pgconn.PgError{Code: "23505"}
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, err := svc.UpdateSafetyRule(ctxWithOperator(), 1, UpdateSafetyRuleRequest{
			Name: stringPtr("duplicate"),
		})
		assertAppErrCodeConflict(t, err, "CONFIG_SAFETY_RULE_DUPLICATE")
	})
}

func TestDeleteSafetyRule(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		repo.items[1] = &entity.SafetyRule{ID: 1, Name: "诊断替换", Category: constants.SafetyCategoryDiagnosis}
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		err := svc.DeleteSafetyRule(ctxWithOperator(), 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := repo.items[1]; ok {
			t.Error("expected item to be deleted")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		err := svc.DeleteSafetyRule(ctxWithOperator(), 999)
		assertAppErrCodeNotFound(t, err, "CONFIG_SAFETY_RULE_NOT_FOUND")
	})
}

// ============================================================================
// RAGConfig Tests
// ============================================================================

func TestGetRAGConfig(t *testing.T) {
	t.Run("happy_path_from_repo", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		repo.config = &entity.RAGConfig{
			ID:                  1,
			ChunkSize:           500,
			ChunkOverlap:        50,
			MaxChunks:           10,
			TopK:                5,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
			RerankThreshold:     0.5,
			DiversityFactor:     0.0,
			OODThreshold:        0.3,
			UpdatedAt:           time.Now(),
		}
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		resp, err := svc.GetRAGConfig(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ChunkSize != 500 {
			t.Errorf("expected ChunkSize=500, got %d", resp.ChunkSize)
		}
		if resp.TopK != 5 {
			t.Errorf("expected TopK=5, got %d", resp.TopK)
		}
	})

	t.Run("self_heal_on_not_found", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		// config is nil → repo.Get returns ErrNotFound → service self-heals
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		resp, err := svc.GetRAGConfig(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should return default values
		if resp.ChunkSize != entity.DefaultChunkSize {
			t.Errorf("expected default ChunkSize=%d, got %d", entity.DefaultChunkSize, resp.ChunkSize)
		}
		if resp.ChunkOverlap != entity.DefaultChunkOverlap {
			t.Errorf("expected default ChunkOverlap=%d, got %d", entity.DefaultChunkOverlap, resp.ChunkOverlap)
		}
	})

	t.Run("repo_unexpected_error", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		repo.getErr = fmt.Errorf("db connection lost")
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		_, err := svc.GetRAGConfig(ctxWithOperator())
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestUpdateRAGConfig(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		repo.config = &entity.RAGConfig{
			ID:                  1,
			ChunkSize:           500,
			ChunkOverlap:        50,
			MaxChunks:           10,
			TopK:                5,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
			RerankThreshold:     0.5,
			DiversityFactor:     0.0,
			OODThreshold:        0.3,
		}
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		resp, err := svc.UpdateRAGConfig(ctxWithOperator(), UpdateRAGConfigRequest{
			ChunkSize: intPtr(1000),
			TopK:      intPtr(10),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ChunkSize != 1000 {
			t.Errorf("expected ChunkSize=1000, got %d", resp.ChunkSize)
		}
		if resp.TopK != 10 {
			t.Errorf("expected TopK=10, got %d", resp.TopK)
		}
		// Unchanged fields
		if resp.ChunkOverlap != 50 {
			t.Errorf("expected ChunkOverlap=50 (unchanged), got %d", resp.ChunkOverlap)
		}
	})

	t.Run("validation_error_chunk_size_out_of_range", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		_, err := svc.UpdateRAGConfig(ctxWithOperator(), UpdateRAGConfigRequest{
			ChunkSize: intPtr(50), // below min 200
		})
		assertAppErrCode(t, err, "CONFIG_RAG_CHUNK_SIZE_RANGE")
	})

	t.Run("overlap_gte_size_rejected", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		repo.config = &entity.RAGConfig{
			ID:                  1,
			ChunkSize:           500,
			ChunkOverlap:        50,
			MaxChunks:           10,
			TopK:                5,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
			RerankThreshold:     0.5,
			DiversityFactor:     0.0,
			OODThreshold:        0.3,
		}
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		// Set overlap = 500 which equals chunk_size
		_, err := svc.UpdateRAGConfig(ctxWithOperator(), UpdateRAGConfigRequest{
			ChunkOverlap: intPtr(500),
		})
		assertAppErrCode(t, err, "CONFIG_RAG_OVERLAP_TOO_LARGE")
	})

	t.Run("overlap_greater_than_size_rejected", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		repo.config = &entity.RAGConfig{
			ID:                  1,
			ChunkSize:           200, // minimum chunk_size
			ChunkOverlap:        50,
			MaxChunks:           10,
			TopK:                5,
			SimilarityThreshold: 0.75,
			RerankEnabled:       false,
			RerankThreshold:     0.5,
			DiversityFactor:     0.0,
			OODThreshold:        0.3,
		}
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		// Set overlap=200 which is within range but equals chunk_size
		_, err := svc.UpdateRAGConfig(ctxWithOperator(), UpdateRAGConfigRequest{
			ChunkOverlap: intPtr(200),
		})
		assertAppErrCode(t, err, "CONFIG_RAG_OVERLAP_TOO_LARGE")
	})

	t.Run("self_heal_on_not_found_then_update", func(t *testing.T) {
		repo := newMockRAGConfigRepo()
		// config is nil → repo.Get returns ErrNotFound → service uses defaults
		svc := newTestService(nil, nil, nil, repo, nil, nil, nil)

		resp, err := svc.UpdateRAGConfig(ctxWithOperator(), UpdateRAGConfigRequest{
			TopK: intPtr(20),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.TopK != 20 {
			t.Errorf("expected TopK=20, got %d", resp.TopK)
		}
		// Other fields should be defaults
		if resp.ChunkSize != entity.DefaultChunkSize {
			t.Errorf("expected default ChunkSize=%d, got %d", entity.DefaultChunkSize, resp.ChunkSize)
		}
	})
}

// ============================================================================
// PromptTemplate Tests
// ============================================================================

func TestCreatePromptTemplate(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		resp, err := svc.CreatePromptTemplate(ctxWithOperator(), CreatePromptTemplateRequest{
			Type:        constants.PromptTypeSystem,
			Content:     "你是一个医疗助手",
			IsActive:    boolPtr(true),
			Description: "系统提示词",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID == 0 {
			t.Error("expected non-zero ID")
		}
		if resp.Type != constants.PromptTypeSystem {
			t.Errorf("expected type=system, got %s", resp.Type)
		}
		if resp.Content != "你是一个医疗助手" {
			t.Errorf("expected content match, got %s", resp.Content)
		}
	})

	t.Run("default_is_active_false", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		resp, err := svc.CreatePromptTemplate(ctxWithOperator(), CreatePromptTemplateRequest{
			Type:    constants.PromptTypeSystem,
			Content: "你是一个医疗助手",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.IsActive {
			t.Error("expected IsActive=false (default for prompt template)")
		}
	})

	t.Run("invalid_type", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		_, err := svc.CreatePromptTemplate(ctxWithOperator(), CreatePromptTemplateRequest{
			Type:    "invalid",
			Content: "test",
		})
		assertAppErrCode(t, err, "CONFIG_INVALID_PROMPT_TYPE")
	})

	t.Run("empty_content", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		_, err := svc.CreatePromptTemplate(ctxWithOperator(), CreatePromptTemplateRequest{
			Type:    constants.PromptTypeSystem,
			Content: "",
		})
		assertAppErrCode(t, err, "CONFIG_CONTENT_REQUIRED")
	})

	t.Run("version_conflict", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		repo.createErr = &pgconn.PgError{Code: "23505"}
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		_, err := svc.CreatePromptTemplate(ctxWithOperator(), CreatePromptTemplateRequest{
			Type:    constants.PromptTypeSystem,
			Content: "test",
		})
		assertAppErrCodeConflict(t, err, "CONFIG_PROMPT_VERSION_CONFLICT")
	})
}

func TestUpdatePromptTemplate(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		repo.items[1] = &entity.PromptTemplate{
			ID: 1, Type: constants.PromptTypeSystem, Version: 1,
			Content: "old content", IsActive: false,
		}
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		resp, err := svc.UpdatePromptTemplate(ctxWithOperator(), 1, UpdatePromptTemplateRequest{
			Content:  stringPtr("new content"),
			IsActive: boolPtr(true),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "new content" {
			t.Errorf("expected content=new content, got %s", resp.Content)
		}
	})

	t.Run("empty_update", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		_, err := svc.UpdatePromptTemplate(ctxWithOperator(), 1, UpdatePromptTemplateRequest{})
		assertAppErrCode(t, err, "CONFIG_EMPTY_UPDATE")
	})

	t.Run("empty_content", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		_, err := svc.UpdatePromptTemplate(ctxWithOperator(), 1, UpdatePromptTemplateRequest{
			Content: stringPtr(""),
		})
		assertAppErrCode(t, err, "CONFIG_CONTENT_REQUIRED")
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		_, err := svc.UpdatePromptTemplate(ctxWithOperator(), 999, UpdatePromptTemplateRequest{
			Content: stringPtr("new content"),
		})
		assertAppErrCodeNotFound(t, err, "CONFIG_PROMPT_NOT_FOUND")
	})
}

func TestDeletePromptTemplate(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		repo.items[1] = &entity.PromptTemplate{ID: 1, Type: constants.PromptTypeSystem, IsActive: false}
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		err := svc.DeletePromptTemplate(ctxWithOperator(), 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := repo.items[1]; ok {
			t.Error("expected item to be deleted")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		err := svc.DeletePromptTemplate(ctxWithOperator(), 999)
		assertAppErrCodeNotFound(t, err, "CONFIG_PROMPT_NOT_FOUND")
	})
}

// ============================================================================
// SafetyMessage Tests
// ============================================================================

func TestGetSafetyMessages(t *testing.T) {
	t.Run("happy_path_with_db_data", func(t *testing.T) {
		repo := newMockSafetyMessageRepo()
		repo.items = []*entity.SafetyMessage{
			{ID: 1, Type: entity.SafetyMessageTypeRejection, Content: "自定义拒绝话术", UpdatedAt: time.Now()},
			{ID: 2, Type: entity.SafetyMessageTypeEmergency, Content: "自定义紧急话术", UpdatedAt: time.Now()},
		}
		svc := newTestService(nil, nil, nil, nil, nil, repo, nil)

		resp, err := svc.GetSafetyMessages(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.RejectionMessage != "自定义拒绝话术" {
			t.Errorf("expected custom rejection message, got %s", resp.RejectionMessage)
		}
		if resp.EmergencyMessage != "自定义紧急话术" {
			t.Errorf("expected custom emergency message, got %s", resp.EmergencyMessage)
		}
		// Unset types should use defaults
		if resp.SafetyWarningMessage != DefaultSafetyMessages.SafetyWarningMessage {
			t.Errorf("expected default safety warning, got %s", resp.SafetyWarningMessage)
		}
	})

	t.Run("empty_db_uses_defaults", func(t *testing.T) {
		repo := newMockSafetyMessageRepo()
		svc := newTestService(nil, nil, nil, nil, nil, repo, nil)

		resp, err := svc.GetSafetyMessages(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.RejectionMessage != DefaultSafetyMessages.RejectionMessage {
			t.Errorf("expected default rejection message, got %s", resp.RejectionMessage)
		}
		if resp.SafetyWarningMessage != DefaultSafetyMessages.SafetyWarningMessage {
			t.Errorf("expected default safety warning, got %s", resp.SafetyWarningMessage)
		}
	})

	t.Run("repo_error", func(t *testing.T) {
		repo := newMockSafetyMessageRepo()
		repo.listErr = fmt.Errorf("db error")
		svc := newTestService(nil, nil, nil, nil, nil, repo, nil)

		_, err := svc.GetSafetyMessages(ctxWithOperator())
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

// ============================================================================
// AuditLog Tests
// ============================================================================

func TestListAuditLogs(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		auditRepo := newMockAuditLogRepo()
		auditRepo.logs = []*entity.ConfigAuditLog{
			{
				ID: 1, Action: entity.AuditActionCreate, EntityType: entity.AuditEntityAIProvider,
				EntityID: int64Ptr(1), OperatorID: 1, OperatorRole: "admin",
				CreatedAt: time.Now(),
			},
			{
				ID: 2, Action: entity.AuditActionUpdate, EntityType: entity.AuditEntityRAGConfig,
				EntityID: nil, OperatorID: 1, OperatorRole: "admin",
				CreatedAt: time.Now(),
			},
		}
		svc := newTestService(nil, nil, nil, nil, nil, nil, auditRepo)

		result, total, err := svc.ListAuditLogs(
			ctxWithOperator(), "", 0, pagination.Params{Page: 1, PageSize: 10},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 2 {
			t.Errorf("expected total=2, got %d", total)
		}
		if len(result) != 2 {
			t.Errorf("expected 2 items, got %d", len(result))
		}
	})

	t.Run("filter_by_entity_type", func(t *testing.T) {
		auditRepo := newMockAuditLogRepo()
		auditRepo.logs = []*entity.ConfigAuditLog{
			{ID: 1, Action: entity.AuditActionCreate, EntityType: entity.AuditEntityAIProvider, CreatedAt: time.Now()},
			{ID: 2, Action: entity.AuditActionUpdate, EntityType: entity.AuditEntityRAGConfig, CreatedAt: time.Now()},
		}
		svc := newTestService(nil, nil, nil, nil, nil, nil, auditRepo)

		result, total, err := svc.ListAuditLogs(
			ctxWithOperator(), entity.AuditEntityAIProvider, 0, pagination.Params{Page: 1, PageSize: 10},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 1 {
			t.Errorf("expected total=1, got %d", total)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 item, got %d", len(result))
		}
		if result[0].EntityType != entity.AuditEntityAIProvider {
			t.Errorf("expected entity_type=ai_provider, got %s", result[0].EntityType)
		}
	})

	t.Run("invalid_entity_type", func(t *testing.T) {
		auditRepo := newMockAuditLogRepo()
		svc := newTestService(nil, nil, nil, nil, nil, nil, auditRepo)

		_, _, err := svc.ListAuditLogs(
			ctxWithOperator(), "invalid_type", 0, pagination.Params{Page: 1, PageSize: 10},
		)
		assertAppErrCode(t, err, "CONFIG_INVALID_ENTITY_TYPE")
	})

	t.Run("empty_result", func(t *testing.T) {
		auditRepo := newMockAuditLogRepo()
		svc := newTestService(nil, nil, nil, nil, nil, nil, auditRepo)

		result, total, err := svc.ListAuditLogs(
			ctxWithOperator(), "", 0, pagination.Params{Page: 1, PageSize: 10},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 0 {
			t.Errorf("expected total=0, got %d", total)
		}
		if len(result) != 0 {
			t.Errorf("expected 0 items, got %d", len(result))
		}
	})
}

// ============================================================================
// Audit integration: verify audit logs are created on CRUD operations
// ============================================================================

func TestAuditLogCreatedOnCRUD(t *testing.T) {
	t.Run("create_ai_provider_writes_audit", func(t *testing.T) {
		aiRepo := newMockAIProviderRepo()
		auditRepo := newMockAuditLogRepo()
		svc := newTestService(aiRepo, nil, nil, nil, nil, nil, auditRepo)

		_, err := svc.CreateAIProvider(ctxWithOperator(), CreateAIProviderRequest{
			ProviderType: constants.ProviderTypeLLM,
			Name:         "gpt4",
			APIBase:      "https://api.openai.com",
			APIKey:       "sk-test1234567890",
			ModelName:    "gpt-4",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(auditRepo.logs) != 1 {
			t.Fatalf("expected 1 audit log, got %d", len(auditRepo.logs))
		}
		log := auditRepo.logs[0]
		if log.Action != entity.AuditActionCreate {
			t.Errorf("expected action=create, got %s", log.Action)
		}
		if log.EntityType != entity.AuditEntityAIProvider {
			t.Errorf("expected entity_type=ai_provider, got %s", log.EntityType)
		}
		if log.OperatorID != 1 {
			t.Errorf("expected operator_id=1, got %d", log.OperatorID)
		}
	})

	t.Run("delete_sensitive_word_writes_audit", func(t *testing.T) {
		swRepo := newMockSensitiveWordRepo()
		swRepo.items[1] = &entity.SensitiveWord{ID: 1, Word: "自杀", Category: constants.SensitiveCategorySuicide}
		auditRepo := newMockAuditLogRepo()
		svc := newTestService(nil, swRepo, nil, nil, nil, nil, auditRepo)

		err := svc.DeleteSensitiveWord(ctxWithOperator(), 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(auditRepo.logs) != 1 {
			t.Fatalf("expected 1 audit log, got %d", len(auditRepo.logs))
		}
		if auditRepo.logs[0].Action != entity.AuditActionDelete {
			t.Errorf("expected action=delete, got %s", auditRepo.logs[0].Action)
		}
		if auditRepo.logs[0].EntityType != entity.AuditEntitySensitiveWord {
			t.Errorf("expected entity_type=sensitive_word, got %s", auditRepo.logs[0].EntityType)
		}
	})

	t.Run("update_rag_config_writes_audit_with_nil_entity_id", func(t *testing.T) {
		ragRepo := newMockRAGConfigRepo()
		ragRepo.config = &entity.RAGConfig{
			ID: 1, ChunkSize: 500, ChunkOverlap: 50, MaxChunks: 10,
			TopK: 5, SimilarityThreshold: 0.75, RerankEnabled: false,
			RerankThreshold: 0.5, DiversityFactor: 0.0, OODThreshold: 0.3,
		}
		auditRepo := newMockAuditLogRepo()
		svc := newTestService(nil, nil, nil, ragRepo, nil, nil, auditRepo)

		_, err := svc.UpdateRAGConfig(ctxWithOperator(), UpdateRAGConfigRequest{
			TopK: intPtr(10),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(auditRepo.logs) != 1 {
			t.Fatalf("expected 1 audit log, got %d", len(auditRepo.logs))
		}
		log := auditRepo.logs[0]
		if log.EntityID != nil {
			t.Errorf("expected nil EntityID for singleton config, got %v", log.EntityID)
		}
		if log.EntityType != entity.AuditEntityRAGConfig {
			t.Errorf("expected entity_type=rag_config, got %s", log.EntityType)
		}
	})
}

// ============================================================================
// ListSensitiveWords / ListSafetyRules / ListPromptTemplates: filter validation
// ============================================================================

func TestListSensitiveWords(t *testing.T) {
	t.Run("invalid_category", func(t *testing.T) {
		repo := newMockSensitiveWordRepo()
		svc := newTestService(nil, repo, nil, nil, nil, nil, nil)

		_, _, err := svc.ListSensitiveWords(
			ctxWithOperator(), "invalid", pagination.Params{Page: 1, PageSize: 10},
		)
		assertAppErrCode(t, err, "CONFIG_INVALID_CATEGORY")
	})
}

func TestListSafetyRules(t *testing.T) {
	t.Run("invalid_category", func(t *testing.T) {
		repo := newMockSafetyRuleRepo()
		svc := newTestService(nil, nil, repo, nil, nil, nil, nil)

		_, _, err := svc.ListSafetyRules(
			ctxWithOperator(), "invalid", pagination.Params{Page: 1, PageSize: 10},
		)
		assertAppErrCode(t, err, "CONFIG_INVALID_CATEGORY")
	})
}

func TestListPromptTemplates(t *testing.T) {
	t.Run("invalid_type", func(t *testing.T) {
		repo := newMockPromptTemplateRepo()
		svc := newTestService(nil, nil, nil, nil, repo, nil, nil)

		_, _, err := svc.ListPromptTemplates(
			ctxWithOperator(), "invalid", nil, pagination.Params{Page: 1, PageSize: 10},
		)
		assertAppErrCode(t, err, "CONFIG_INVALID_PROMPT_TYPE")
	})
}

// ============================================================================
// GetConfigStatus Tests
// ============================================================================

func TestGetConfigStatus(t *testing.T) {
	t.Run("all_configured_from_db", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{ID: 1, ProviderType: constants.ProviderTypeLLM, IsActive: true}
		repo.items[2] = &entity.AIProvider{ID: 2, ProviderType: constants.ProviderTypeEmbedding, IsActive: true}
		repo.items[3] = &entity.AIProvider{ID: 3, ProviderType: constants.ProviderTypeRerank, IsActive: true}
		repo.items[4] = &entity.AIProvider{ID: 4, ProviderType: constants.ProviderTypeRewrite, IsActive: true}
		svc := newTestServiceWithLLM(repo, config.LLMConfig{})

		status, err := svc.GetConfigStatus(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !status.LLM.Configured {
			t.Error("expected LLM configured=true")
		}
		if !status.Embedding.Configured {
			t.Error("expected Embedding configured=true")
		}
		if !status.Rerank.Configured {
			t.Error("expected Rerank configured=true")
		}
		if !status.Rewrite.Configured {
			t.Error("expected Rewrite configured=true")
		}
	})

	t.Run("none_configured_no_db_no_yaml", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		svc := newTestServiceWithLLM(repo, config.LLMConfig{}) // 空 LLMConfig

		status, err := svc.GetConfigStatus(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status.LLM.Configured {
			t.Error("expected LLM configured=false")
		}
		if status.LLM.Message == "" {
			t.Error("expected LLM message to be non-empty when not configured")
		}
		if status.Embedding.Configured {
			t.Error("expected Embedding configured=false")
		}
		if status.Rerank.Configured {
			t.Error("expected Rerank configured=false")
		}
		if status.Rewrite.Configured {
			t.Error("expected Rewrite configured=false")
		}
	})

	t.Run("yaml_fallback_when_no_db_provider", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		// DB 中没有 active provider，但 config.yaml 有 api_key
		svc := newTestServiceWithLLM(repo, config.LLMConfig{
			APIKey:         "sk-yaml-key",
			EmbeddingModel: "text-embedding-3-small",
		})

		status, err := svc.GetConfigStatus(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !status.LLM.Configured {
			t.Error("expected LLM configured=true (from yaml fallback)")
		}
		// Embedding: yaml 中主 api_key 非空但 embedding 子配置无独立 api_key，
		// resolveProvider 会回退到主 api_key，所以也应该是 configured
		if !status.Embedding.Configured {
			t.Error("expected Embedding configured=true (from yaml fallback)")
		}
	})

	t.Run("db_overrides_yaml", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{ID: 1, ProviderType: constants.ProviderTypeLLM, IsActive: true}
		// yaml 也有 api_key，但 DB 有 active provider，应优先 DB
		svc := newTestServiceWithLLM(repo, config.LLMConfig{APIKey: "sk-yaml-key"})

		status, err := svc.GetConfigStatus(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !status.LLM.Configured {
			t.Error("expected LLM configured=true (from DB)")
		}
	})

	t.Run("partial_configured", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{ID: 1, ProviderType: constants.ProviderTypeLLM, IsActive: true}
		// 只有 LLM 在 DB 中，其他都没有
		svc := newTestServiceWithLLM(repo, config.LLMConfig{})

		status, err := svc.GetConfigStatus(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !status.LLM.Configured {
			t.Error("expected LLM configured=true")
		}
		if status.Embedding.Configured {
			t.Error("expected Embedding configured=false")
		}
		if status.Rerank.Configured {
			t.Error("expected Rerank configured=false")
		}
		if status.Rewrite.Configured {
			t.Error("expected Rewrite configured=false")
		}
	})

	t.Run("inactive_db_provider_not_counted", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.items[1] = &entity.AIProvider{ID: 1, ProviderType: constants.ProviderTypeLLM, IsActive: false}
		svc := newTestServiceWithLLM(repo, config.LLMConfig{})

		status, err := svc.GetConfigStatus(ctxWithOperator())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status.LLM.Configured {
			t.Error("expected LLM configured=false (inactive provider should not count)")
		}
	})

	t.Run("repo_error", func(t *testing.T) {
		repo := newMockAIProviderRepo()
		repo.listErr = fmt.Errorf("db connection lost")
		svc := newTestServiceWithLLM(repo, config.LLMConfig{})

		_, err := svc.GetConfigStatus(ctxWithOperator())
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}
