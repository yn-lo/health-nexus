// Package integration 真库集成测试：用真实 PostgreSQL 验证关键写入链路与检索可见性。
//
// 覆盖（均为本轮修复项）：
//   - 消息持久化与顺序（P0-1 / P0-2）：RETURNING 与 Scan 列数一致、同轮消息按 seq 定序；
//   - 向量化写入（P0-3）：旧切片失效删除、新切片按当前版本写入并记录向量模型；
//     embedding 期间文章被更新时必须放弃写入（不覆盖新版本切片）；
//   - 检索可见性（P1）：待重新审核期间继续服务已审核版本；未发布/高风险逾期/已过期/异模型切片排除；
//   - 危机闭环（P1）：事件创建同事务写入 crisis_outbox、接单时限按级别设置、超时事件可升级且不重复升级。
//
// 运行：INTEGRATION_TEST_DSN='postgres://user:pass@host:5432/db?sslmode=disable' \
//
//	go test -tags=integration ./tests/integration/... -v
//
// 未设置 DSN 且默认地址不可达时整体 Skip（不阻断本地无库环境）。
// 所有用例的写入都在事务内完成并回滚，不污染数据库。
//
//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	asynqlib "github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"health-nexus/internal/adapter"
	chatentity "health-nexus/internal/domain/chat/entity"
	chatrepo "health-nexus/internal/domain/chat/repository"
	wikientity "health-nexus/internal/domain/wiki/entity"
	wikirepo "health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/contenthash"
)

// errRollback 用于在校验结束后回滚事务（测试数据不落库）。
var errRollback = errors.New("rollback after verification")

// testDSN 解析测试用 DSN：优先 INTEGRATION_TEST_DSN，其次 SCHEMA_TEST_DSN（与 schema 校验共用），
// 均未设置时回退本地默认地址。
func testDSN() string {
	for _, key := range []string{"INTEGRATION_TEST_DSN", "SCHEMA_TEST_DSN"} {
		if dsn := os.Getenv(key); dsn != "" {
			return dsn
		}
	}
	return "postgres://health:health@localhost:5432/health_nexus?sslmode=disable"
}

