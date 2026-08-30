// Package handler 实现 auth 域 HTTP 端点。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"health-nexus/internal/domain/auth/service"
	"health-nexus/internal/shared/contextkeys"
	apperrors "health-nexus/internal/shared/errors"
	"health-nexus/internal/shared/pagination"
	"health-nexus/internal/shared/response"
)

// AuthHandler 处理 auth 域的 14 个 HTTP 端点。
// 公开/JWT 自助端点挂载于 /api/auth；管理员账户管理端点挂载于 /api/staff/auth（JWT + RequireAdmin）。
type AuthHandler struct {
	svc *service.AuthService
}

// NewAuthHandler 构造 AuthHandler。
func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// loginRequest 登录请求体。
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// registerRequest 注册请求体（PATIENT 注册须携带邀请码 invite_code，强制）。
type registerRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
}

// refreshRequest 刷新请求体。
type refreshRequest struct {
	Refresh string `json:"refresh"`
}

// logoutRequest 登出请求体。
type logoutRequest struct {
	Refresh string `json:"refresh"`
}

// passwordResetRequestRequest 请求密码重置请求体。
type passwordResetRequestRequest struct {
	Username string `json:"username"`
}

// passwordResetConfirmRequest 确认密码重置请求体。
type passwordResetConfirmRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// changePasswordRequest 已登录用户修改密码请求体。
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// updateProfileRequest 更新个人资料请求体。
type updateProfileRequest struct {
	Phone            string     `json:"phone"`
	DateOfBirth      *time.Time `json:"date_of_birth"`
	Gender           string     `json:"gender"`
	EmergencyContact string     `json:"emergency_contact"`
	EmergencyPhone   string     `json:"emergency_phone"`
}

// createAccountRequest 管理员创建账户请求体。
type createAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	DeptID   int64  `json:"dept_id"`
}

// generateInviteRequest 生成邀请码请求体（count 可省略，缺省为 1，上限 100）。
type generateInviteRequest struct {
	Count int `json:"count"`
}

// resetPasswordRequest 超级管理员重置用户密码请求体。
type resetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// updateDeptRequest 修改账户科室请求体。
type updateDeptRequest struct {
	DeptID int64 `json:"dept_id"`
}

// updateRoleRequest 修改账户角色请求体。
type updateRoleRequest struct {
	Role string `json:"role"`
}

// refreshResponse 刷新响应体。
type refreshResponse struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

// successResponse 动作类端点统一响应体。
type successResponse struct {
	Success bool `json:"success"`
}

// UnifiedLogin POST /api/auth/login - 统一登录。
// 缺字段（username/password 为空）走 login() 路径返回 401 AUTH_INVALID_CREDENTIALS，
// 而非 422 VALIDATION_MISSING--这是有意安全设计：模糊响应避免泄露用户名是否存在。
func (h *AuthHandler) UnifiedLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	res, err := h.svc.UnifiedLogin(r.Context(), req.Username, req.Password)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, res)
}

// Register POST /api/auth/register - 患者注册（201）。
// 字段缺失返回 422 VALIDATION_MISSING（与 refresh/logout 一致，契约 §1.3）；
// 字段存在但格式不合法（用户名非法字符/密码强度不足）返回 422（语义验证错误）。
// invite_code（邀请码）必填：无邀请码或邀请码无效/过期/已用均返回 422。
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if req.Username == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "username 字段必填"))
		return
	}
	if req.Password == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "password 字段必填"))
		return
	}
	if req.InviteCode == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "invite_code 字段必填"))
		return
	}
	res, err := h.svc.Register(r.Context(), req.Username, req.Password, req.InviteCode)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, res)
}

// Refresh POST /api/auth/refresh — 刷新 Token（轮换 refresh）。
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if req.Refresh == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "refresh 字段必填"))
		return
	}
	access, refresh, err := h.svc.Refresh(r.Context(), req.Refresh)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, refreshResponse{Access: access, Refresh: refresh})
}

