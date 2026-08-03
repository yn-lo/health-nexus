// Package service 实现 wiki 域的业务编排：文章生命周期、审核状态机、跨科室引用授权、检索骨架。
// Service 层是唯一事务边界（data-flow.md §5），通过 postgres.TxManager.WithTx 开启；
// Repository 通过 ctx 内的事务句柄参与同一事务。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/middleware"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contenthash"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
)

// summaryMaxRunes 文章摘要自动截取的字符数上限（REQ-WIKI-007）。
const summaryMaxRunes = 100

// titleMaxRunes 文章标题字符数上限，对齐 articles.title VARCHAR(255)。
// 前置校验避免超长标题落库触发 DB 错误（500），统一返回 422。
const titleMaxRunes = 255

// Actor 操作者上下文（由 handler 从 JWT ctx 提取后传入）。
type Actor struct {
	UserID int64
	Role   string
	DeptID int64
}

// ActorFromDataScope 从 ctx 中的 DataScope（DataIsolation 中间件注入）构造 Actor。
// 返回 ok=false 表示 ctx 未挂载 DataScope（如未走 DataIsolation 中间件的路由）。
// 保留现有 Actor 参数兼容：handler 可继续显式传 Actor，本 helper 仅供 service 内部需要时使用（REQ-SEC-003）。
func ActorFromDataScope(ctx context.Context) (Actor, bool) {
	scope, ok := ctx.Value(contextkeys.DataScopeKey).(*middleware.DataScope)
	if !ok || scope == nil {
		return Actor{}, false
	}
	return Actor{UserID: scope.UserID, Role: scope.Role, DeptID: scope.DeptID}, true
}

// VectorizeEnqueuer 文章向量化任务入队（消费者定义，ISP）。
// 实现由 adapter.AsynqVectorizeEnqueuer 提供，将 int64 articleID 序列化为 asynq payload。
// ponytail: asynq 包仅暴露任务类型常量，入队由 adapter 实现；wiki 域不依赖 asynq 包，保持领域纯净，简化
type VectorizeEnqueuer interface {
	Enqueue(ctx context.Context, articleID int64) error
}

// ArticleRepoPort 文章持久化能力（消费者定义，ISP）。由 repository.ArticleRepo 实现。
type ArticleRepoPort interface {
	Create(ctx context.Context, a *entity.Article) error
	GetByID(ctx context.Context, id int64) (*entity.Article, error)
	GetPublishedByID(ctx context.Context, id int64) (*entity.Article, error)
	ListPublished(
		ctx context.Context, f repository.ListPublishedFilter, limit, offset int,
	) ([]*entity.Article, int64, error)
	ListForStaff(ctx context.Context, f repository.ListStaffFilter, limit, offset int) ([]*entity.Article, int64, error)
	UpdateFields(ctx context.Context, id int64, fields repository.UpdateFields) (*entity.Article, error)
	UpdateStatus(ctx context.Context, id int64, fromStatus, toStatus string, opts repository.StatusUpdateOpts) error
	ListFeatured(ctx context.Context, departmentID *int64, limit int) ([]*entity.Article, error)
	SetFeaturedRank(ctx context.Context, id int64, rank int) error
	SoftDelete(ctx context.Context, id int64) error
}

// AuditRepoPort 审计日志持久化能力。
type AuditRepoPort interface {
	Create(ctx context.Context, log *entity.ArticleAuditLog) error
}

// ChunkRepoPort 文章切片持久化能力（消费者定义，ISP）。由 repository.ChunkRepo 实现。
// 失效旧切片用于已发布文章内容变更后的版本切换（REQ-WIKI-016）；
// 列出生效切片用于医护端诊断 RAG 切片状态（契约 §4.12）。
type ChunkRepoPort interface {
	DeactivateByArticle(ctx context.Context, articleID int64) (int64, error)
	ListActiveByArticle(ctx context.Context, articleID int64) ([]*entity.ArticleChunk, error)
}

// OutboxRepoPort 向量化任务 outbox 持久化能力（消费者定义，ISP）。
// 事务内写入 outbox 记录，由 relay 进程扫描投递到 asynq，
// 保证文章发布/更新/恢复后向量化任务最终一致投递。
type OutboxRepoPort interface {
	Insert(ctx context.Context, articleID int64) error
}

// ReferenceRevoker 文章变动时撤销相关引用（消费者定义，ISP）。
// 由引用域 ReferenceService 实现，用于文章删除/归档时自动撤销已通过的引用。
type ReferenceRevoker interface {
	RevokeByArticle(ctx context.Context, articleID int64) error
}

// TxRunner 事务执行能力（消费者定义，ISP）。由 postgres.TxManager 实现；
// 抽成接口仅为单测可注入伪实现（直接同步执行 fn，不开真实事务）。
type TxRunner interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// ArticleService 文章生命周期服务：创建/查询/更新/审核状态机/软删除。
// 状态机：draft → pending → published → archived → deleted（REQ-WIKI-001）。
type ArticleService struct {
	repo       ArticleRepoPort
	audit      AuditRepoPort
	chunks     ChunkRepoPort
	tx         TxRunner
	vector     VectorizeEnqueuer
	outbox     OutboxRepoPort   // nil = 不写 outbox（旧版本兼容）
	refRevoker ReferenceRevoker // nil = 不撤销引用（旧版本兼容）
}