// newPool 连接测试库；不可达时 Skip。
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, testDSN())
	if err != nil {
		t.Skipf("跳过：创建连接池失败: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("跳过：数据库不可达: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// firstConversation 取一个已有会话（消息/危机事件写入的外键依赖）。
func firstConversation(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, int64) {
	t.Helper()
	var convID uuid.UUID
	var patientID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT id, patient_id FROM conversations LIMIT 1`).Scan(&convID, &patientID); err != nil {
		t.Skipf("跳过：库中无会话数据: %v", err)
	}
	return convID, patientID
}

// firstUserID 取一个已有用户（文章 author_id 外键依赖）。
func firstUserID(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users LIMIT 1`).Scan(&id); err != nil {
		t.Skipf("跳过：库中无用户数据: %v", err)
	}
	return id
}

// onesVector 返回 1024 维单位向量（首维为 1）。
func onesVector() []float32 {
	v := make([]float32, 1024)
	v[0] = 1
	return v
}

// fakeEmbedder 固定返回单位向量；onEmbed 用于模拟 embedding 期间的并发副作用。
type fakeEmbedder struct {
	onEmbed func()
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.onEmbed != nil {
		f.onEmbed()
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = onesVector()
	}
	return out, nil
}

func (f *fakeEmbedder) EmbeddingModel() string { return "integration-check-model" }

// ── 消息持久化与顺序 ────────────────────────────────────────────────────────

// TestMessagePersistenceAndOrder 覆盖 P0-1 / P0-2：
// RETURNING 列数与 Scan 目标一致（否则所有消息写入失败）；
// 同一事务内写入的 user + assistant 占位必须靠 seq 保证"先问后答"。
func TestMessagePersistenceAndOrder(t *testing.T) {
	pool := newPool(t)
	convID, _ := firstConversation(t, pool)
	repo := chatrepo.NewMessageRepo(pool)
	txm := postgres.NewTxManager(pool)
	turnID := uuid.New()

	err := txm.WithTx(context.Background(), func(ctx context.Context) error {
		user, err := repo.SaveUserMessage(ctx, convID, turnID, "集成校验：高血压日常怎么监测血压")
		if err != nil {
			t.Fatalf("SaveUserMessage 失败（RETURNING/Scan 列数不一致？）: %v", err)
		}
		placeholder, err := repo.SaveAssistantPlaceholder(ctx, convID, turnID)
		if err != nil {
			t.Fatalf("SaveAssistantPlaceholder 失败: %v", err)
		}
		const answer = "建议每天固定时间测量并记录。"
		if err := repo.FinalizeAssistant(ctx, placeholder.ID, answer, "ANSWERED", nil); err != nil {
			t.Fatalf("FinalizeAssistant 失败: %v", err)
		}
		if user.Seq >= placeholder.Seq {
			t.Fatalf("seq 未严格递增：user=%d assistant=%d", user.Seq, placeholder.Seq)
		}

		msgs, err := repo.ListByConversation(ctx, convID, nil, 5)
		if err != nil {
			t.Fatalf("ListByConversation 失败: %v", err)
		}
		if len(msgs) < 2 || msgs[0].Role != "assistant" || msgs[1].Role != "user" {
			t.Fatalf("列表顺序异常：期望 assistant→user，实际 %d 条", len(msgs))
		}
		history, err := repo.GetRecentHistory(ctx, convID, 3, nil)
		if err != nil {
			t.Fatalf("GetRecentHistory 失败: %v", err)
		}
		if len(history) < 2 {
			t.Fatalf("历史返回 %d 条，期望至少 2 条", len(history))
		}
		last := history[len(history)-1]
		if history[len(history)-2].Role != "user" || last.Role != "assistant" {
			t.Fatalf("历史顺序异常：期望 user→assistant")
		}
		if last.Content != answer {
			t.Fatalf("历史内容回填失败：%q", last.Content)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("校验失败: %v", err)
	}
}

// ── 向量化写入 ──────────────────────────────────────────────────────────────

// TestVectorizeAtomicSwap 覆盖 P0-3：正常路径原子替换切片；
// embedding 期间文章被更新（版本变化）时放弃写入，不覆盖新版本切片。
func TestVectorizeAtomicSwap(t *testing.T) {
	pool := newPool(t)
	authorID := firstUserID(t, pool)
	articleRepo := wikirepo.NewArticleRepo(pool)
	chunkRepo := wikirepo.NewChunkRepo(pool)
	txm := postgres.NewTxManager(pool)

	newArticle := func(ctx context.Context, title string, version int) int64 {
		t.Helper()
		var id int64
		err := postgres.Q(ctx, pool).QueryRow(ctx,
			`INSERT INTO articles (title, content, status, version, content_hash, author_id, published_at)
			 VALUES ($1, $2, 'published', $3, '', $4, now()) RETURNING id`,
			title, "集成校验正文：高血压患者应每天固定时间测量血压并记录。", version, authorID).Scan(&id)
		if err != nil {
			t.Fatalf("插入校验文章失败: %v", err)
		}
		return id
	}
	insertChunk := func(ctx context.Context, articleID int64, content string, version int, model string) {
		t.Helper()
		if err := chunkRepo.Create(ctx, &wikientity.ArticleChunk{
			ArticleID: articleID, ChunkIndex: 0, Content: content,
			ContentHash: contenthash.SHA256(content),
			Embedding:   pgvector.NewVector(onesVector()), EmbeddingModel: model,
			IsActive: true, Version: version,
		}); err != nil {
			t.Fatalf("插入校验切片失败: %v", err)
		}
	}

	err := txm.WithTx(context.Background(), func(ctx context.Context) error {
		// 正常路径：旧切片（v1）应被失效并物理删除，新切片按当前版本（v2）写入。
		artID := newArticle(ctx, "集成校验-正常向量化", 2)
		insertChunk(ctx, artID, "旧版本切片", 1, "old-model")

		h := adapter.NewVectorizeHandler(articleRepo, chunkRepo, &fakeEmbedder{}, nil, txm)
		task := asynqlib.NewTask("wiki:vectorize_article", []byte(fmt.Sprint(artID)))
		if err := h.HandleVectorize(ctx, task); err != nil {
			t.Fatalf("HandleVectorize 正常路径失败: %v", err)
		}
		var active, inactive int
		if err := postgres.Q(ctx, pool).QueryRow(ctx,
			`SELECT count(*) FILTER (WHERE is_active), count(*) FILTER (WHERE NOT is_active)
			 FROM article_chunks WHERE article_id = $1`, artID).Scan(&active, &inactive); err != nil {
			t.Fatalf("统计切片失败: %v", err)
		}
		if active != 1 || inactive != 0 {
			t.Fatalf("切片状态异常：active=%d inactive=%d（期望 1/0）", active, inactive)
		}
		var content, model string
		var version int
		if err := postgres.Q(ctx, pool).QueryRow(ctx,
			`SELECT content, embedding_model, version FROM article_chunks WHERE article_id = $1 AND is_active`,
			artID).Scan(&content, &model, &version); err != nil {
			t.Fatalf("读取新切片失败: %v", err)
		}
		if model != "integration-check-model" {
			t.Errorf("切片 embedding_model = %q，期望记录当前模型", model)
		}
		if version != 2 {
			t.Errorf("切片 version = %d，期望 2（文章当前版本）", version)
		}
		if content == "旧版本切片" {
			t.Error("生效切片仍是旧内容")
		}

		// 竞态路径：embedding 期间文章版本 5 → 6，本任务必须放弃写入。
		raceID := newArticle(ctx, "集成校验-竞态", 5)
		insertChunk(ctx, raceID, "竞态前的生效切片", 5, "integration-check-model")
		racer := &fakeEmbedder{onEmbed: func() {
			_, _ = postgres.Q(ctx, pool).Exec(ctx,
				`UPDATE articles SET version = version + 1 WHERE id = $1`, raceID)
		}}
		rh := adapter.NewVectorizeHandler(articleRepo, chunkRepo, racer, nil, txm)
		raceTask := asynqlib.NewTask("wiki:vectorize_article", []byte(fmt.Sprint(raceID)))
		rerr := rh.HandleVectorize(ctx, raceTask)
		if rerr == nil || !errors.Is(rerr, asynqlib.SkipRetry) {
			t.Fatalf("竞态路径应返回 SkipRetry 并放弃写入，实际: %v", rerr)
		}
		var raceContent string
		if err := postgres.Q(ctx, pool).QueryRow(ctx,
			`SELECT content FROM article_chunks WHERE article_id = $1 AND is_active`,
			raceID).Scan(&raceContent); err != nil {
			t.Fatalf("读取竞态切片失败: %v", err)
		}
		if raceContent != "竞态前的生效切片" {
			t.Errorf("竞态路径覆盖了原切片：%q", raceContent)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("校验失败: %v", err)
	}
}

// ── 检索可见性 ──────────────────────────────────────────────────────────────

// TestRetrievalVisibility 覆盖 P1 检索可见性：只有"已审核发布版且未过期、非高风险逾期"的切片可命中；
// 指定向量模型时其他模型的切片排除。
func TestRetrievalVisibility(t *testing.T) {
	pool := newPool(t)
	authorID := firstUserID(t, pool)
	chunkRepo := wikirepo.NewChunkRepo(pool)
	txm := postgres.NewTxManager(pool)

	type spec struct {
		name       string
		status     string
		risk       string
		overdue    bool
		validPast  bool
		everPubbed bool
		model      string
		wantPlain  bool
		wantModel  bool
	}
	specs := []spec{
		{name: "已发布_正常", status: "published", risk: "normal", everPubbed: true, model: "m1", wantPlain: true, wantModel: true},
		{name: "待重新审核_曾发布", status: "pending", risk: "normal", everPubbed: true, model: "m1", wantPlain: true, wantModel: true},
		{name: "待审核_从未发布", status: "pending", risk: "normal", model: "m1"},
		{name: "已发布_高风险逾期", status: "published", risk: "high", overdue: true, everPubbed: true, model: "m1"},
		{name: "已发布_有效期已过", status: "published", risk: "normal", validPast: true, everPubbed: true, model: "m1"},
		{name: "已发布_其他模型", status: "published", risk: "normal", everPubbed: true, model: "other-model", wantPlain: true},
	}

	err := txm.WithTx(context.Background(), func(ctx context.Context) error {
		ids := map[string]int64{}
		for i, s := range specs {
			pubAt, validUntil := "NULL", "NULL"
			if s.everPubbed {
				pubAt = "now()"
			}
			if s.validPast {
				validUntil = "now() - interval '1 day'"
			}
			var id int64
			sql := fmt.Sprintf(`INSERT INTO articles
				(title, content, status, version, content_hash, author_id, content_risk, published_at, valid_until, review_overdue)
				VALUES ($1, $2, $3, 1, '', $4, $5, %s, %s, $6) RETURNING id`, pubAt, validUntil)
			if err := postgres.Q(ctx, pool).QueryRow(ctx, sql,
				fmt.Sprintf("集成校验文章-%d", i), "集成校验内容", s.status, authorID, s.risk, s.overdue,
			).Scan(&id); err != nil {
				t.Fatalf("插入文章 %s 失败: %v", s.name, err)
			}
			ids[s.name] = id
			if err := chunkRepo.Create(ctx, &wikientity.ArticleChunk{
				ArticleID: id, ChunkIndex: 0, Content: "集成校验切片",
				ContentHash: contenthash.SHA256("集成校验切片"),
				Embedding:   pgvector.NewVector(onesVector()), EmbeddingModel: s.model,
				IsActive: true, Version: 1,
			}); err != nil {
				t.Fatalf("插入切片 %s 失败: %v", s.name, err)
			}
		}

		check := func(label, model string, want func(spec) bool) {
			hits, err := chunkRepo.SearchByVector(ctx, onesVector(), 50, nil, 0.0, model)
			if err != nil {
				t.Fatalf("[%s] SearchByVector 失败: %v", label, err)
			}
			got := map[int64]bool{}
			for _, h := range hits {
				got[h.ArticleID] = true
			}
			for _, s := range specs {
				if got[ids[s.name]] != want(s) {
					t.Errorf("[%s] %s 命中=%v，期望 %v", label, s.name, got[ids[s.name]], want(s))
				}
			}
		}
		check("不指定模型", "", func(s spec) bool { return s.wantPlain })
		check("指定模型 m1", "m1", func(s spec) bool { return s.wantModel })
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("校验失败: %v", err)
	}
}

// ── 知识条目元数据写入 ──────────────────────────────────────────────────────

// TestArticleCreateWithMetadata 覆盖 P1：创建文章时元数据（来源/适用人群/有效期/风险等级）
// 必须与实体字段一一对应地落库（INSERT 列与参数错位是本类改动最易犯的错）。
func TestArticleCreateWithMetadata(t *testing.T) {
	pool := newPool(t)
	authorID := firstUserID(t, pool)
	articleRepo := wikirepo.NewArticleRepo(pool)
	txm := postgres.NewTxManager(pool)

	validUntil := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
	err := txm.WithTx(context.Background(), func(ctx context.Context) error {
		a := &wikientity.Article{
			Title:                "集成校验-元数据落库",
			Content:              "集成校验正文",
			Summary:              "摘要",
			Status:               "draft",
			Version:              1,
			ContentHash:          contenthash.SHA256("集成校验正文"),
			AuthorID:             authorID,
			Source:               "《集成校验来源》",
			ApplicablePopulation: "高血压患者",
			ValidUntil:           &validUntil,
			ContentRisk:          wikientity.ContentRiskHigh,
		}
		if err := articleRepo.Create(ctx, a); err != nil {
			t.Fatalf("Create 失败: %v", err)
		}
		got, err := articleRepo.GetByID(ctx, a.ID)
		if err != nil {
			t.Fatalf("GetByID 失败: %v", err)
		}
		if got.Source != "《集成校验来源》" {
			t.Errorf("source = %q，期望原样落库", got.Source)
		}
		if got.ApplicablePopulation != "高血压患者" {
			t.Errorf("applicable_population = %q，期望原样落库", got.ApplicablePopulation)
		}
		if got.ContentRisk != wikientity.ContentRiskHigh {
			t.Errorf("content_risk = %q，期望 %q", got.ContentRisk, wikientity.ContentRiskHigh)
		}
		if got.ValidUntil == nil || !got.ValidUntil.Equal(validUntil) {
			t.Errorf("valid_until = %v，期望 %v", got.ValidUntil, validUntil)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("校验失败: %v", err)
	}
}

// ── 危机闭环 ────────────────────────────────────────────────────────────────

// TestCrisisOutboxAndEscalation 覆盖 P1：事件创建同事务写入 crisis_outbox、
// 接单时限按级别设置、超时事件可被升级且不重复升级。
func TestCrisisOutboxAndEscalation(t *testing.T) {
	pool := newPool(t)
	convID, patientID := firstConversation(t, pool)
	crisisRepo := chatrepo.NewCrisisRepo(pool)
	outboxRepo := chatrepo.NewCrisisOutboxRepo(pool)
	txm := postgres.NewTxManager(pool)

	err := txm.WithTx(context.Background(), func(ctx context.Context) error {
		eventID, err := crisisRepo.Create(ctx, &chatentity.CrisisEvent{
			PatientID:        patientID,
			ConversationID:   convID,
			TriggeredContent: "集成校验：我不想再醒过来",
			MatchedKeywords:  []string{"self_harm: 集成校验"},
			Level:            "high",
		})
		if err != nil {
			t.Fatalf("创建危机事件失败: %v", err)
		}

		pending, err := outboxRepo.ListPending(ctx, 50)
		if err != nil {
			t.Fatalf("ListPending 失败: %v", err)
		}
		found := false
		for _, rec := range pending {
			if rec.EventID == eventID {
				found = true
			}
		}
		if !found {
			t.Fatalf("crisis_outbox 未写入事件 %d 的待投递记录（通知无法补投）", eventID)
		}

		var dueMinutes float64
		if err := postgres.Q(ctx, pool).QueryRow(ctx,
			`SELECT EXTRACT(EPOCH FROM (acknowledge_due_at - created_at)) / 60
			 FROM crisis_events WHERE id = $1`, eventID).Scan(&dueMinutes); err != nil {
			t.Fatalf("查询接单时限失败: %v", err)
		}
		if dueMinutes < 14 || dueMinutes > 16 {
			t.Errorf("high 级别接单时限 = %.1f 分钟，期望约 15 分钟", dueMinutes)
		}

		if _, err := postgres.Q(ctx, pool).Exec(ctx,
			`UPDATE crisis_events SET acknowledge_due_at = now() - interval '1 minute' WHERE id = $1`,
			eventID); err != nil {
			t.Fatalf("构造超时事件失败: %v", err)
		}
		listed := func() bool {
			overdue, err := crisisRepo.ListOverdueUnhandled(ctx, 50)
			if err != nil {
				t.Fatalf("ListOverdueUnhandled 失败: %v", err)
			}
			for _, o := range overdue {
				if o.ID == eventID {
					return true
				}
			}
			return false
		}
		if !listed() {
			t.Fatalf("超时未处理事件 %d 未被升级扫描列出", eventID)
		}
		ok, err := crisisRepo.MarkEscalated(ctx, eventID)
		if err != nil {
			t.Fatalf("MarkEscalated 失败: %v", err)
		}
		if !ok {
			t.Fatalf("MarkEscalated 未生效（事件 %d）", eventID)
		}
		if listed() {
			t.Errorf("已升级事件 %d 仍被重复列出", eventID)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("校验失败: %v", err)
	}
}
