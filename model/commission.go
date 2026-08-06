package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const commissionQueryMaxPageSize = 100

// CommissionRecord is the immutable audit record for one settled top-up.
// TopUpID, rather than a provider order number, is the idempotency key.
type CommissionRecord struct {
	ID                  int    `json:"id" gorm:"primaryKey"`
	TopUpID             int    `json:"top_up_id" gorm:"not null;uniqueIndex:ux_commission_records_top_up_id"`
	OrderNo             string `json:"order_no" gorm:"type:varchar(255);not null;index"`
	PaymentProvider     string `json:"payment_provider" gorm:"type:varchar(50);not null"`
	PaidMoney           string `json:"paid_money" gorm:"type:varchar(64);not null"`
	InviterID           int    `json:"inviter_id" gorm:"not null;index"`
	InviteeID           int    `json:"invitee_id" gorm:"not null;index"`
	CommissionBaseQuota int64  `json:"commission_base_quota" gorm:"not null;default:0"`
	CommissionRate      string `json:"commission_rate" gorm:"type:varchar(64);not null"`
	CommissionQuota     int64  `json:"commission_quota" gorm:"not null;default:0"`
	CreatedAt           int64  `json:"created_at" gorm:"not null;autoCreateTime"`
}

func (CommissionRecord) TableName() string {
	return "commission_records"
}

// InsertCommissionRecord atomically claims a top-up's commission. A false
// inserted result means another settlement already claimed the same TopUpID.
func InsertCommissionRecord(tx *gorm.DB, record *CommissionRecord) (inserted bool, err error) {
	if tx == nil {
		tx = DB
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "top_up_id"}},
		DoNothing: true,
	}).Create(record)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// CreateCommissionRecord is kept as a transaction-friendly synonym for
// callers that use Create naming for model inserts.
func CreateCommissionRecord(tx *gorm.DB, record *CommissionRecord) (bool, error) {
	return InsertCommissionRecord(tx, record)
}

type AffiliateRelation struct {
	InviterID           int    `json:"inviter_id" gorm:"column:inviter_id"`
	Username            string `json:"username" gorm:"column:username"`
	DisplayName         string `json:"display_name" gorm:"column:display_name"`
	Email               string `json:"email" gorm:"column:email"`
	AffCode             string `json:"aff_code" gorm:"column:aff_code"`
	AffQuota            int    `json:"aff_quota" gorm:"column:aff_quota"`
	AffHistoryQuota     int    `json:"aff_history_quota" gorm:"column:aff_history_quota"`
	InviteeCount        int64  `json:"invitee_count" gorm:"column:invitee_count"`
	CommissionBaseQuota int64  `json:"commission_base_quota" gorm:"column:commission_base_quota"`
	CommissionQuota     int64  `json:"commission_quota" gorm:"column:commission_quota"`
	CreatedAt           int64  `json:"created_at" gorm:"column:created_at"`
}

type CommissionRecordDetail struct {
	CommissionRecord
	InviterUsername string `json:"inviter_username" gorm:"column:inviter_username"`
	InviteeUsername string `json:"invitee_username" gorm:"column:invitee_username"`
}

type SelfInvitee struct {
	ID             int    `json:"id" gorm:"column:id"`
	InviterID      int    `json:"inviter_id" gorm:"column:inviter_id"`
	Username       string `json:"-" gorm:"column:username"`
	MaskedUsername string `json:"masked_username" gorm:"-"`
	DisplayName    string `json:"-" gorm:"column:display_name"`
	CreatedAt      int64  `json:"created_at" gorm:"column:created_at"`
}

type SelfCommissionRecord struct {
	ID                    int    `json:"id" gorm:"column:id"`
	TopUpID               int    `json:"top_up_id" gorm:"column:top_up_id"`
	OrderNo               string `json:"order_no" gorm:"column:order_no"`
	PaymentProvider       string `json:"payment_provider" gorm:"column:payment_provider"`
	InviterID             int    `json:"inviter_id" gorm:"column:inviter_id"`
	InviteeID             int    `json:"invitee_id" gorm:"column:invitee_id"`
	InviteeUsername       string `json:"-" gorm:"column:invitee_username"`
	MaskedInviteeUsername string `json:"invitee_username" gorm:"-"`
	CommissionBaseQuota   int64  `json:"commission_base_quota" gorm:"column:commission_base_quota"`
	CommissionRate        string `json:"commission_rate" gorm:"column:commission_rate"`
	CommissionQuota       int64  `json:"commission_quota" gorm:"column:commission_quota"`
	CreatedAt             int64  `json:"created_at" gorm:"column:created_at"`
}

