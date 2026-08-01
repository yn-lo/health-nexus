package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/domain/chat/repository"
	apperrors "health-nexus/internal/shared/errors"
)

// ============================================================================
// Mock: ConversationRepoPort
// ============================================================================

type mockConvRepo struct {
	items       map[uuid.UUID]*entity.Conversation
	list        []*entity.Conversation
	listErr     error
	getErr      error
	patchResult *entity.Conversation
	patchErr    error
	deleteRows  int64
	deleteErr   error
}

func (m *mockConvRepo) ListByPatient(_ context.Context, _ int64, _ bool, _, _ int) ([]*entity.Conversation, int64, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.list, int64(len(m.list)), nil
}

func (m *mockConvRepo) GetByIDForPatient(_ context.Context, id uuid.UUID, _ int64) (*entity.Conversation, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if c, ok := m.items[id]; ok {
		return c, nil
	}
	return nil, nil
}

func (m *mockConvRepo) Patch(_ context.Context, _ uuid.UUID, _ int64, _ *string, _ *bool) (*entity.Conversation, error) {
	return m.patchResult, m.patchErr
}

func (m *mockConvRepo) Delete(_ context.Context, _ uuid.UUID, _ int64) (int64, error) {
	return m.deleteRows, m.deleteErr
}

// ============================================================================
// Mock: MessageRepoPort
// ============================================================================

type mockMsgRepo struct {
	msgs    []*entity.Message
	listErr error
}

func (m *mockMsgRepo) ListByConversation(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ int) ([]*entity.Message, error) {
	return m.msgs, m.listErr
}

func (m *mockMsgRepo) UpdateFeedback(_ context.Context, _ uuid.UUID, _ int64, _ string) (int64, error) {
	return 0, nil
}

// ============================================================================
// ConversationService tests
// ============================================================================

func TestConversationService_List(t *testing.T) {
	t.Run("正常返回列表", func(t *testing.T) {
		c1 := &entity.Conversation{ID: uuid.New(), Title: "会话1", CreatedAt: time.Now(), LastMessageAt: time.Now()}
		svc := NewConversationService(&mockConvRepo{list: []*entity.Conversation{c1}}, &mockMsgRepo{})
		items, total, err := svc.List(context.Background(), 1, false, 10, 0)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if total != 1 {
			t.Errorf("期望 total=1，实际 %d", total)
		}
		if len(items) != 1 || items[0].Title != "会话1" {
			t.Errorf("期望 1 个会话，实际 %v", items)
		}
	})

	t.Run("repo错误_向上传播", func(t *testing.T) {
		svc := NewConversationService(&mockConvRepo{listErr: errors.New("db down")}, &mockMsgRepo{})
		_, _, err := svc.List(context.Background(), 1, false, 10, 0)
		if err == nil {
			t.Fatal("期望 error，实际 nil")
		}
	})
}

func TestConversationService_Get(t *testing.T) {
	t.Run("存在_返回详情", func(t *testing.T) {
		id := uuid.New()
		c := &entity.Conversation{ID: id, Title: "测试", CreatedAt: time.Now(), LastMessageAt: time.Now()}
		svc := NewConversationService(&mockConvRepo{items: map[uuid.UUID]*entity.Conversation{id: c}}, &mockMsgRepo{})
		resp, err := svc.Get(context.Background(), id, 1)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if resp.Title != "测试" {
			t.Errorf("期望 Title=测试，实际 %s", resp.Title)
		}
	})

	t.Run("不存在_返回404", func(t *testing.T) {
		svc := NewConversationService(&mockConvRepo{items: map[uuid.UUID]*entity.Conversation{}}, &mockMsgRepo{})
		_, err := svc.Get(context.Background(), uuid.New(), 1)
		assertAppErr(t, err, 404, "CHAT_CONVERSATION_NOT_FOUND")
	})

	t.Run("repo错误_向上传播", func(t *testing.T) {
		svc := NewConversationService(&mockConvRepo{getErr: errors.New("db down")}, &mockMsgRepo{})
		_, err := svc.Get(context.Background(), uuid.New(), 1)
		if err == nil {
			t.Fatal("期望 error，实际 nil")
		}
	})
}

