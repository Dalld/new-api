package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/affiliate_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateOptionValueRejectsInvalidAffiliateCommissionRate(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "not a number", value: "not-a-rate"},
		{name: "negative", value: "-0.01"},
		{name: "above one", value: "1.01"},
		{name: "nan", value: "NaN"},
		{name: "infinity", value: "Inf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptionValue(affiliate_setting.OptionKey, tt.value)
			require.Error(t, err)
		})
	}

	require.NoError(t, validateOptionValue(affiliate_setting.OptionKey, "0.25"))
	require.NoError(t, validateOptionValue(affiliate_setting.OptionKey, "1"))
}

func TestValidateOptionValueRejectsInvalidGroupProbeConfig(t *testing.T) {
	valid := `{"enabled":true,"interval_minutes":10,"retention_days":7,"timeout_seconds":45,"groups":[{"group":"codex","display_name":"Codex","model":"gpt-5.5","public":true}]}`
	require.NoError(t, validateOptionValue("group_probe_setting.config", valid))

	invalid := []string{
		`{"enabled":true,"interval_minutes":4,"retention_days":7,"timeout_seconds":45,"groups":[]}`,
		`{"enabled":true,"interval_minutes":10,"retention_days":7,"timeout_seconds":45,"groups":[{"group":"codex","display_name":"Codex","model":"","public":true}]}`,
		`{"enabled":true,"interval_minutes":10,"retention_days":7,"timeout_seconds":45,"groups":[{"group":"codex","display_name":"Codex","model":"gpt-5.5"},{"group":"codex","display_name":"Other","model":"gpt-5.5"}]}`,
		`not-json`,
	}
	for _, value := range invalid {
		assert.Error(t, validateOptionValue("group_probe_setting.config", value))
	}
}

func TestUpdateOptionDoesNotPublishWhenDatabaseWriteFails(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	DB = db

	const key = "test.closed_database_option"
	common.OptionMapRWMutex.Lock()
	delete(common.OptionMap, key)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		DB = previousDB
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, key)
		common.OptionMapRWMutex.Unlock()
	})

	require.Error(t, UpdateOption(key, "value"))
	common.OptionMapRWMutex.RLock()
	_, published := common.OptionMap[key]
	common.OptionMapRWMutex.RUnlock()
	assert.False(t, published)
}
