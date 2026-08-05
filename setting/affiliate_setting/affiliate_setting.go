package affiliate_setting

import (
	"fmt"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ConfigName  = "affiliate_setting"
	ConfigField = "commission_rate"
	OptionKey   = ConfigName + "." + ConfigField
)

// AffiliateSetting is persisted under affiliate_setting.commission_rate.
type AffiliateSetting struct {
	CommissionRate float64 `json:"commission_rate"`
}

var (
	affiliateSetting   = AffiliateSetting{}
	affiliateSettingMu sync.RWMutex
)

func init() {
	config.GlobalConfig.Register(ConfigName, &affiliateSetting)
}

// ValidateRate accepts only finite decimal fractions from zero through one.
func ValidateRate(rate float64) error {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 1 {
		return fmt.Errorf("commission rate must be finite and between 0 and 1")
	}
	return nil
}

// GetAffiliateSetting returns the registered setting. An invalid persisted
// value is normalized to the disabled state before it can be used.
func GetAffiliateSetting() *AffiliateSetting {
	affiliateSettingMu.RLock()
	current := affiliateSetting
	affiliateSettingMu.RUnlock()
	if ValidateRate(current.CommissionRate) != nil {
		current.CommissionRate = 0
	}
	return &current
}

func GetRate() float64 {
	return GetAffiliateSetting().CommissionRate
}

// SetRate validates before mutation so a rejected administrator update does
// not replace the last valid setting.
func SetRate(rate float64) error {
	if err := ValidateRate(rate); err != nil {
		return err
	}
	affiliateSettingMu.Lock()
	affiliateSetting.CommissionRate = rate
	affiliateSettingMu.Unlock()
	return nil
}

// UnmarshalJSON validates settings loaded through the project's JSON helpers.
func (setting *AffiliateSetting) UnmarshalJSON(data []byte) error {
	type affiliateSettingJSON AffiliateSetting
	var decoded affiliateSettingJSON
	if err := common.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("invalid affiliate setting: %w", err)
	}
	if err := ValidateRate(decoded.CommissionRate); err != nil {
		return err
	}
	*setting = AffiliateSetting(decoded)
	return nil
}