func TestConversationService_Patch(t *testing.T) {
	t.Run("全nil_返回422", func(t *testing.T) {
		svc := NewConversationService(&mockConvRepo{}, &mockMsgRepo{})
		_, err := svc.Patch(context.Background(), uuid.New(), 1, PatchInput{})
		assertAppErr(t, err, 422, "CHAT_PATCH_EMPTY")
	})

	t.Run("正常更新标题", func(t *testing.T) {
		title := "新标题"
		patched := &entity.Conversation{ID: uuid.New(), Title: "新标题", CreatedAt: time.Now(), LastMessageAt: time.Now()}
		svc := NewConversationService(&mockConvRepo{patchResult: patched}, &mockMsgRepo{})
		resp, err := svc.Patch(context.Background(), uuid.New(), 1, PatchInput{Title: &title})
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if resp.Title != "新标题" {
			t.Errorf("期望 Title=新标题，实际 %s", resp.Title)
		}
	})

	t.Run("patch返回nil_返回404", func(t *testing.T) {
		title := "标题"
		svc := NewConversationService(&mockConvRepo{patchResult: nil}, &mockMsgRepo{})
		_, err := svc.Patch(context.Background(), uuid.New(), 1, PatchInput{Title: &title})
		assertAppErr(t, err, 404, "CHAT_CONVERSATION_NOT_FOUND")
	})

	t.Run("repo错误_向上传播", func(t *testing.T) {
		title := "标题"
		svc := NewConversationService(&mockConvRepo{patchErr: errors.New("db down")}, &mockMsgRepo{})
		_, err := svc.Patch(context.Background(), uuid.New(), 1, PatchInput{Title: &title})
		if err == nil {
			t.Fatal("期望 error，实际 nil")
		}
	})
}

func TestConversationService_Delete(t *testing.T) {
	t.Run("存在_删除成功", func(t *testing.T) {
		svc := NewConversationService(&mockConvRepo{deleteRows: 1}, &mockMsgRepo{})
		err := svc.Delete(context.Background(), uuid.New(), 1)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})

	t.Run("不存在_返回404", func(t *testing.T) {
		svc := NewConversationService(&mockConvRepo{deleteRows: 0}, &mockMsgRepo{})
		err := svc.Delete(context.Background(), uuid.New(), 1)
		assertAppErr(t, err, 404, "CHAT_CONVERSATION_NOT_FOUND")
	})

	t.Run("repo错误_向上传播", func(t *testing.T) {
		svc := NewConversationService(&mockConvRepo{deleteErr: errors.New("db down")}, &mockMsgRepo{})
		err := svc.Delete(context.Background(), uuid.New(), 1)
		if err == nil {
			t.Fatal("期望 error，实际 nil")
		}
	})
}

