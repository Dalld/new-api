package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDefaultSidebarConfigForRoleIncludesAffiliateModules(t *testing.T) {
	tests := []struct {
		name             string
		role             int
		expectAdmin      bool
		expectSetting    bool
		expectAdminEntry bool
	}{
		{
			name: "common user",
			role: common.RoleCommonUser,
		},
		{
			name:             "admin",
			role:             common.RoleAdminUser,
			expectAdmin:      true,
			expectAdminEntry: true,
		},
		{
			name:             "root",
			role:             common.RoleRootUser,
			expectAdmin:      true,
			expectSetting:    true,
			expectAdminEntry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config map[string]map[string]bool
			require.NoError(t, common.Unmarshal(
				[]byte(generateDefaultSidebarConfigForRole(tt.role)),
				&config,
			))

			require.Contains(t, config, "personal")
			assert.True(t, config["personal"]["referral"])

			admin, hasAdmin := config["admin"]
			assert.Equal(t, tt.expectAdmin, hasAdmin)
			if hasAdmin {
				assert.Equal(t, tt.expectAdminEntry, admin["affiliate"])
				assert.Equal(t, tt.expectSetting, admin["setting"])
			}
		})
	}
}
