package types

import "time"

// ResearchProposal is a reviewable candidate, never a second authoritative copy
// of a document. The native wiki page and its revision history remain canonical.
type ResearchProposal struct {
	ID              string      `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TenantID        uint64      `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID string      `json:"knowledge_base_id" gorm:"index"`
	PageID          string      `json:"page_id" gorm:"index"`
	BaseVersion     int         `json:"base_version"`
	BaseContent     string      `json:"base_content" gorm:"type:text"`
	Content         string      `json:"content" gorm:"type:text"`
	Instruction     string      `json:"instruction" gorm:"type:text"`
	ModelID         string      `json:"model_id"`
	SourceRefs      StringArray `json:"source_refs" gorm:"type:json"`
	SourceVersions  JSON        `json:"source_versions" gorm:"type:json"`
	WritingRules    JSON        `json:"writing_rules" gorm:"type:json"`
	Status          string      `json:"status" gorm:"index"`
	CreatedBy       string      `json:"created_by"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type ResearchExport struct {
	ID              string `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TenantID        uint64 `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID string `json:"knowledge_base_id" gorm:"index"`
	PageID          string `json:"page_id"`
	Version         int    `json:"version"`
	Format          string `json:"format"`
	Status          string `json:"status"`
	Error           string `json:"error,omitempty" gorm:"type:text"`
	FilePath        string `json:"-" gorm:"type:text"`
	FileName        string `json:"file_name"`
	CreatedBy       string `json:"created_by"`
	// Exact membership snapshot prevents an old archive from exposing a
	// document later moved to a more restricted project or deleted.
	SourceIDs StringArray `json:"-" gorm:"type:json"`
	PageIDs   StringArray `json:"-" gorm:"type:json"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type ResearchTemplate struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func ResearchTemplates() []ResearchTemplate {
	return []ResearchTemplate{
		{ID: "one-pager", Name: "项目一页纸", Content: "# 项目一页纸\n\n## 核心判断\n\n## 公司与团队\n\n## 技术与产品\n\n## 市场与竞争\n\n## 证据与不确定性\n\n## 下一步与负责人\n"},
		{ID: "brief", Name: "研究简报", Content: "# 研究简报\n\n## 研究问题\n\n## 主要发现\n\n## 证据与相互矛盾的信息\n\n## 对投资判断的影响\n\n## 待验证事项\n"},
		{ID: "comparison", Name: "公司对比", Content: "# 公司对比\n\n## 对比范围与口径\n\n| 维度 | 公司 A | 公司 B | 证据 |\n| --- | --- | --- | --- |\n| 技术 | 待补充 | 待补充 | 待补充 |\n\n## 差异与风险\n\n## 信息缺口\n"},
		{ID: "diligence", Name: "尽调问题清单", Content: "# 尽调问题清单\n\n## 技术与研发\n\n## 产品与客户\n\n## 团队与组织\n\n## 商业模式与财务\n\n## 关键风险\n\n| 待验证问题 | 已有证据 | 负责人 | 状态 |\n| --- | --- | --- | --- |\n| 待补充 | 待补充 | 待分配 | 未开始 |\n"},
	}
}