// NewArticleService 构造文章服务。
// vector 可为 nil（阶段 1 不入队，仅日志告警）。
// chunks 可为 nil（旧版本调用兼容，但已发布文章内容变更将无法失效旧切片，REQ-WIKI-016 会降级）。
// outbox 可为 nil（不写 outbox，仅事务外直接入队）。
// refRevoker 可为 nil（不撤销引用，文章删除/归档时不自动撤销关联引用）。
func NewArticleService(
	repo ArticleRepoPort, audit AuditRepoPort, chunks ChunkRepoPort, tx TxRunner, vector VectorizeEnqueuer,
	outbox OutboxRepoPort, refRevoker ReferenceRevoker,
) *ArticleService {
	return &ArticleService{
		repo: repo, audit: audit, chunks: chunks, tx: tx,
		vector: vector, outbox: outbox, refRevoker: refRevoker,
	}
}

// ============ DTO ============

// ArticleListItemDTO 已发布文章列表项（契约 §4.1）。
type ArticleListItemDTO struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	Summary        string     `json:"summary"`
	CoverURL       string     `json:"cover_url"`
	DepartmentID   *int64     `json:"department_id"`
	DepartmentName string     `json:"department_name"`
	ViewCount      int64      `json:"view_count"`
	Version        int        `json:"version"`
	AllowReference bool       `json:"allow_reference"`
	FeaturedRank   int        `json:"featured_rank"`
	PublishedAt    *time.Time `json:"published_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ArticleDetailDTO 已发布文章详情（契约 §4.2）。
type ArticleDetailDTO struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	Summary        string     `json:"summary"`
	CoverURL       string     `json:"cover_url"`
	DepartmentID   *int64     `json:"department_id"`
	DepartmentName string     `json:"department_name"`
	ViewCount      int64      `json:"view_count"`
	Version        int        `json:"version"`
	AllowReference bool       `json:"allow_reference"`
	AuthorID       int64      `json:"author_id"`
	AuthorName     string     `json:"author_name"`
	PublishedAt    *time.Time `json:"published_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ArticleStaffDTO 医护端文章视图（含所有状态）。
