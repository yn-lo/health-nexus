package adapter

import (
	"context"
	"time"

	chatrepo "health-nexus/internal/domain/chat/repository"
)

// crisisOutboxEnqueuer 危机通知任务入队能力（消费者定义，ISP）。
// 由 AsynqCrisisNotifier.NotifyCrisis 满足。
type crisisOutboxEnqueuer interface {
	NotifyCrisis(ctx context.Context, eventID int64) error
}

// CrisisOutboxRelay 扫描 crisis_outbox 未处理记录并投递危机通知任务（P1）。
// 危机事件创建时在同一事务内写入 outbox 记录，因此即使 SSE 链路的快速入队失败，
// 本 relay 也会在下一次扫描时补投，保证医护通知最终送达。
type CrisisOutboxRelay struct {
	outbox   *chatrepo.CrisisOutboxRepo
	enqueuer crisisOutboxEnqueuer
}

// NewCrisisOutboxRelay 构造危机通知 outbox relay。
func NewCrisisOutboxRelay(outbox *chatrepo.CrisisOutboxRepo, enqueuer crisisOutboxEnqueuer) *CrisisOutboxRelay {
	return &CrisisOutboxRelay{outbox: outbox, enqueuer: enqueuer}
}

// RunOnce 执行一次扫描 + 投递，返回投递的记录数。
func (r *CrisisOutboxRelay) RunOnce(ctx context.Context) (int, error) {
	records, err := r.outbox.ListPending(ctx, outboxBatchSize)
	if err != nil {
		return 0, err
	}
	batch := make([]outboxRecord, 0, len(records))
	for _, rec := range records {
		batch = append(batch, outboxRecord{ID: rec.ID, Payload: rec.EventID})
	}
	// 通知按 event_id 幂等落库（落库前先查已存在），重复入队可接受。
	return deliverOutbox(ctx, "crisis outbox", "event_id", batch,
		r.enqueuer.NotifyCrisis, r.outbox.MarkProcessed), nil
}

// Start 启动定时扫描（阻塞，直到 ctx 取消）。
func (r *CrisisOutboxRelay) Start(ctx context.Context, interval time.Duration) {
	runRelayLoop(ctx, "crisis outbox relay", interval, r.RunOnce)
}
