package repository

import (
	"fmt"

	"health-nexus/internal/shared/constants"
)

// buildListWhere 构建引用列表的 WHERE 子句和参数。
// 纯函数，无 DB 依赖，可单元测试。
func buildListWhere(f ListFilter) (where string, args []any) {
	where = "1=1"
	args = []any{}
	addCond := func(cond string, val any) {
		args = append(args, val)
		where += fmt.Sprintf(" AND %s $%d", cond, len(args))
	}
	if f.Status != "" {
		addCond("ref.status =", f.Status)
	}
	switch f.Direction {
	case constants.ReferenceDirectionOutgoing:
		if f.CurrentDept > 0 {
			addCond("ref.source_dept_id =", f.CurrentDept)
		}
	case constants.ReferenceDirectionIncoming:
		if f.CurrentDept > 0 {
			addCond("ref.target_dept_id =", f.CurrentDept)
		}
	case "":
		if f.CurrentDept > 0 {
			args = append(args, f.CurrentDept)
			where += fmt.Sprintf(" AND (ref.source_dept_id = $%d OR ref.target_dept_id = $%d)", len(args), len(args))
		}
	}
	if f.DeptIDs != nil {
		args = append(args, f.DeptIDs)
		where += fmt.Sprintf(
			" AND (ref.source_dept_id = ANY($%d) OR ref.target_dept_id = ANY($%d))", len(args), len(args))
	}
	return where, args
}