type ArticleStaffDTO struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	Summary        string     `json:"summary"`
	CoverURL       string     `json:"cover_url"`
	Status         string     `json:"status"`
	Version        int        `json:"version"`
	DepartmentID   *int64     `json:"department_id"`
	DepartmentName string     `json:"department_name"`
	AuthorID       int64      `json:"author_id"`
	AuthorName     string     `json:"author_name"`
	ReviewerID     *int64     `json:"reviewer_id"`
	ReviewComment  string     `json:"review_comment"`
	ViewCount      int64      `json:"view_count"`
	AllowReference bool       `json:"allow_reference"`
	FeaturedRank   int        `json:"featured_rank"`
	PublishedAt    *time.Time `json:"published_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ============ Create ============

// CreateInput 创建文章输入。
type CreateInput struct {
	Title          string
	Content        string
	Summary        string // 空则自动截取前 100 字（REQ-WIKI-007）
	CoverImageURL  string
	DepartmentID   int64
	AllowReference bool
	Actor          Actor
}

// Create 创建草稿文章（REQ-WIKI-003）。事务内：插入文章 + 审计日志。
func (s *ArticleService) Create(ctx context.Context, in CreateInput) (*ArticleStaffDTO, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, apperrors.Validation("WIKI_TITLE_REQUIRED", "title 不能为空")
	}
	if utf8.RuneCountInString(in.Title) > titleMaxRunes {
		return nil, apperrors.Validation("WIKI_TITLE_TOO_LONG", "title 长度不能超过 255 字符")
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, apperrors.Validation("WIKI_CONTENT_REQUIRED", "content 不能为空")
	}
	if in.DepartmentID <= 0 {
		return nil, apperrors.Validation("WIKI_DEPT_REQUIRED", "department_id 不能为空")
	}
	// 数据隔离：非超管只能在本科室创建（REQ-SEC-001）
	if in.Actor.Role != constants.RoleSuperAdmin && in.DepartmentID != in.Actor.DeptID {
		return nil, apperrors.Forbidden("WIKI_DEPT_FORBIDDEN", "只能在本科室创建文章")
	}

	summary := in.Summary
	if summary == "" {
		summary = truncateRunes(stripHTMLTags(in.Content), summaryMaxRunes)
	}
	deptID := in.DepartmentID
	a := &entity.Article{
		Title:          in.Title,
		Content:        in.Content,
		Summary:        summary,
		CoverImageURL:  in.CoverImageURL,
		Status:         constants.ArticleStatusDraft,
		Version:        1,
		ContentHash:    contenthash.SHA256(in.Content),
		AuthorID:       in.Actor.UserID,
		DepartmentID:   &deptID,
		AllowReference: in.AllowReference,
	}

	var created *entity.Article
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, a); err != nil {
			return fmt.Errorf("create article: %w", err)
		}
		if err := s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  a.ID,
			OperatorID: in.Actor.UserID,
			Action:     entity.AuditActionCreate,
			FromStatus: "",
			ToStatus:   constants.ArticleStatusDraft,
			Summary:    auditSummary(a),
		}); err != nil {
			return fmt.Errorf("audit create: %w", err)
		}
		created = a
		return nil
	})
	if err != nil {
		return nil, err
	}
	dto := toStaffDTO(created)
	return &dto, nil
}

// ============ Public Read ============

// ListPublished 已发布文章列表（匿名可访问，契约 §4.1）。departmentID 为 nil 表示不限定。
// allowReference 为 nil 表示不限定；true 表示仅公开文章。
// excludeDeptID 为 nil 表示不排除；非 nil 表示排除指定科室的文章。
// search 非空时按 title/summary 模糊匹配（前端搜索）。
func (s *ArticleService) ListPublished(
	ctx context.Context, departmentID *int64, allowReference *bool, excludeDeptID *int64,
	search string, limit, offset int,
) ([]ArticleListItemDTO, int64, error) {
	list, total, err := s.repo.ListPublished(
		ctx, repository.ListPublishedFilter{
			DepartmentID:   departmentID,
			AllowReference: allowReference,
			ExcludeDeptID:  excludeDeptID,
			Search:         search,
		}, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list published: %w", err)
	}
	dtos := make([]ArticleListItemDTO, 0, len(list))
	for _, a := range list {
		dtos = append(dtos, toListItemDTO(a))
	}
	return dtos, total, nil
}

// GetPublished 已发布文章详情（匿名可访问，契约 §4.2）。
// 副作用：每次访问阅读量 +1（契约 §4.2 仅规定 +1，未定义去重，故不去重）。
func (s *ArticleService) GetPublished(ctx context.Context, id int64) (*ArticleDetailDTO, error) {
	a, err := s.repo.GetPublishedByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, apperrors.NotFound("WIKI_ARTICLE_NOT_FOUND", "文章不存在或未发布")
		}
		return nil, fmt.Errorf("get published article: %w", err)
	}
	dto := toDetailDTO(a)
	return &dto, nil
}

func (s *ArticleService) ListFeatured(
	ctx context.Context, departmentID *int64, limit int,
) ([]ArticleListItemDTO, error) {
	list, err := s.repo.ListFeatured(ctx, departmentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list featured: %w", err)
	}
	dtos := make([]ArticleListItemDTO, 0, len(list))
	for _, a := range list {
		dtos = append(dtos, toListItemDTO(a))
	}
	return dtos, nil
}

func (s *ArticleService) SetFeaturedRank(ctx context.Context, articleID int64, rank int, actor Actor) error {
	if rank < 0 || rank > 3 {
		return apperrors.Validation("WIKI_INVALID_FEATURED_RANK", "featured_rank 必须在 0 到 3 之间")
	}
	return s.tx.WithTx(ctx, func(ctx context.Context) error {
		a, err := s.repo.GetByID(ctx, articleID)
		if err != nil {
			return translateArticleErr(err)
		}
		if err := assertCanFeature(a, actor); err != nil {
			return err
		}
		if a.Status != constants.ArticleStatusPublished {
			return apperrors.Conflict("WIKI_FEATURED_NOT_PUBLISHED", "仅已发布文章可设为热门")
		}
		if err := s.repo.SetFeaturedRank(ctx, articleID, rank); err != nil {
			return translateArticleErr(err)
		}
		return s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  articleID,
			OperatorID: actor.UserID,
			Action:     entity.AuditActionFeature,
			FromStatus: a.Status,
			ToStatus:   a.Status,
			Summary:    fmt.Sprintf("热门位：%d", rank),
		})
	})
}

// ============ Staff Read ============

// ListMineInput 我的文章列表查询（契约 §4.4）。
type ListMineInput struct {
	Status       string // 空 = 全部状态
	DepartmentID *int64 // 超管可指定；非超管强制为本科室
	Actor        Actor
}

// ListMine 医护端文章列表。数据隔离：非超管仅本科室；超管可指定 department_id 或全部（REQ-SEC-001）。
func (s *ArticleService) ListMine(
	ctx context.Context, in ListMineInput, limit, offset int,
) ([]ArticleStaffDTO, int64, error) {
	if in.Status != "" && !isValidArticleStatus(in.Status) {
		// Medium 2: 用独立错误码区分"查询参数校验"(422)与"状态机迁移冲突"(409)，
		// 避免 WIKI_INVALID_STATUS 同时承载 409/422 两个 HTTP 状态码语义冲突。
		return nil, 0, apperrors.Validation("WIKI_INVALID_STATUS_PARAM", "status 参数无效")
	}
	filter := repository.ListStaffFilter{Status: in.Status}
	if in.Actor.Role == constants.RoleSuperAdmin {
		filter.DepartmentID = in.DepartmentID // nil = 全部
	} else {
		deptID := in.Actor.DeptID
		filter.DepartmentID = &deptID
	}
	list, total, err := s.repo.ListForStaff(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list mine: %w", err)
	}
	dtos := make([]ArticleStaffDTO, 0, len(list))
	for _, a := range list {
		dtos = append(dtos, toStaffDTO(a))
	}
	return dtos, total, nil
}

// GetMine 医护端按 ID 获取单篇文章（编辑回填用）。鉴权：仅作者或超管可读。
func (s *ArticleService) GetMine(ctx context.Context, articleID int64, actor Actor) (*ArticleStaffDTO, error) {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return nil, translateArticleErr(err)
	}
	if err := assertCanAuthorOrAdmin(article, actor); err != nil {
		return nil, err
	}
	dto := toStaffDTO(article)
	return &dto, nil
}

// ArticleChunkDTO 文章切片视图（契约 §4.12）。隐藏 embedding 等内部字段。
type ArticleChunkDTO struct {
	ID          int64     `json:"id"`
	ChunkIndex  int       `json:"chunk_index"`
	Content     string    `json:"content"`
	ContentHash string    `json:"content_hash"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListChunks 列出文章当前生效切片（契约 §4.12）。鉴权：仅作者或超管可读。