// handleSingleFieldAction 处理"解码单字段请求 + 校验非空 + 调用无返回 service + 写成功"的端点流程，
// 抽离 Logout/PasswordResetRequest 的重复样板（fieldValue 取出被校验的请求字段）。
func handleSingleFieldAction[T any](
	w http.ResponseWriter, r *http.Request,
	fieldName string, fieldValue func(*T) string,
	action func(context.Context, string) error,
) {
	var req T
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	val := fieldValue(&req)
	if val == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", fieldName+" 字段必填"))
		return
	}
	if err := action(r.Context(), val); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, successResponse{Success: true})
}

// Logout POST /api/auth/logout — 登出（JWT 认证，将 refresh token 加黑名单）。
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	handleSingleFieldAction(w, r, "refresh",
		func(q *logoutRequest) string { return q.Refresh },
		h.svc.Logout)
}

// PasswordResetRequest POST /api/auth/password-reset/request — 请求密码重置。
// 生成重置 token 存入 Redis（TTL 15 分钟），始终返回成功（安全设计：不泄露用户是否存在）。
func (h *AuthHandler) PasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	handleSingleFieldAction(w, r, "username",
		func(q *passwordResetRequestRequest) string { return q.Username },
		h.svc.RequestPasswordReset)
}

// PasswordResetConfirm POST /api/auth/password-reset/confirm — 确认密码重置。
// 校验 token 有效性，更新密码，token 一次性使用。
func (h *AuthHandler) PasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var req passwordResetConfirmRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if req.Token == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "token 字段必填"))
		return
	}
	if req.NewPassword == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "new_password 字段必填"))
		return
	}
	if err := h.svc.ConfirmPasswordReset(r.Context(), req.Token, req.NewPassword); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, successResponse{Success: true})
}

// ChangePassword POST /api/auth/change-password — 已登录用户修改自己的密码（JWT 认证）。
// userID 取自 JWT 中间件注入的 ctx，不由请求体传入——避免越权改他人密码。
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(contextkeys.UserID).(int64)
	if !ok || userID <= 0 {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if req.OldPassword == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "old_password 字段必填"))
		return
	}
	if req.NewPassword == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "new_password 字段必填"))
		return
	}
	if err := h.svc.ChangePassword(r.Context(), userID, req.OldPassword, req.NewPassword); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, successResponse{Success: true})
}

// GetProfile GET /api/auth/profile — 读取已登录用户的个人资料（JWT 认证）。
func (h *AuthHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(contextkeys.UserID).(int64)
	if !ok || userID <= 0 {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	profile, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, profile)
}

// UpdateProfile PATCH /api/auth/profile — 更新已登录用户的个人资料（JWT 认证）。
func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(contextkeys.UserID).(int64)
	if !ok || userID <= 0 {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	var req updateProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	profile, err := h.svc.UpdateProfile(
		r.Context(), userID, req.Phone, req.DateOfBirth,
		req.Gender, req.EmergencyContact, req.EmergencyPhone,
	)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, profile)
}

// ListAccounts GET /api/staff/auth/accounts — 管理员分页查询账户列表（JWT + RequireAdmin）。
// 数据隔离：非超管仅查看本科室账户。
// 查询参数：page、page_size（pagination 统一解析，page_size 上限 100）、include_deleted（仅超管生效，为 true 时包含已删除用户）。
func (h *AuthHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	params, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	_, actorRole, actorDeptID, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	includeDeleted := r.URL.Query().Get("include_deleted") == "true"
	accounts, total, err := h.svc.ListAccounts(
		r.Context(), actorRole, actorDeptID,
		int64(params.Page), int64(params.PageSize), includeDeleted,
	)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, pagination.NewResult(accounts, total, params))
}

