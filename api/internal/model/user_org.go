package model

import "time"

// User 对应 users 表
type User struct {
	Base
	Username     string     `gorm:"column:username;size:64;uniqueIndex;not null" json:"username"`
	Email        string     `gorm:"column:email;size:255;uniqueIndex;not null"   json:"email"`
	PasswordHash string     `gorm:"column:password_hash;size:255"                json:"-"`
	OIDCSubject  string     `gorm:"column:oidc_subject;size:255"                 json:"oidc_subject,omitempty"`
	DisplayName  string     `gorm:"column:display_name;size:128"                 json:"display_name,omitempty"`
	AvatarURL    string     `gorm:"column:avatar_url"                            json:"avatar_url,omitempty"`
	IsActive     bool       `gorm:"column:is_active;default:true"                json:"is_active"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"                         json:"last_login_at,omitempty"`
}

func (User) TableName() string { return "users" }

// Organization 对应 organizations 表
type Organization struct {
	Base
	Name        string `gorm:"column:name;size:128;not null"          json:"name"`
	Slug        string `gorm:"column:slug;size:64;uniqueIndex;not null" json:"slug"`
	OwnerID     *int64 `gorm:"column:owner_id"                        json:"owner_id,omitempty"`
	Description string `gorm:"column:description"                     json:"description,omitempty"`

	Owner *User `gorm:"foreignKey:OwnerID" json:"-"`
}

func (Organization) TableName() string { return "organizations" }

// OrgMember 对应 org_members 表 (复合主键)
type OrgMember struct {
	OrgID     int64     `gorm:"column:org_id;primaryKey"      json:"org_id"`
	UserID    int64     `gorm:"column:user_id;primaryKey"     json:"user_id"`
	Role      string    `gorm:"column:role;size:32;not null"  json:"role"` // owner / admin / member
	CreatedAt time.Time `gorm:"column:created_at;not null"    json:"created_at"`
}

func (OrgMember) TableName() string { return "org_members" }
