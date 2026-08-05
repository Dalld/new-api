package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/affiliate_setting"
	"github.com/shopspring/decimal"
)

func commissionUnitPrice(baseUnitPrice float64, group string) decimal.Decimal {
	groupRatio := common.GetTopupGroupRatio(group)
	if groupRatio == 0 {
		groupRatio = 1
	}
	return decimal.NewFromFloat(baseUnitPrice).Mul(decimal.NewFromFloat(groupRatio))
}

func freezeCommissionBaseQuota(paidMoney float64, unitPrice decimal.Decimal) (int64, error) {
	paid := decimal.NewFromFloat(paidMoney).Round(2)
	if paid.LessThanOrEqual(decimal.Zero) || unitPrice.LessThanOrEqual(decimal.Zero) {
		return 0, errors.New("invalid commission paid amount or unit price")
	}
	base := paid.Div(unitPrice).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	quota, clamp := common.QuotaFromDecimalChecked(base)
	if clamp != nil {
		return 0, clamp
	}
	if quota < 0 {
		return 0, errors.New("commission base quota must not be negative")
	}
	return int64(quota), nil
}

func freezeCommissionSnapshot(topUp *model.TopUp, user *model.User, baseQuota int64) error {
	if topUp == nil || user == nil || topUp.UserId <= 0 || topUp.UserId != user.Id || baseQuota < 0 {
		return errors.New("invalid commission snapshot input")
	}

	rate := affiliate_setting.GetRate()
	if err := affiliate_setting.ValidateRate(rate); err != nil {
		return err
	}

	topUp.CommissionBaseQuota = baseQuota
	topUp.CommissionInviterID = user.InviterId
	topUp.CommissionRate = decimal.NewFromFloat(rate).String()
	topUp.CommissionEligible = user.InviterId > 0 && baseQuota > 0 && rate > 0
	return nil
}
