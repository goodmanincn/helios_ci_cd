package model

// Secret 对应 secrets 表 — 密钥保险箱
type Secret struct {
	Base
	Scope            string `gorm:"column:scope;size:32;not null;index:idx_secrets_scope,priority:1"  json:"scope"` // org/project/pipeline
	ScopeID          int64  `gorm:"column:scope_id;not null;index:idx_secrets_scope,priority:2"       json:"scope_id"`
	Name             string `gorm:"column:name;size:128;not null"                                     json:"name"`
	Type             string `gorm:"column:type;size:32;not null"                                      json:"type"`
	Description      string `gorm:"column:description"                                                json:"description,omitempty"`
	EncryptedValue   []byte `gorm:"column:encrypted_value;not null"                                   json:"-"`
	EncryptionKEKID  string `gorm:"column:encryption_kek_id;size:128"                                 json:"-"`
	CreatedBy        *int64 `gorm:"column:created_by"                                                 json:"created_by,omitempty"`
}

func (Secret) TableName() string { return "secrets" }