func TestConversationService_ListMessages(t *testing.T) {
	t.Run("会话不存在_返回404", func(t *testing.T) {
		svc := NewConversationService(
			&mockConvRepo{items: map[uuid.UUID]*entity.Conversation{}},
			&mockMsgRepo{},
		)
		_, err := svc.ListMessages(context.Background(), uuid.New(), 1, nil, 50)
		assertAppErr(t, err, 404, "CHAT_CONVERSATION_NOT_FOUND")
	})

	t.Run("正常返回消息列表", func(t *testing.T) {
		convID := uuid.New()
		conv := &entity.Conversation{ID: convID, Title: "测试", CreatedAt: time.Now(), LastMessageAt: time.Now()}
		msg := &entity.Message{
			ID: uuid.New(), ConversationID: convID, Role: "user",
			Content: "你好", CreatedAt: time.Now(),
		}
		svc := NewConversationService(
			&mockConvRepo{items: map[uuid.UUID]*entity.Conversation{convID: conv}},
			&mockMsgRepo{msgs: []*entity.Message{msg}},
		)
		items, err := svc.ListMessages(context.Background(), convID, 1, nil, 50)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if len(items) != 1 || items[0].Content != "你好" {
			t.Errorf("期望 1 条消息，实际 %v", items)
		}
	})

	t.Run("按时间升序返回_反转repo的DESC", func(t *testing.T) {
		convID := uuid.New()
		conv := &entity.Conversation{ID: convID, Title: "测试", CreatedAt: time.Now(), LastMessageAt: time.Now()}
		older := &entity.Message{
			ID: uuid.New(), ConversationID: convID, Role: "user",
			Content: "第一条", CreatedAt: time.Now().Add(-time.Minute),
		}
		newer := &entity.Message{
			ID: uuid.New(), ConversationID: convID, Role: "assistant",
			Content: "第二条", CreatedAt: time.Now(),
		}
		svc := NewConversationService(
			&mockConvRepo{items: map[uuid.UUID]*entity.Conversation{convID: conv}},
			// repo 按 created_at DESC 返回（游标分页语义），service 应反转为升序供前端渲染
			&mockMsgRepo{msgs: []*entity.Message{newer, older}},
		)
		items, err := svc.ListMessages(context.Background(), convID, 1, nil, 50)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("期望 2 条消息，实际 %d", len(items))
		}
		if items[0].Content != "第一条" || items[1].Content != "第二条" {
			t.Errorf("期望升序 [第一条 第二条]，实际 [%s %s]", items[0].Content, items[1].Content)
		}
	})

	t.Run("LastMessageAt零值_序列化为null", func(t *testing.T) {
		id := uuid.New()
		c := &entity.Conversation{ID: id, Title: "新会话", CreatedAt: time.Now()} // LastMessageAt 零值
		svc := NewConversationService(&mockConvRepo{items: map[uuid.UUID]*entity.Conversation{id: c}}, &mockMsgRepo{})
		resp, err := svc.Get(context.Background(), id, 1)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if resp.LastMessageAt != nil {
			t.Errorf("期望 LastMessageAt=nil，实际 %q", *resp.LastMessageAt)
		}
	})

	t.Run("消息含ResultCode和References", func(t *testing.T) {
		convID := uuid.New()
		conv := &entity.Conversation{ID: convID, Title: "测试", CreatedAt: time.Now(), LastMessageAt: time.Now()}
		rc := "CRISIS_DETECTED"
		msg := &entity.Message{
			ID: uuid.New(), ConversationID: convID, Role: "assistant",
			Content: "安全提示", ResultCode: rc,
			ReferencedChunks: []entity.Reference{{ChunkID: "c1", ArticleID: "a1", Score: 0.9}},
			CreatedAt:        time.Now(),
		}
		svc := NewConversationService(
			&mockConvRepo{items: map[uuid.UUID]*entity.Conversation{convID: conv}},
			&mockMsgRepo{msgs: []*entity.Message{msg}},
		)
		items, err := svc.ListMessages(context.Background(), convID, 1, nil, 50)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("期望 1 条消息")
		}
		if items[0].ResultCode == nil || *items[0].ResultCode != rc {
			t.Errorf("期望 ResultCode=%s，实际 %v", rc, items[0].ResultCode)
		}
		if len(items[0].References) != 1 || items[0].References[0].ChunkID != "c1" {
			t.Errorf("期望 1 个 Reference，实际 %v", items[0].References)
		}
	})
}

// ============================================================================
// Mock: CrisisRepoPort
// ============================================================================

type mockCrisisRepo struct {
	getResult *entity.CrisisEvent
	getErr    error
	listRows  []*repository.CrisisListRow
	listTotal int64
	listErr   error
	markOK    bool
	markErr   error
}

func (m *mockCrisisRepo) GetByID(_ context.Context, _ int64) (*entity.CrisisEvent, error) {
	return m.getResult, m.getErr
}

func (m *mockCrisisRepo) List(_ context.Context, _ repository.CrisisFilter, _, _ int) ([]*repository.CrisisListRow, int64, error) {
	return m.listRows, m.listTotal, m.listErr
}

func (m *mockCrisisRepo) MarkHandled(_ context.Context, _, _ int64, _ string) (bool, error) {
	return m.markOK, m.markErr
}

// ============================================================================
// CrisisService tests
// ============================================================================

