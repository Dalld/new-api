package main

import (
	"errors"
	"testing"

	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInitializePublicStatusProbeConfigurationOrchestration(t *testing.T) {
	bootstrap := publicstatusprobesetting.DefaultDocument()
	tests := []struct {
		name        string
		master      bool
		getErr      error
		loadErr     error
		wantLoad    int
		wantEnsure  int
		wantApply   int
		wantInvalid bool
		wantErr     error
	}{
		{name: "existing option loads on master", master: true, wantApply: 1},
		{name: "existing option loads on non-master", master: false, wantApply: 1},
		{name: "missing option imports on master", master: true, getErr: gorm.ErrRecordNotFound, wantLoad: 1, wantEnsure: 1},
		{name: "missing option stays absent on non-master", master: false, getErr: gorm.ErrRecordNotFound},
		{name: "invalid environment stays absent on master", master: true, getErr: gorm.ErrRecordNotFound, loadErr: errors.New("invalid environment"), wantLoad: 1, wantInvalid: true},
		{name: "database failure is returned", master: true, getErr: errors.New("database failed"), wantErr: errors.New("database failed")},
		{name: "existing option apply failure is returned", master: true, wantApply: 1, wantErr: errors.New("apply failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loads := 0
			ensures := 0
			applies := 0
			invalid, err := initializePublicStatusProbeConfigurationWith(
				tt.master,
				func() (publicstatusprobesetting.Document, error) { return bootstrap, tt.getErr },
				func(document publicstatusprobesetting.Document) error {
					applies++
					if tt.name == "existing option apply failure is returned" {
						return tt.wantErr
					}
					return nil
				},
				func() (publicstatusprobesetting.Document, error) {
					loads++
					return bootstrap, tt.loadErr
				},
				func(document publicstatusprobesetting.Document) (publicstatusprobesetting.Document, bool, error) {
					ensures++
					return document, true, nil
				},
			)
			if tt.wantErr != nil {
				require.EqualError(t, err, tt.wantErr.Error())
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantInvalid, invalid)
			assert.Equal(t, tt.wantLoad, loads)
			assert.Equal(t, tt.wantEnsure, ensures)
			assert.Equal(t, tt.wantApply, applies)
		})
	}
}
