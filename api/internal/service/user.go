// Package service 业务服务层 — 在 model 层之上,聚合多模型操作 + 事务 + 权限。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/helios-cicd/helios/api/internal/model"
)

// BcryptCost 12 是 spec 默认 — 比对 ~250ms,可接受。
const BcryptCost = 12

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrUserInactive        = errors.New("user inactive")
	ErrUsernameTaken       = errors.New("username already taken")
	ErrEmailTaken          = errors.New("email already taken")
	ErrPasswordTooShort    = errors.New("password too short (min 8)")
)

// UserService 提供用户增/查/认证。
type UserService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) *UserService {
	return &UserService{db: db}
}

// CreateUserInput 创建用户入参
type CreateUserInput struct {
	Username    string
	Email       string
	Password    string
	DisplayName string
}

// Create 创建一个本地账户用户 (password_hash 必填)。
func (s *UserService) Create(ctx context.Context, in CreateUserInput) (*model.User, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if len(in.Password) < 8 {
		return nil, ErrPasswordTooShort
	}

	// 唯一性预检 (避免依赖 PG error code 解析)
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", in.Username).Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt > 0 {
		return nil, ErrUsernameTaken
	}
	if err := s.db.WithContext(ctx).Model(&model.User{}).Where("email = ?", in.Email).Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt > 0 {
		return nil, ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), BcryptCost)
	if err != nil {
		return nil, err
	}
	u := &model.User{
		Username:     in.Username,
		Email:        in.Email,
		PasswordHash: string(hash),
		DisplayName:  in.DisplayName,
		IsActive:     true,
	}
	if err := s.db.WithContext(ctx).Create(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

// GetByID 按 id 查 (软删除被自动过滤)。
func (s *UserService) GetByID(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	err := s.db.WithContext(ctx).First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByUsername 按用户名查。
func (s *UserService) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// VerifyPassword 验证一个用户的密码。返回 (user, nil) 表示成功。
// 失败统一返回 ErrInvalidCredentials,避免暴露 "用户不存在" vs "密码错" 给攻击者。
func (s *UserService) VerifyPassword(ctx context.Context, username, password string) (*model.User, error) {
	u, err := s.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// 仍做一次 bcrypt 比对消耗时间,防止 timing attack
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$DUMMYDUMMYDUMMYDUMMYDU.dummydummydummydummydummydummydumm"), []byte(password))
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !u.IsActive {
		return nil, ErrUserInactive
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

// TouchLastLogin 更新最后登录时间 (登录成功后异步调用,失败不阻塞登录)。
func (s *UserService) TouchLastLogin(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ?", id).
		Update("last_login_at", now).Error
}
