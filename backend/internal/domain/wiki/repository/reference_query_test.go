package repository

import (
	"testing"

	"health-nexus/internal/shared/constants"
)

func TestBuildListWhere(t *testing.T) {
	t.Run("空filter_仅1=1", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{})
		if where != "1=1" {
			t.Errorf("期望 where='1=1', got %s", where)
		}
		if len(args) != 0 {
			t.Errorf("期望 0 args, got %d", len(args))
		}
	})

	t.Run("Status过滤", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{Status: "approved"})
		if where == "1=1" {
			t.Error("期望 where 含 status 条件")
		}
		if len(args) != 1 || args[0] != "approved" {
			t.Errorf("期望 args=[approved], got %v", args)
		}
	})

	t.Run("Outgoing方向_CurrentDept>0", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{
			Direction:   constants.ReferenceDirectionOutgoing,
			CurrentDept: 5,
		})
		if where == "1=1" {
			t.Error("期望 where 含 source_dept_id 条件")
		}
		if len(args) != 1 || args[0].(int64) != 5 {
			t.Errorf("期望 args=[5], got %v", args)
		}
	})

	t.Run("Outgoing方向_CurrentDept=0_跳过条件", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{
			Direction:   constants.ReferenceDirectionOutgoing,
			CurrentDept: 0,
		})
		if where != "1=1" {
			t.Errorf("期望 where='1=1' (CurrentDept=0 跳过), got %s", where)
		}
		if len(args) != 0 {
			t.Errorf("期望 0 args, got %d", len(args))
		}
	})

	t.Run("Incoming方向_CurrentDept>0", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{
			Direction:   constants.ReferenceDirectionIncoming,
			CurrentDept: 3,
		})
		if where == "1=1" {
			t.Error("期望 where 含 target_dept_id 条件")
		}
		if len(args) != 1 || args[0].(int64) != 3 {
			t.Errorf("期望 args=[3], got %v", args)
		}
	})

	t.Run("空方向_CurrentDept>0_双向条件", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{CurrentDept: 7})
		if where == "1=1" {
			t.Error("期望 where 含双向 source/target 条件")
		}
		if len(args) != 1 || args[0].(int64) != 7 {
			t.Errorf("期望 args=[7], got %v", args)
		}
	})

	t.Run("DeptIDs非nil_科室隔离", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{DeptIDs: []int64{1, 2, 3}})
		if where == "1=1" {
			t.Error("期望 where 含 ANY 条件")
		}
		if len(args) != 1 {
			t.Errorf("期望 1 arg (deptIDs slice), got %d", len(args))
		}
	})

	t.Run("全字段组合", func(t *testing.T) {
		where, args := buildListWhere(ListFilter{
			Status:      "pending",
			Direction:   constants.ReferenceDirectionOutgoing,
			CurrentDept: 5,
			DeptIDs:     []int64{1, 2},
		})
		if where == "1=1" {
			t.Error("期望 where 含多个条件")
		}
		if len(args) != 3 {
			t.Errorf("期望 3 args (status + dept + deptIDs), got %d", len(args))
		}
	})
}
