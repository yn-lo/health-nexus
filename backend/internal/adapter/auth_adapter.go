// Package adapter 提供 auth 域适配器。
package adapter

import (
	"context"
	"errors"
	"fmt"

	authrepo "health-nexus/internal/domain/auth/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
)

// AuthApplicantRoleResolver 实现 wikiservice.ApplicantRoleResolver。
// 桥接 auth 域 UserRepo.GetByID 到 wiki 域消费者接口，用于 D-MED-08 单超管自审豁免。
type AuthApplicantRoleResolver struct {
	repo *authrepo.UserRepo
}

// NewAuthApplicantRoleResolver 构造适配器。
func NewAuthApplicantRoleResolver(repo *authrepo.UserRepo) *AuthApplicantRoleResolver {
	return &AuthApplicantRoleResolver{repo: repo}
}

// GetRoleByUserID 返回用户角色；用户不存在返回 "" + error。
func (r *AuthApplicantRoleResolver) GetRoleByUserID(ctx context.Context, userID int64) (string, error) {
	u, err := r.repo.GetByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get user role: %w", err)
	}
	if u == nil {
		return "", errors.New("user not found")
	}
	return u.Role, nil
}

// 编译期断言：确保适配器实现接口。
var _ wikiservice.ApplicantRoleResolver = (*AuthApplicantRoleResolver)(nil)
