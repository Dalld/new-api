package affiliate_setting

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRate(t *testing.T) {
	tests := []struct {
		name    string
		rate    float64
		wantErr bool
	}{
		{name: "disabled", rate: 0},
		{name: "fraction", rate: 0.25},
		{name: "full rate", rate: 1},
		{name: "negative", rate: -0.01, wantErr: true},
		{name: "above one", rate: 1.01, wantErr: true},
		{name: "not a number", rate: math.NaN(), wantErr: true},
		{name: "positive infinity", rate: math.Inf(1), wantErr: true},
		{name: "negative infinity", rate: math.Inf(-1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRate(tt.rate)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSetRateRejectsInvalidValueWithoutChangingCurrentRate(t *testing.T) {
	original := GetRate()
	t.Cleanup(func() { require.NoError(t, SetRate(original)) })

	require.NoError(t, SetRate(0.2))
	require.Error(t, SetRate(math.NaN()))
	assert.Equal(t, 0.2, GetRate())
}

func TestAffiliateSettingJSONValidationUsesCommonJSONHelpers(t *testing.T) {
	var setting AffiliateSetting
	require.NoError(t, common.Unmarshal([]byte(`{"commission_rate":0.4}`), &setting))
	assert.Equal(t, 0.4, setting.CommissionRate)

	err := common.Unmarshal([]byte(`{"commission_rate":1.1}`), &setting)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 0 and 1")
}