// 用于编辑页诊断 RAG 切片状态——published 文章应有切片，否则说明向量化失败。
func (s *ArticleService) ListChunks(ctx context.Context, articleID int64, actor Actor) ([]ArticleChunkDTO, error) {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return nil, translateArticleErr(err)
	}
	if err := assertCanAuthorOrAdmin(article, actor); err != nil {
		return nil, err
	}
	if s.chunks == nil {
		return []ArticleChunkDTO{}, nil
	}
	chunks, err := s.chunks.ListActiveByArticle(ctx, articleID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	dtos := make([]ArticleChunkDTO, 0, len(chunks))
	for _, c := range chunks {
		dtos = append(dtos, ArticleChunkDTO{
			ID:          c.ID,
			ChunkIndex:  c.ChunkIndex,
			Content:     c.Content,
			ContentHash: c.ContentHash,
			Version:     c.Version,
			CreatedAt:   c.CreatedAt,
		})
	}
	return dtos, nil
}

// Revectorize 重新切片向量化（契约 §4.13）。鉴权：仅作者或超管可触发。
// 仅 published 状态可重新切片；入队失败仅日志告警（与 Approve/Update 一致）。
func (s *ArticleService) Revectorize(ctx context.Context, articleID int64, actor Actor) error {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return translateArticleErr(err)
	}
	if err := assertCanAuthorOrAdmin(article, actor); err != nil {
		return err
	}
	if article.Status != constants.ArticleStatusPublished {
		return apperrors.Conflict("WIKI_NOT_PUBLISHED", "仅已发布文章可重新切片")
	}
	if s.vector == nil {
		return apperrors.ServiceUnavailable("WIKI_VECTORIZE_UNAVAILABLE", "向量化服务未配置")
	}
	if err := s.vector.Enqueue(ctx, articleID); err != nil {
		// ponytail: 入队失败仅日志告警，不阻塞响应；worker 兜底重试或运维补偿，折中。
		slog.ErrorContext(ctx, "wiki: enqueue revectorize failed",
			"article_id", articleID, "err", err)
		return apperrors.ServiceUnavailable("WIKI_VECTORIZE_ENQUEUE_FAILED", "入队失败，请稍后重试")
	}
	return nil
}

// ============ Update ============

// UpdateInput 更新文章输入（指针字段为 nil 表示不更新）。
type UpdateInput struct {
	Title          *string
	Content        *string
	Summary        *string
	CoverImageURL  *string
	AllowReference *bool
	ArticleID      int64
	Actor          Actor
	// ExpectedVersion 客户端编辑时加载到的版本号；非 nil 时启用乐观锁，
	// 版本已被他人改动则返回 409，避免并发编辑丢失更新。nil 表示不校验（向后兼容旧客户端）。
	ExpectedVersion *int
}

// Update 更新文章（契约 §4.5）。检测 content_hash；已发布文章修改后版本号递增（REQ-WIKI-005/015）。
// archived 状态为终态只读（REQ-WIKI-001 状态机）——禁止修改以保留历史完整性。
func (s *ArticleService) Update(ctx context.Context, in UpdateInput) (*ArticleStaffDTO, error) {
	if err := validateUpdateInput(in); err != nil {
		return nil, err
	}
	// 先取文章并鉴权（不在事务内，减少事务持有时间）
	article, err := s.repo.GetByID(ctx, in.ArticleID)
	if err != nil {
		return nil, translateArticleErr(err)
	}
	if err := assertCanAuthorOrAdmin(article, in.Actor); err != nil {
		return nil, err
	}
	// archived 终态只读：保留历史版本完整性，避免改内容后旧切片未失效/未重排版本。
	if article.Status == constants.ArticleStatusArchived {
		return nil, apperrors.Conflict("WIKI_ARCHIVED_READONLY", "归档文章不可修改")
	}

	fields := prepareUpdateFields(article, in)
	updated, err := s.commitUpdate(ctx, in.ArticleID, in.Actor, fields)
	if err != nil {
		return nil, err
	}
	s.enqueueVectorizeAfterUpdate(ctx, in.ArticleID, updated, fields.ContentHash != nil)
	dto := toStaffDTO(updated)
	return &dto, nil
}

// validateUpdateInput 显式传入的字段不可置空（与 Create 校验对齐，防止清空 title/content 造成脏数据）。
func validateUpdateInput(in UpdateInput) error {
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return apperrors.Validation("WIKI_TITLE_REQUIRED", "title 不能为空")
	}
	if in.Title != nil && utf8.RuneCountInString(*in.Title) > titleMaxRunes {
		return apperrors.Validation("WIKI_TITLE_TOO_LONG", "title 长度不能超过 255 字符")
	}
	if in.Content != nil && strings.TrimSpace(*in.Content) == "" {
		return apperrors.Validation("WIKI_CONTENT_REQUIRED", "content 不能为空")
	}
	return nil
}

// prepareUpdateFields 构建待更新字段：内容变化时检测 content_hash（REQ-WIKI-015），
// summary 空则自动截取（仅在 content 也更新时生效），已发布文章修改后版本号递增（REQ-WIKI-005）。
func prepareUpdateFields(article *entity.Article, in UpdateInput) repository.UpdateFields {
	fields := repository.UpdateFields{
		Title:           in.Title,
		Summary:         in.Summary,
		CoverImageURL:   in.CoverImageURL,
		AllowReference:  in.AllowReference,
		ExpectedVersion: in.ExpectedVersion,
	}
	// 乐观锁守卫：客户端未传 ExpectedVersion 时，自动使用读取时的 version 防并发丢失更新。
	if fields.ExpectedVersion == nil {
		v := article.Version
		fields.ExpectedVersion = &v
	}
	if in.Content != nil && *in.Content != article.Content {
		newHash := contenthash.SHA256(*in.Content)
		if newHash != article.ContentHash {
			fields.Content = in.Content
			fields.ContentHash = &newHash
		}
	}
	if fields.Content != nil && (in.Summary == nil || *in.Summary == "") {
		summary := truncateRunes(stripHTMLTags(*fields.Content), summaryMaxRunes)
		fields.Summary = &summary
	}
	if article.Status == constants.ArticleStatusPublished {
		fields.IncrementVersion = true
	}
	return fields
}