func maskAffiliateUsername(username string) string {
	runes := []rune(username)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 2 {
		return "**"
	}
	if len(runes) <= 5 {
		return string(runes[0]) + "**" + string(runes[len(runes)-1])
	}
	return string(runes[:3]) + "**" + string(runes[len(runes)-2:])
}

func maskSelfInvitees(invitees []SelfInvitee) {
	for index := range invitees {
		invitees[index].MaskedUsername = maskAffiliateUsername(invitees[index].Username)
	}
}

func maskSelfCommissionRecords(records []SelfCommissionRecord) {
	for index := range records {
		records[index].MaskedInviteeUsername = maskAffiliateUsername(records[index].InviteeUsername)
	}
}

func normalizeCommissionPage(startIdx, pageSize int) (int, int) {
	if startIdx < 0 {
		startIdx = 0
	}
	if pageSize <= 0 || pageSize > commissionQueryMaxPageSize {
		pageSize = commissionQueryMaxPageSize
	}
	return startIdx, pageSize
}

func commissionLikePattern(keyword string) string {
	replacer := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + replacer.Replace(strings.TrimSpace(keyword)) + "%"
}

func applyCommissionSearch(query *gorm.DB, keyword string, columns ...string) *gorm.DB {
	if strings.TrimSpace(keyword) == "" || len(columns) == 0 {
		return query
	}
	conditions := make([]string, 0, len(columns))
	args := make([]interface{}, 0, len(columns))
	pattern := commissionLikePattern(keyword)
	for _, column := range columns {
		conditions = append(conditions, column+" LIKE ? ESCAPE '!'")
		args = append(args, pattern)
	}
	return query.Where("("+strings.Join(conditions, " OR ")+")", args...)
}

// GetAffiliateRelations returns all inviters discovered from actual user
// relationships or commission records. Aggregates avoid trusting users.aff_count.
func GetAffiliateRelations(keyword string, startIdx int, pageSize int) (relations []AffiliateRelation, total int64, err error) {
	startIdx, pageSize = normalizeCommissionPage(startIdx, pageSize)
	relationTotals := DB.Table("users").
		Select("inviter_id, COUNT(*) AS invitee_count").
		Where("inviter_id <> ?", 0).
		Group("inviter_id")
	commissionTotals := DB.Table("commission_records").
		Select("inviter_id, SUM(commission_base_quota) AS commission_base_quota, SUM(commission_quota) AS commission_quota").
		Group("inviter_id")

	query := DB.Table("users AS inviter").
		Joins("LEFT JOIN (?) AS relation_totals ON relation_totals.inviter_id = inviter.id", relationTotals).
		Joins("LEFT JOIN (?) AS commission_totals ON commission_totals.inviter_id = inviter.id", commissionTotals).
		Where("relation_totals.inviter_id IS NOT NULL OR commission_totals.inviter_id IS NOT NULL")
	query = applyCommissionSearch(query, keyword, "inviter.username", "inviter.display_name", "inviter.email", "inviter.aff_code")

	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Select(
		"inviter.id AS inviter_id, inviter.username, inviter.display_name, inviter.email, inviter.aff_code, " +
			"inviter.aff_quota, inviter.aff_history AS aff_history_quota, inviter.created_at, " +
			"COALESCE(relation_totals.invitee_count, 0) AS invitee_count, " +
			"COALESCE(commission_totals.commission_base_quota, 0) AS commission_base_quota, " +
			"COALESCE(commission_totals.commission_quota, 0) AS commission_quota").
		Order("commission_quota DESC").
		Order("inviter.id DESC").
		Offset(startIdx).
		Limit(pageSize).
		Scan(&relations).Error
	if err == nil && relations == nil {
		relations = make([]AffiliateRelation, 0)
	}
	return relations, total, err
}