func TestCrisisService_List(t *testing.T) {
	t.Run("正常返回列表", func(t *testing.T) {
		now := time.Now()
		rows := []*repository.CrisisListRow{
			{CrisisEvent: entity.CrisisEvent{ID: 1, PatientID: 10, Level: "high", CreatedAt: now}, PatientName: "张三"},
		}
		svc := NewCrisisService(&mockCrisisRepo{listRows: rows, listTotal: 1})
		items, total, err := svc.List(context.Background(), "", nil, CrisisActor{UserID: 1, Role: "DOCTOR", DeptID: 10}, 10, 0)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if total != 1 {
			t.Errorf("期望 total=1，实际 %d", total)
		}
		if len(items) != 1 || items[0].PatientName != "张三" {
			t.Errorf("期望 1 条记录，实际 %v", items)
		}
	})

	t.Run("repo错误_向上传播", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{listErr: errors.New("db down")})
		_, _, err := svc.List(context.Background(), "", nil, CrisisActor{UserID: 1, Role: "DOCTOR", DeptID: 10}, 10, 0)
		if err == nil {
			t.Fatal("期望 error，实际 nil")
		}
	})

	t.Run("空列表", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{})
		items, total, err := svc.List(context.Background(), "", nil, CrisisActor{UserID: 1, Role: "DOCTOR", DeptID: 10}, 10, 0)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if total != 0 || len(items) != 0 {
			t.Errorf("期望空列表，实际 total=%d len=%d", total, len(items))
		}
	})

	t.Run("MatchedKeywords为nil时返回空切片", func(t *testing.T) {
		now := time.Now()
		rows := []*repository.CrisisListRow{
			{CrisisEvent: entity.CrisisEvent{ID: 1, PatientID: 10, Level: "high", CreatedAt: now, MatchedKeywords: nil}, PatientName: "张三"},
		}
		svc := NewCrisisService(&mockCrisisRepo{listRows: rows, listTotal: 1})
		items, _, err := svc.List(context.Background(), "", nil, CrisisActor{UserID: 1, Role: "DOCTOR", DeptID: 10}, 10, 0)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if items[0].MatchedKeywords == nil {
			t.Error("期望 MatchedKeywords 为空切片，实际 nil")
		}
	})

	t.Run("HandlerID和HandledAt填充", func(t *testing.T) {
		now := time.Now()
		handlerID := int64(99)
		handledAt := now.Add(-time.Hour)
		rows := []*repository.CrisisListRow{
			{CrisisEvent: entity.CrisisEvent{
				ID: 1, PatientID: 10, Level: "high", CreatedAt: now,
				IsHandled: true, HandlerID: &handlerID, HandledAt: &handledAt,
			}, PatientName: "张三"},
		}
		svc := NewCrisisService(&mockCrisisRepo{listRows: rows, listTotal: 1})
		items, _, err := svc.List(context.Background(), "", nil, CrisisActor{UserID: 1, Role: "DOCTOR", DeptID: 10}, 10, 0)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		if items[0].HandlerID == nil || *items[0].HandlerID != 99 {
			t.Errorf("期望 HandlerID=99，实际 %v", items[0].HandlerID)
		}
		if items[0].HandledAt == nil {
			t.Error("期望 HandledAt 非 nil")
		}
	})
}

func TestCrisisService_Handle(t *testing.T) {
	t.Run("正常处理", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{
			getResult: &entity.CrisisEvent{ID: 1, IsHandled: false},
			markOK:    true,
		})
		err := svc.Handle(context.Background(), 1, 99, "已处理")
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
	})

	t.Run("不存在_返回404", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{getErr: repository.ErrNotFound})
		err := svc.Handle(context.Background(), 999, 99, "note")
		assertAppErr(t, err, 404, "CHAT_CRISIS_NOT_FOUND")
	})

	t.Run("repo其他错误_向上传播", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{getErr: errors.New("db down")})
		err := svc.Handle(context.Background(), 1, 99, "note")
		if err == nil {
			t.Fatal("期望 error，实际 nil")
		}
	})

	t.Run("已处理_查询阶段_返回409", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{
			getResult: &entity.CrisisEvent{ID: 1, IsHandled: true},
		})
		err := svc.Handle(context.Background(), 1, 99, "note")
		assertAppErr(t, err, 409, "CHAT_CRISIS_ALREADY_HANDLED")
	})

	t.Run("并发冲突_MarkHandled返回false_返回409", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{
			getResult: &entity.CrisisEvent{ID: 1, IsHandled: false},
			markOK:    false,
		})
		err := svc.Handle(context.Background(), 1, 99, "note")
		assertAppErr(t, err, 409, "CHAT_CRISIS_ALREADY_HANDLED")
	})

	t.Run("MarkHandled错误_向上传播", func(t *testing.T) {
		svc := NewCrisisService(&mockCrisisRepo{
			getResult: &entity.CrisisEvent{ID: 1, IsHandled: false},
			markErr:   errors.New("db down"),
		})
		err := svc.Handle(context.Background(), 1, 99, "note")
		if err == nil {
			t.Fatal("期望 error，实际 nil")
		}
	})
}

// ============================================================================
// 测试辅助函数
// ============================================================================

// assertAppErr 断言 err 是 *AppError 且 HTTP 和 Code 匹配。
func assertAppErr(t *testing.T, err error, wantHTTP int, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望 error，实际 nil")
	}
	appErr, ok := err.(*apperrors.AppError)
	if !ok {
		t.Fatalf("期望 *AppError，实际 %T: %v", err, err)
	}
	if appErr.HTTP != wantHTTP {
		t.Errorf("期望 HTTP=%d，实际 %d", wantHTTP, appErr.HTTP)
	}
	if appErr.Code != wantCode {
		t.Errorf("期望 Code=%s，实际 %s", wantCode, appErr.Code)
	}
}