// commitUpdate 事务内落库更新（REQ-WIKI-016/High 3/High 4）：
// 已发布文章内容变更后失效旧切片；审计日志仅在 content_hash 变化时记录，
// 元数据变化（title/cover/allow_reference）不记审计，避免 FromStatus==ToStatus 的混淆噪声。
// 已发布文章内容变更时事务内写 outbox 记录，保证向量化最终投递。
func (s *ArticleService) commitUpdate(
	ctx context.Context, articleID int64, actor Actor, fields repository.UpdateFields,
) (*entity.Article, error) {
	var updated *entity.Article
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		a, err := s.repo.UpdateFields(ctx, articleID, fields)
		if err != nil {
			if errors.Is(err, repository.ErrVersionConflict) {
				return apperrors.Conflict("WIKI_VERSION_CONFLICT", "文章已被他人修改，请刷新后重试")
			}
			return translateArticleErr(err)
		}
		if fields.ContentHash != nil && a.Status == constants.ArticleStatusPublished && s.chunks != nil {
			if _, dErr := s.chunks.DeactivateByArticle(ctx, articleID); dErr != nil {
				return fmt.Errorf("deactivate chunks on update: %w", dErr)
			}
		}
		// 已发布文章内容变更：事务内写 outbox，保证向量化最终投递。
		if fields.ContentHash != nil && a.Status == constants.ArticleStatusPublished && s.outbox != nil {
			if oErr := s.outbox.Insert(ctx, articleID); oErr != nil {
				return fmt.Errorf("outbox insert on update: %w", oErr)
			}
		}
		if fields.ContentHash != nil {
			if err := s.audit.Create(ctx, &entity.ArticleAuditLog{
				ArticleID:  a.ID,
				OperatorID: actor.UserID,
				Action:     entity.AuditActionUpdate,
				FromStatus: a.Status,
				ToStatus:   a.Status,
				Summary:    auditSummary(a),
			}); err != nil {
				return fmt.Errorf("audit update: %w", err)
			}
		}
		updated = a
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// enqueueVectorizeAfterUpdate 已发布文章内容变更后，事务提交后异步入队重新切片向量化
// （与 Approve 入队模式一致，REQ-WIKI-011/012）。outbox 已在事务内写入，此处为快速路径。
func (s *ArticleService) enqueueVectorizeAfterUpdate(
	ctx context.Context, articleID int64, updated *entity.Article, contentChanged bool,
) {
	if !contentChanged || updated.Status != constants.ArticleStatusPublished || s.vector == nil {
		return
	}
	if enqErr := s.vector.Enqueue(ctx, articleID); enqErr != nil {
		slog.ErrorContext(ctx, "wiki: enqueue vectorize on update failed (outbox will retry)",
			"article_id", articleID, "err", enqErr)
	}
}

// ============ Submit ============

// SubmitForReview 提交审核（draft → pending，契约 §4.6）。
func (s *ArticleService) SubmitForReview(ctx context.Context, articleID int64, actor Actor) error {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return translateArticleErr(err)
	}
	if err := assertCanManage(article, actor); err != nil {
		return err
	}
	if article.Status != constants.ArticleStatusDraft {
		return apperrors.Conflict("WIKI_INVALID_STATUS", "仅草稿文章可提交审核")
	}
	return s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdateStatus(ctx, articleID,
			constants.ArticleStatusDraft, constants.ArticleStatusPending,
			repository.StatusUpdateOpts{}); err != nil {
			return s.translateArticleStatusErr(ctx, articleID, err)
		}
		return s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  articleID,
			OperatorID: actor.UserID,
			Action:     entity.AuditActionSubmit,
			FromStatus: constants.ArticleStatusDraft,
			ToStatus:   constants.ArticleStatusPending,
			Summary:    auditSummary(article),
		})
	})
}

// ============ Delete ============

// Delete 软删除文章（契约 §4.7）。任意状态可删除；is_deleted=true。
// 仅作者本人或超管可删除（契约 §4.7 "非作者或非本科室" → 403；超管除外）。
// 删除后自动撤销该文章的所有 approved 引用。
func (s *ArticleService) Delete(ctx context.Context, articleID int64, actor Actor) error {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return translateArticleErr(err)
	}
	if err := assertCanAuthorOrAdmin(article, actor); err != nil {
		return err
	}
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SoftDelete(ctx, articleID); err != nil {
			return translateArticleErr(err)
		}
		// 失效该文章的生效切片：避免软删除后 DB 残留 active 切片（数据卫生）。
		if s.chunks != nil {
			if _, dErr := s.chunks.DeactivateByArticle(ctx, articleID); dErr != nil {
				return fmt.Errorf("deactivate chunks on delete: %w", dErr)
			}
		}
		return s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  articleID,
			OperatorID: actor.UserID,
			Action:     entity.AuditActionDelete,
			FromStatus: article.Status,
			ToStatus:   constants.ArticleStatusDeleted,
			Summary:    auditSummary(article),
		})
	})
	if err != nil {
		return err
	}
	// 事务外：撤销该文章的所有 approved 引用（fire-and-forget，不阻断删除主流程）。
	s.revokeReferencesAfterArticleChange(ctx, articleID)
	return nil
}