// GetCommissionRecords returns the administrator's global audit view.
func GetCommissionRecords(keyword string, startIdx int, pageSize int) (records []CommissionRecordDetail, total int64, err error) {
	startIdx, pageSize = normalizeCommissionPage(startIdx, pageSize)
	query := DB.Table("commission_records").
		Joins("LEFT JOIN users AS inviter ON inviter.id = commission_records.inviter_id").
		Joins("LEFT JOIN users AS invitee ON invitee.id = commission_records.invitee_id")
	query = applyCommissionSearch(query, keyword, "commission_records.order_no", "inviter.username", "invitee.username")
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Select("commission_records.*, inviter.username AS inviter_username, invitee.username AS invitee_username").
		Order("commission_records.created_at DESC").
		Order("commission_records.id DESC").
		Offset(startIdx).
		Limit(pageSize).
		Scan(&records).Error
	if err == nil && records == nil {
		records = make([]CommissionRecordDetail, 0)
	}
	return records, total, err
}

// GetSelfInvitees always scopes the result to the authenticated inviter ID.
func GetSelfInvitees(inviterID int, keyword string, startIdx int, pageSize int) (invitees []SelfInvitee, total int64, err error) {
	startIdx, pageSize = normalizeCommissionPage(startIdx, pageSize)
	query := DB.Table("users AS invitee").Where("invitee.inviter_id = ?", inviterID)
	query = applyCommissionSearch(query, keyword, "invitee.username", "invitee.display_name")
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Select("invitee.id, invitee.inviter_id, invitee.username, invitee.display_name, invitee.created_at").
		Order("invitee.created_at DESC").
		Order("invitee.id DESC").
		Offset(startIdx).
		Limit(pageSize).
		Scan(&invitees).Error
	if err == nil && invitees == nil {
		invitees = make([]SelfInvitee, 0)
	}
	maskSelfInvitees(invitees)
	return invitees, total, err
}

// GetSelfCommissionRecords always scopes the result to the authenticated inviter ID.
func GetSelfCommissionRecords(inviterID int, keyword string, startIdx int, pageSize int) (records []SelfCommissionRecord, total int64, err error) {
	startIdx, pageSize = normalizeCommissionPage(startIdx, pageSize)
	query := DB.Table("commission_records").
		Joins("LEFT JOIN users AS invitee ON invitee.id = commission_records.invitee_id").
		Where("commission_records.inviter_id = ?", inviterID)
	query = applyCommissionSearch(query, keyword, "commission_records.order_no", "invitee.username")
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Select(
		"commission_records.id, commission_records.top_up_id, commission_records.order_no, " +
			"commission_records.payment_provider, commission_records.inviter_id, commission_records.invitee_id, " +
			"commission_records.commission_base_quota, commission_records.commission_rate, " +
			"commission_records.commission_quota, commission_records.created_at, invitee.username AS invitee_username").
		Order("commission_records.created_at DESC").
		Order("commission_records.id DESC").
		Offset(startIdx).
		Limit(pageSize).
		Scan(&records).Error
	if err == nil && records == nil {
		records = make([]SelfCommissionRecord, 0)
	}
	maskSelfCommissionRecords(records)
	return records, total, err
}

// GetSelfInviteeCount counts the authoritative inviter relationship instead
// of relying on the denormalized users.aff_count field.
func GetSelfInviteeCount(inviterID int) (total int64, err error) {
	err = DB.Model(&User{}).Where("inviter_id = ?", inviterID).Count(&total).Error
	return total, err
}

func GetSelfRechargeTotal(inviterID int) (total int64, err error) {
	err = DB.Table("commission_records").
		Where("inviter_id = ?", inviterID).
		Select("COALESCE(SUM(commission_base_quota), 0)").
		Scan(&total).Error
	return total, err
}

func GetAffiliateRelationsPage(keyword string, pageInfo *common.PageInfo) ([]AffiliateRelation, int64, error) {
	return GetAffiliateRelations(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
}

func GetCommissionRecordsPage(keyword string, pageInfo *common.PageInfo) ([]CommissionRecordDetail, int64, error) {
	return GetCommissionRecords(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
}

func GetSelfInviteesPage(inviterID int, keyword string, pageInfo *common.PageInfo) ([]SelfInvitee, int64, error) {
	return GetSelfInvitees(inviterID, keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
}

func GetSelfCommissionRecordsPage(inviterID int, keyword string, pageInfo *common.PageInfo) ([]SelfCommissionRecord, int64, error) {
	return GetSelfCommissionRecords(inviterID, keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
}
