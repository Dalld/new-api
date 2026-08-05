package model

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id                  int     `json:"id"`
	UserId              int     `json:"user_id" gorm:"index"`
	Amount              int64   `json:"amount"`
	Money               float64 `json:"money"`
	TradeNo             string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod       string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider     string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CommissionBaseQuota int64   `json:"commission_base_quota" gorm:"not null;default:0"`
	CommissionEligible  bool    `json:"-" gorm:"not null;default:false"`
	CommissionInviterID int     `json:"-" gorm:"not null;default:0"`
	CommissionRate      string  `json:"-" gorm:"type:varchar(32);not null;default:'0'"`
	CreateTime          int64   `json:"create_time"`
	CompleteTime        int64   `json:"complete_time"`
	Status              string  `json:"status"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
	PaymentProviderAdmin        = "admin"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
	ErrTopUpQuotaInvalid     = errors.New("topup quota invalid")
	ErrSettlementMetadata    = errors.New("settlement user metadata invalid")
	ErrCommissionSnapshot    = errors.New("commission snapshot invalid")
)

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

// TopUpSettlementRequest contains only provider-verified settlement data.
// SettlementPaymentProvider overrides the commission audit provider for
// administrative completion; normal callbacks leave it empty.
type TopUpSettlementRequest struct {
	TradeNo                   string
	ExpectedPaymentProvider   string
	SettlementPaymentProvider string
	VerifiedPaymentMethod     string
	UpdateUser                map[string]interface{}
}

// TopUpSettlementResult lets controllers perform logging and cache work after
// commit without repeating any database increment.
type TopUpSettlementResult struct {
	TopUpID         int
	TradeNo         string
	UserID          int
	PaymentMethod   string
	PaymentProvider string
	PaidMoney       string
	QuotaAdded      int64
	CommissionQuota int64
	AlreadySettled  bool
}

func newTopUpSettlementResult(topUp *TopUp) *TopUpSettlementResult {
	paidMoney := ""
	if !math.IsNaN(topUp.Money) && !math.IsInf(topUp.Money, 0) {
		paidMoney = decimal.NewFromFloat(topUp.Money).StringFixed(2)
	}
	return &TopUpSettlementResult{
		TopUpID:         topUp.Id,
		TradeNo:         topUp.TradeNo,
		UserID:          topUp.UserId,
		PaymentMethod:   topUp.PaymentMethod,
		PaymentProvider: topUp.PaymentProvider,
		PaidMoney:       paidMoney,
	}
}

func settlementQuota(topUp *TopUp) (int64, error) {
	if topUp == nil || topUp.Amount < 0 || math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) {
		return 0, ErrTopUpQuotaInvalid
	}

	var quotaDecimal decimal.Decimal
	if topUp.PaymentProvider == PaymentProviderCreem {
		quotaDecimal = decimal.NewFromInt(topUp.Amount)
	} else {
		if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
			return 0, ErrTopUpQuotaInvalid
		}
		if topUp.PaymentProvider == PaymentProviderStripe {
			quotaDecimal = decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
		} else {
			quotaDecimal = decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
		}
	}

	quota, clamp := common.QuotaFromDecimalChecked(quotaDecimal)
	if clamp != nil || quota <= 0 {
		return 0, ErrTopUpQuotaInvalid
	}
	return int64(quota), nil
}

func settlementUserUpdates(current *User, metadata map[string]interface{}, quota int64) (map[string]interface{}, error) {
	updates := map[string]interface{}{"quota": gorm.Expr("quota + ?", quota)}
	for key, value := range metadata {
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: %s must be a string", ErrSettlementMetadata, key)
		}
		switch key {
		case "stripe_customer":
			updates[key] = text
		case "email":
			if current.Email == "" && text != "" {
				updates[key] = text
			}
		default:
			return nil, fmt.Errorf("%w: unsupported field %s", ErrSettlementMetadata, key)
		}
	}
	return updates, nil
}

// SettleTopUp atomically settles one direct top-up and, when eligible, its
// affiliate commission. Provider callbacks must do signature and amount
// verification before calling this function.
func SettleTopUp(request TopUpSettlementRequest) (*TopUpSettlementResult, error) {
	if request.TradeNo == "" {
		return nil, ErrTopUpNotFound
	}

	var result *TopUpSettlementResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		if err := lockForUpdate(tx).Where("trade_no = ?", request.TradeNo).First(&topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}
		if request.ExpectedPaymentProvider != "" && topUp.PaymentProvider != request.ExpectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}

		result = newTopUpSettlementResult(&topUp)
		if topUp.Status == common.TopUpStatusSuccess {
			result.AlreadySettled = true
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if request.VerifiedPaymentMethod != "" {
			topUp.PaymentMethod = request.VerifiedPaymentMethod
			result.PaymentMethod = request.VerifiedPaymentMethod
		}

		quota, err := settlementQuota(&topUp)
		if err != nil {
			return err
		}
		var invitee User
		if err := tx.Where("id = ?", topUp.UserId).First(&invitee).Error; err != nil {
			return err
		}
		userUpdates, err := settlementUserUpdates(&invitee, request.UpdateUser, quota)
		if err != nil {
			return err
		}

		now := common.GetTimestamp()
		orderUpdates := map[string]interface{}{
			"status":        common.TopUpStatusSuccess,
			"complete_time": now,
		}
		if request.VerifiedPaymentMethod != "" {
			orderUpdates["payment_method"] = request.VerifiedPaymentMethod
		}
		orderUpdate := tx.Model(&TopUp{}).
			Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
			Updates(orderUpdates)
		if orderUpdate.Error != nil {
			return orderUpdate.Error
		}
		if orderUpdate.RowsAffected != 1 {
			return ErrTopUpStatusInvalid
		}
		userUpdate := tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(userUpdates)
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}

		result.QuotaAdded = quota
		if !topUp.CommissionEligible {
			return nil
		}
		if topUp.CommissionInviterID <= 0 || topUp.CommissionBaseQuota <= 0 {
			return ErrCommissionSnapshot
		}
		rateDecimal, err := decimal.NewFromString(topUp.CommissionRate)
		if err != nil || rateDecimal.LessThanOrEqual(decimal.Zero) || rateDecimal.GreaterThan(decimal.NewFromInt(1)) {
			return ErrCommissionSnapshot
		}
		commission, clamp := common.QuotaFromDecimalChecked(decimal.NewFromInt(topUp.CommissionBaseQuota).Mul(rateDecimal))
		if clamp != nil {
			return clamp
		}
		if commission <= 0 {
			return nil
		}

		auditProvider := topUp.PaymentProvider
		if request.SettlementPaymentProvider != "" {
			auditProvider = request.SettlementPaymentProvider
		}
		record := CommissionRecord{
			TopUpID:             topUp.Id,
			OrderNo:             topUp.TradeNo,
			PaymentProvider:     auditProvider,
			PaidMoney:           result.PaidMoney,
			InviterID:           topUp.CommissionInviterID,
			InviteeID:           invitee.Id,
			CommissionBaseQuota: topUp.CommissionBaseQuota,
			CommissionRate:      rateDecimal.String(),
			CommissionQuota:     int64(commission),
			CreatedAt:           now,
		}
		inserted, err := InsertCommissionRecord(tx, &record)
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}
		commissionUpdate := tx.Model(&User{}).Where("id = ?", topUp.CommissionInviterID).Updates(map[string]interface{}{
			"aff_quota":   gorm.Expr("aff_quota + ?", commission),
			"aff_history": gorm.Expr("aff_history + ?", commission),
		})
		if commissionUpdate.Error != nil {
			return commissionUpdate.Error
		}
		if commissionUpdate.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		result.CommissionQuota = int64(commission)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	result, err := SettleTopUp(TopUpSettlementRequest{
		TradeNo: referenceId, ExpectedPaymentProvider: PaymentProviderStripe,
		UpdateUser: map[string]interface{}{"stripe_customer": customerId},
	})
	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadySettled {
		RecordTopupLog(result.UserID, fmt.Sprintf("使用在线充值成功，充值额度: %v，支付金额：%s", logger.FormatQuota(int(result.QuotaAdded)), result.PaidMoney), callerIp, result.PaymentMethod, PaymentMethodStripe)
	}
	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	result, err := SettleTopUp(TopUpSettlementRequest{
		TradeNo: tradeNo, SettlementPaymentProvider: PaymentProviderAdmin,
	})
	if err != nil {
		return err
	}
	if !result.AlreadySettled {
		RecordTopupLog(result.UserID, fmt.Sprintf("管理员补单成功，充值额度: %v，支付金额：%s", logger.FormatQuota(int(result.QuotaAdded)), result.PaidMoney), callerIp, result.PaymentMethod, PaymentProviderAdmin)
	}
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	_ = customerName
	result, err := SettleTopUp(TopUpSettlementRequest{
		TradeNo: referenceId, ExpectedPaymentProvider: PaymentProviderCreem,
		UpdateUser: map[string]interface{}{"email": customerEmail},
	})
	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadySettled {
		RecordTopupLog(result.UserID, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%s", result.QuotaAdded, result.PaidMoney), callerIp, result.PaymentMethod, PaymentMethodCreem)
	}
	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: tradeNo, ExpectedPaymentProvider: PaymentProviderWaffo})
	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadySettled {
		RecordTopupLog(result.UserID, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %s", logger.FormatQuota(int(result.QuotaAdded)), result.PaidMoney), callerIp, result.PaymentMethod, PaymentMethodWaffo)
	}
	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	result, err := SettleTopUp(TopUpSettlementRequest{TradeNo: tradeNo, ExpectedPaymentProvider: PaymentProviderWaffoPancake})
	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadySettled {
		RecordLog(result.UserID, LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %s", logger.FormatQuota(int(result.QuotaAdded)), result.PaidMoney))
	}
	return nil
}