// ============ Approve ============

// ApproveInput 审核通过输入。
type ApproveInput struct {
	ArticleID int64
	Note      string // 可选审核备注
	Actor     Actor
}

// Approve 审核通过（pending → published，契约 §4.8）。
// 管理员可自审（含自己的文章）；非本科室不可审核（超管除外）。
// 事务内写 outbox 记录 + 事务外直接入队向量化任务（快速路径），保证最终一致性。
func (s *ArticleService) Approve(ctx context.Context, in ApproveInput) error {
	article, err := s.repo.GetByID(ctx, in.ArticleID)
	if err != nil {
		return translateArticleErr(err)
	}
	if err := assertCanReview(article, in.Actor); err != nil {
		return err
	}
	if article.Status != constants.ArticleStatusPending {
		return apperrors.Conflict("WIKI_INVALID_STATUS", "仅待审核文章可审核通过")
	}
	reviewerID := in.Actor.UserID
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdateStatus(ctx, in.ArticleID,
			constants.ArticleStatusPending, constants.ArticleStatusPublished,
			repository.StatusUpdateOpts{
				ReviewerID:     &reviewerID,
				ReviewComment:  strPtrOrNil(in.Note),
				SetPublishedAt: true,
			}); err != nil {
			return s.translateArticleStatusErr(ctx, in.ArticleID, err)
		}
		// 事务内写 outbox：保证向量化任务最终投递（即使事务外 Enqueue 失败，relay 兜底）。
		if s.outbox != nil {
			if oErr := s.outbox.Insert(ctx, in.ArticleID); oErr != nil {
				return fmt.Errorf("outbox insert on approve: %w", oErr)
			}
		}
		return s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  in.ArticleID,
			OperatorID: in.Actor.UserID,
			Action:     entity.AuditActionPublish,
			FromStatus: constants.ArticleStatusPending,
			ToStatus:   constants.ArticleStatusPublished,
			Summary:    auditSummary(article),
			Reason:     in.Note,
		})
	})
	if err != nil {
		return err
	}
	// 事务外：直接入队向量化任务（快速路径）。失败仅记录，outbox relay 兜底。
	if s.vector != nil {
		if enqErr := s.vector.Enqueue(ctx, in.ArticleID); enqErr != nil {
			slog.ErrorContext(ctx, "wiki: enqueue vectorize failed (outbox will retry)",
				"article_id", in.ArticleID, "err", enqErr)
		}
	}
	return nil
}

// ============ Reject ============

// RejectInput 驳回输入。
type RejectInput struct {
	ArticleID int64
	Reason    string // 必填
	Actor     Actor
}

// Reject 驳回（pending → draft，契约 §4.9）。记录驳回原因。
func (s *ArticleService) Reject(ctx context.Context, in RejectInput) error {
	if in.Reason == "" {
		return apperrors.Validation("WIKI_REASON_REQUIRED", "reason 不能为空")
	}
	article, err := s.repo.GetByID(ctx, in.ArticleID)
	if err != nil {
		return translateArticleErr(err)
	}
	if err := assertCanReview(article, in.Actor); err != nil {
		return err
	}
	if article.Status != constants.ArticleStatusPending {
		return apperrors.Conflict("WIKI_INVALID_STATUS", "仅待审核文章可驳回")
	}
	reviewerID := in.Actor.UserID
	reason := in.Reason
	return s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdateStatus(ctx, in.ArticleID,
			constants.ArticleStatusPending, constants.ArticleStatusDraft,
			repository.StatusUpdateOpts{
				ReviewerID:    &reviewerID,
				ReviewComment: &reason,
			}); err != nil {
			return s.translateArticleStatusErr(ctx, in.ArticleID, err)
		}
		return s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  in.ArticleID,
			OperatorID: in.Actor.UserID,
			Action:     entity.AuditActionReject,
			FromStatus: constants.ArticleStatusPending,
			ToStatus:   constants.ArticleStatusDraft,
			Summary:    auditSummary(article),
			Reason:     in.Reason,
		})
	})
}

// ============ Archive ============

// Archive 归档已发布文章（published → archived，REQ-WIKI-001）。
// 仅 published 状态可归档；归档后文章对公众不可见，但保留版本以便历史回看。
// 事务内：状态迁移 + 失效切片 + 审计日志（AuditActionArchive）。
// 归档后自动撤销该文章的所有 approved 引用。
func (s *ArticleService) Archive(ctx context.Context, articleID int64, actor Actor) error {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return translateArticleErr(err)
	}
	if err := assertCanManage(article, actor); err != nil {
		return err
	}
	if article.Status != constants.ArticleStatusPublished {
		return apperrors.Conflict("WIKI_INVALID_STATUS", "仅已发布文章可归档")
	}
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdateStatus(ctx, articleID,
			constants.ArticleStatusPublished, constants.ArticleStatusArchived,
			repository.StatusUpdateOpts{}); err != nil {
			return s.translateArticleStatusErr(ctx, articleID, err)
		}
		// 失效该文章的生效切片：归档文章不应再被检索命中（数据卫生）。
		if s.chunks != nil {
			if _, dErr := s.chunks.DeactivateByArticle(ctx, articleID); dErr != nil {
				return fmt.Errorf("deactivate chunks on archive: %w", dErr)
			}
		}
		return s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  articleID,
			OperatorID: actor.UserID,
			Action:     entity.AuditActionArchive,
			FromStatus: constants.ArticleStatusPublished,
			ToStatus:   constants.ArticleStatusArchived,
			Summary:    auditSummary(article),
		})
	})
	if err != nil {
		return err
	}
	// 事务外：撤销该文章的所有 approved 引用（fire-and-forget，不阻断归档主流程）。
	s.revokeReferencesAfterArticleChange(ctx, articleID)
	return nil
}