// CreateAccount POST /api/staff/auth/accounts — 管理员创建账户（JWT + RequireAdmin，201）。
// 角色提权收口在 service 层：非 SUPER_ADMIN 不得创建管理员角色账户。
func (h *AuthHandler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	_, actorRole, actorDeptID, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	var req createAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if req.Username == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "username 字段必填"))
		return
	}
	if req.Password == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "password 字段必填"))
		return
	}
	if req.Role == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "role 字段必填"))
		return
	}
	account, err := h.svc.CreateAccount(
		r.Context(), actorRole, actorDeptID, req.Username, req.Password, req.Role, req.DeptID,
	)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, account)
}

// GenerateInviteCodes POST /api/staff/auth/invite-codes — 管理员批量生成患者邀请码（JWT + RequireAdmin，201）。
// count 越界由 service 层收口到 [1,100]。返回每个新生成的码及其有效期（30 天）。
func (h *AuthHandler) GenerateInviteCodes(w http.ResponseWriter, r *http.Request) {
	actorID, actorRole, _, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	var req generateInviteRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	codes, err := h.svc.CreateInviteCodes(r.Context(), actorID, actorRole, req.Count)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteCreated(w, codes)
}

// ListInviteCodes GET /api/staff/auth/invite-codes — 管理员分页查询邀请码（JWT + RequireAdmin）。
// 查询参数：page、page_size（pagination 统一解析，page_size 上限 100）。
func (h *AuthHandler) ListInviteCodes(w http.ResponseWriter, r *http.Request) {
	_, actorRole, _, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	params, err := pagination.Parse(r)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	codes, total, err := h.svc.ListInviteCodes(r.Context(), actorRole, params.Page, params.PageSize)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, pagination.NewResult(codes, total, params))
}

// LockAccount POST /api/staff/auth/accounts/{id}/lock — 管理员锁定账户（JWT + RequireAdmin）。
func (h *AuthHandler) LockAccount(w http.ResponseWriter, r *http.Request) {
	h.setAccountActive(w, r, false)
}

// UnlockAccount POST /api/staff/auth/accounts/{id}/unlock — 管理员解锁账户（JWT + RequireAdmin）。
func (h *AuthHandler) UnlockAccount(w http.ResponseWriter, r *http.Request) {
	h.setAccountActive(w, r, true)
}

// SoftDeleteAccount DELETE /api/staff/auth/accounts/{id} — 超级管理员软删除账户（JWT + RequireAdmin）。
// 仅 SUPER_ADMIN 可执行，路由层由 RequireAdmin 放行，service 层二次校验角色。
func (h *AuthHandler) SoftDeleteAccount(w http.ResponseWriter, r *http.Request) {
	actorID, actorRole, _, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetID <= 0 {
		response.WriteError(w, r, apperrors.BadRequest("AUTH_INVALID_ID", "账户 ID 无效"))
		return
	}
	if err := h.svc.SoftDeleteUser(r.Context(), actorID, actorRole, targetID); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, successResponse{Success: true})
}

// ResetAccountPassword POST /api/staff/auth/accounts/{id}/reset-password — 超级管理员重置用户密码（JWT + RequireAdmin）。
// 仅 SUPER_ADMIN 可执行，路由层由 RequireAdmin 放行，service 层二次校验角色。
func (h *AuthHandler) ResetAccountPassword(w http.ResponseWriter, r *http.Request) {
	actorID, actorRole, _, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetID <= 0 {
		response.WriteError(w, r, apperrors.BadRequest("AUTH_INVALID_ID", "账户 ID 无效"))
		return
	}
	var req resetPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if req.NewPassword == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "new_password 字段必填"))
		return
	}
	if err := h.svc.ResetUserPassword(r.Context(), actorID, actorRole, targetID, req.NewPassword); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, successResponse{Success: true})
}

// setAccountActive LockAccount/UnlockAccount 共用逻辑：解析路径 ID 与操作者身份，调用 service 切换 is_active。
func (h *AuthHandler) setAccountActive(w http.ResponseWriter, r *http.Request, active bool) {
	actorID, actorRole, actorDeptID, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetID <= 0 {
		response.WriteError(w, r, apperrors.BadRequest("AUTH_INVALID_ID", "账户 ID 无效"))
		return
	}
	if err := h.svc.SetAccountActive(r.Context(), actorID, actorRole, actorDeptID, targetID, active); err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, successResponse{Success: true})
}