// Unarchive 恢复归档文章（archived → published）。
// 仅 SUPER_ADMIN/DEPT_ADMIN 可执行；恢复后文章重新对公众可见，需重建切片。
// 事务内：状态迁移 + outbox 记录 + 审计日志；事务外：直接入队向量化任务（快速路径）。
func (s *ArticleService) Unarchive(ctx context.Context, articleID int64, actor Actor) error {
	article, err := s.repo.GetByID(ctx, articleID)
	if err != nil {
		return translateArticleErr(err)
	}
	if !constants.IsAdmin(actor.Role) {
		return apperrors.Forbidden("WIKI_FORBIDDEN", "仅管理员可恢复归档文章")
	}
	if article.Status != constants.ArticleStatusArchived {
		return apperrors.Conflict("WIKI_INVALID_STATUS", "仅归档文章可恢复")
	}
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdateStatus(ctx, articleID,
			constants.ArticleStatusArchived, constants.ArticleStatusPublished,
			repository.StatusUpdateOpts{SetPublishedAt: true}); err != nil {
			return s.translateArticleStatusErr(ctx, articleID, err)
		}
		// 事务内写 outbox：保证向量化任务最终投递，重建切片。
		if s.outbox != nil {
			if oErr := s.outbox.Insert(ctx, articleID); oErr != nil {
				return fmt.Errorf("outbox insert on unarchive: %w", oErr)
			}
		}
		return s.audit.Create(ctx, &entity.ArticleAuditLog{
			ArticleID:  articleID,
			OperatorID: actor.UserID,
			Action:     entity.AuditActionUnarchive,
			FromStatus: constants.ArticleStatusArchived,
			ToStatus:   constants.ArticleStatusPublished,
			Summary:    auditSummary(article),
		})
	})
	if err != nil {
		return err
	}
	// 事务外：直接入队向量化任务（快速路径）。失败仅记录，outbox relay 兜底。
	if s.vector != nil {
		if enqErr := s.vector.Enqueue(ctx, articleID); enqErr != nil {
			slog.ErrorContext(ctx, "wiki: enqueue vectorize on unarchive failed (outbox will retry)",
				"article_id", articleID, "err", enqErr)
		}
	}
	return nil
}

// ============ 鉴权辅助 ============

// revokeReferencesAfterArticleChange 文章删除/归档后撤销关联的 approved 引用。
// fire-and-forget：撤销失败仅记录日志，不阻断主业务。
// ponytail: 事务外调用，引用撤销与文章状态迁移非强一致——若需强一致可移入事务，折中。
func (s *ArticleService) revokeReferencesAfterArticleChange(ctx context.Context, articleID int64) {
	if s.refRevoker == nil {
		return
	}
	if err := s.refRevoker.RevokeByArticle(ctx, articleID); err != nil {
		slog.ErrorContext(ctx, "wiki: revoke references after article change failed",
			"article_id", articleID, "err", err)
	}
}

// assertCanManage 校验：作者本人或本科室医护（或超管）可编辑/提交/删除（REQ-SEC-002）。
func assertCanManage(a *entity.Article, actor Actor) error {
	if actor.Role == constants.RoleSuperAdmin {
		return nil
	}
	if a.AuthorID == actor.UserID {
		return nil
	}
	if a.DepartmentID != nil && *a.DepartmentID == actor.DeptID {
		return nil
	}
	return apperrors.Forbidden("WIKI_FORBIDDEN", "无权操作该文章")
}

// assertCanAuthorOrAdmin 校验：仅作者本人或超管可操作（用于 Update/Delete，契约 §4.5/4.7）。
// 同科室非作者仍返回 403——契约原文 "非作者或非本科室" 取并集，意为"既需是作者又需是本科室"，
// 简化为：作者满足"作者且本科室"（作者本身必属于其文章科室），超管跨作者豁免。
func assertCanAuthorOrAdmin(a *entity.Article, actor Actor) error {
	if actor.Role == constants.RoleSuperAdmin {
		return nil
	}
	if a.AuthorID == actor.UserID {
		return nil
	}
	return apperrors.Forbidden("WIKI_FORBIDDEN", "无权操作该文章")
}

// assertCanFeature 校验热门设置权限：仅管理员可操作；超管可操作所有文章；科室管理员限本科室。
func assertCanFeature(a *entity.Article, actor Actor) error {
	if !constants.IsAdmin(actor.Role) {
		return apperrors.Forbidden("WIKI_FEATURED_FORBIDDEN", "仅管理员可设置热门文章")
	}
	if actor.Role != constants.RoleSuperAdmin && (a.DepartmentID == nil || *a.DepartmentID != actor.DeptID) {
		return apperrors.Forbidden("WIKI_DEPT_MISMATCH", "非本科室文章不可设置热门")
	}
	return nil
}

// assertCanReview 校验审核权限：仅管理员可审核；超管可审核所有文章（含自审）；科室管理员可审核本科室文章（含自审）。
func assertCanReview(a *entity.Article, actor Actor) error {
	// 仅管理员可审核文章（SUPER_ADMIN / DEPT_ADMIN）。
	if !constants.IsAdmin(actor.Role) {
		return apperrors.Forbidden("WIKI_REVIEW_FORBIDDEN", "仅管理员可审核文章")
	}
	// 超级管理员可审核所有文章（含自己的）。
	if actor.Role == constants.RoleSuperAdmin {
		return nil
	}
	// 科室管理员可审核本科室所有文章（含自己的），不可审核其他科室文章。
	if a.DepartmentID == nil || *a.DepartmentID != actor.DeptID {
		return apperrors.Forbidden("WIKI_DEPT_MISMATCH", "非本科室文章不可审核")
	}
	return nil
}

// ============ DTO 转换 ============

func toListItemDTO(a *entity.Article) ArticleListItemDTO {
	return ArticleListItemDTO{
		ID:             a.ID,
		Title:          a.Title,
		Summary:        a.Summary,
		CoverURL:       a.CoverImageURL,
		DepartmentID:   a.DepartmentID,
		DepartmentName: a.DepartmentName,
		ViewCount:      a.ViewCount,
		Version:        a.Version,
		AllowReference: a.AllowReference,
		FeaturedRank:   a.FeaturedRank,
		PublishedAt:    a.PublishedAt,
		CreatedAt:      a.CreatedAt,
	}
}

func toDetailDTO(a *entity.Article) ArticleDetailDTO {
	return ArticleDetailDTO{
		ID:             a.ID,
		Title:          a.Title,
		Content:        a.Content,
		Summary:        a.Summary,
		CoverURL:       a.CoverImageURL,
		DepartmentID:   a.DepartmentID,
		DepartmentName: a.DepartmentName,
		ViewCount:      a.ViewCount,
		Version:        a.Version,
		AllowReference: a.AllowReference,
		AuthorID:       a.AuthorID,
		AuthorName:     a.AuthorName,
		PublishedAt:    a.PublishedAt,
		CreatedAt:      a.CreatedAt,
	}
}

func toStaffDTO(a *entity.Article) ArticleStaffDTO {
	return ArticleStaffDTO{
		ID:             a.ID,
		Title:          a.Title,
		Content:        a.Content,
		Summary:        a.Summary,
		CoverURL:       a.CoverImageURL,
		Status:         a.Status,
		Version:        a.Version,
		DepartmentID:   a.DepartmentID,
		DepartmentName: a.DepartmentName,
		AuthorID:       a.AuthorID,
		AuthorName:     a.AuthorName,
		ReviewerID:     a.ReviewerID,
		ReviewComment:  a.ReviewComment,
		ViewCount:      a.ViewCount,
		AllowReference: a.AllowReference,
		FeaturedRank:   a.FeaturedRank,
		PublishedAt:    a.PublishedAt,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
}

// ============ 通用辅助 ============

// auditSummary 生成审计日志摘要：标题 + 内容前若干字符（REQ-WIKI-002）。
// 仅记 Title 会丢失内容语义；用 "Title | Content" 拼接后按 rune 截取 100 字，
// 既保留标题上下文，又包含正文摘要，避免 PII 外泄风险（审计日志仅记录摘要长度受限的字符串）。
func auditSummary(a *entity.Article) string {
	return truncateRunes(stripHTMLTags(a.Title+" | "+a.Content), summaryMaxRunes)
}

// stripHTMLTags 去除 HTML 标签，返回纯文本。
// 用于从富文本 content 生成 summary 时剥离标签，避免摘要中出现 &lt;p&gt; 等转义标签。
var reHTMLTags = regexp.MustCompile(`<[^>]*>`)

func stripHTMLTags(s string) string {
	return reHTMLTags.ReplaceAllString(s, "")
}

// truncateRunes 按 rune 截断字符串，避免截断多字节字符。
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// strPtrOrNil 空字符串返回 nil，否则返回指针。
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// translateArticleErr 把 repo 的 ErrNotFound 翻译为 AppError 404；其他原样返回。
func translateArticleErr(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return apperrors.NotFound("WIKI_ARTICLE_NOT_FOUND", "文章不存在")
	}
	return err
}

// translateArticleStatusErr 区分 UpdateStatus 的"行不存在(404)"与"状态不匹配(409)"。
// UpdateStatus 用 WHERE status=$from 乐观锁，RowsAffected==0 可能是不存在或并发状态漂移；
// 在同事务内二次 GetByID 区分：存在则 409，不存在则 404。
func (s *ArticleService) translateArticleStatusErr(ctx context.Context, id int64, err error) error {
	if !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	if _, getErr := s.repo.GetByID(ctx, id); errors.Is(getErr, repository.ErrNotFound) {
		return apperrors.NotFound("WIKI_ARTICLE_NOT_FOUND", "文章不存在")
	} else if getErr != nil {
		return getErr
	}
	return apperrors.Conflict("WIKI_INVALID_STATUS", "状态非预期，无法操作")
}

// isValidArticleStatus 文章状态白名单校验（draft|pending|published|archived|deleted）。
func isValidArticleStatus(s string) bool {
	switch s {
	case constants.ArticleStatusDraft, constants.ArticleStatusPending,
		constants.ArticleStatusPublished, constants.ArticleStatusArchived,
		constants.ArticleStatusDeleted:
		return true
	}
	return false
}