// UpdateAccountDept PATCH /api/staff/auth/accounts/{id}/department — 修改账户主科室（JWT + RequireAdmin）。
// 权限收口在 service 层：超管可改任意；科室管理员仅本科室账户且只能改成本科室。
func (h *AuthHandler) UpdateAccountDept(w http.ResponseWriter, r *http.Request) {
	_, actorRole, actorDeptID, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetID <= 0 {
		response.WriteError(w, r, apperrors.BadRequest("AUTH_INVALID_ID", "账户 ID 无效"))
		return
	}
	var req updateDeptRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	account, err := h.svc.UpdateAccountDept(r.Context(), actorRole, actorDeptID, targetID, req.DeptID)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, account)
}

// UpdateAccountRole PATCH /api/staff/auth/accounts/{id}/role — 超级管理员修改账户角色（JWT + RequireAdmin）。
// 仅 SUPER_ADMIN 可执行，service 层二次校验角色并禁止修改自己。
func (h *AuthHandler) UpdateAccountRole(w http.ResponseWriter, r *http.Request) {
	actorID, actorRole, _, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetID <= 0 {
		response.WriteError(w, r, apperrors.BadRequest("AUTH_INVALID_ID", "账户 ID 无效"))
		return
	}
	var req updateRoleRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteError(w, r, err)
		return
	}
	if req.Role == "" {
		response.WriteError(w, r, apperrors.Validation("VALIDATION_MISSING", "role 字段必填"))
		return
	}
	account, err := h.svc.UpdateAccountRole(r.Context(), actorID, actorRole, targetID, req.Role)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, account)
}

// RestoreAccount POST /api/staff/auth/accounts/{id}/restore — 超级管理员恢复软删除账户（JWT + RequireAdmin）。
// 仅 SUPER_ADMIN 可执行，service 层二次校验角色。
func (h *AuthHandler) RestoreAccount(w http.ResponseWriter, r *http.Request) {
	actorID, actorRole, _, ok := currentIdentity(r)
	if !ok {
		response.WriteError(w, r, apperrors.Unauthorized("UNAUTHORIZED", "missing user identity"))
		return
	}
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetID <= 0 {
		response.WriteError(w, r, apperrors.BadRequest("AUTH_INVALID_ID", "账户 ID 无效"))
		return
	}
	account, err := h.svc.RestoreUser(r.Context(), actorID, actorRole, targetID)
	if err != nil {
		response.WriteError(w, r, err)
		return
	}
	response.WriteOK(w, account)
}

// currentIdentity 从 JWT 中间件注入的 ctx 提取操作者 (userID, role, deptID)。
// UserID 为 int64、UserRole 为 string、DeptID 为 int64（JWTAuth 写入，非字符串键不用 FromCtx 取 ID）。
func currentIdentity(r *http.Request) (userID int64, role string, deptID int64, ok bool) {
	userID, ok = r.Context().Value(contextkeys.UserID).(int64)
	if !ok || userID <= 0 {
		return 0, "", 0, false
	}
	role, _ = r.Context().Value(contextkeys.UserRole).(string)
	deptID, _ = r.Context().Value(contextkeys.DeptID).(int64)
	return userID, role, deptID, true
}

const maxAuthBodyBytes = 1 << 20

// decodeJSON 解析请求体为 v。空体 → 422 VALIDATION_MISSING；格式错误 → 422 VALIDATION_INVALID。
// 限制请求体 1MB，避免恶意大报文耗尽内存（auth 请求体极小，1MB 已远超合理上限）。
// 严格模式：拒绝未知字段，与 wiki/config handler 一致，避免客户端笔误被静默忽略。
func decodeJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxAuthBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return apperrors.Validation("VALIDATION_MISSING", "请求体不能为空")
		}
		return apperrors.Validation("VALIDATION_INVALID", "请求体格式错误")
	}
	return nil
}
