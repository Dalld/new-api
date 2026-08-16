package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PublicStatusProbeSlotIndex      = "idx_public_status_probe_slot"
	PublicStatusProbeLatestIndex    = "idx_public_status_probe_latest"
	PublicStatusProbeCheckedAtIndex = "idx_public_status_probe_checked_at"

	MaxPublicStatusProbeTargetKeyLength   = 96
	MaxPublicStatusProbeGroupNameLength   = 64
	MaxPublicStatusProbeDisplayNameLength = 128
	MaxPublicStatusProbeModelNameLength   = 255
	MaxPublicStatusProbeStateLength       = 32
	MaxPublicStatusProbeErrorCodeLength   = 64
	MaxPublicStatusProbeOwnerIDLength     = 128
	MaxPublicStatusProbeHistory           = 60
)

type PublicStatusProbeState string

const (
	PublicStatusProbeStateOperational      PublicStatusProbeState = "operational"
	PublicStatusProbeStateDegraded         PublicStatusProbeState = "degraded"
	PublicStatusProbeStateValidationFailed PublicStatusProbeState = "validation_failed"
	PublicStatusProbeStateFailed           PublicStatusProbeState = "failed"
	PublicStatusProbeStateUnknown          PublicStatusProbeState = "unknown"
)

type PublicStatusProbeResult struct {
	ID            int64                  `json:"id" gorm:"primaryKey"`
	TargetKey     string                 `json:"target_key" gorm:"type:varchar(96);not null;uniqueIndex:idx_public_status_probe_slot,priority:1;index:idx_public_status_probe_latest,priority:1"`
	GroupName     string                 `json:"group_name" gorm:"type:varchar(64);not null"`
	DisplayName   string                 `json:"display_name" gorm:"type:varchar(128);not null"`
	ModelName     string                 `json:"model_name" gorm:"type:varchar(255);not null"`
	ChannelID     int                    `json:"-" gorm:"not null"`
	SlotStartedAt int64                  `json:"slot_started_at" gorm:"bigint;not null;uniqueIndex:idx_public_status_probe_slot,priority:2"`
	CheckedAt     int64                  `json:"checked_at" gorm:"bigint;not null;index:idx_public_status_probe_latest,priority:2,sort:desc;index:idx_public_status_probe_checked_at"`
	State         PublicStatusProbeState `json:"state" gorm:"type:varchar(32);not null"`
	PingLatencyMS *int64                 `json:"ping_latency_ms,omitempty" gorm:"bigint"`
	ChatLatencyMS *int64                 `json:"chat_latency_ms,omitempty" gorm:"bigint"`
	ErrorCode     string                 `json:"error_code,omitempty" gorm:"type:varchar(64);not null;default:''"`
}

func (PublicStatusProbeResult) TableName() string {
	return "public_status_probe_results"
}

func (result *PublicStatusProbeResult) BeforeSave(_ *gorm.DB) error {
	result.TargetKey = strings.TrimSpace(result.TargetKey)
	result.GroupName = strings.TrimSpace(result.GroupName)
	result.DisplayName = strings.TrimSpace(result.DisplayName)
	result.ModelName = strings.TrimSpace(result.ModelName)
	result.State = PublicStatusProbeState(strings.ToLower(strings.TrimSpace(string(result.State))))
	result.ErrorCode = sanitizePublicStatusProbeErrorCode(result.ErrorCode)
	return validatePublicStatusProbeResult(result)
}

type PublicStatusProbeLease struct {
	TargetKey  string `json:"target_key" gorm:"type:varchar(96);primaryKey"`
	OwnerID    string `json:"owner_id" gorm:"type:varchar(128);not null;default:''"`
	LeaseUntil int64  `json:"lease_until" gorm:"bigint;not null;default:0"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint;not null;default:0;autoUpdateTime:false"`
}

func (PublicStatusProbeLease) TableName() string {
	return "public_status_probe_leases"
}

func CreatePublicStatusProbeResult(result *PublicStatusProbeResult) (bool, error) {
	if result == nil {
		return false, errors.New("public status probe result is nil")
	}
	created := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "target_key"},
			{Name: "slot_started_at"},
		},
		DoNothing: true,
	}).Create(result)
	if created.Error != nil {
		return false, created.Error
	}
	return created.RowsAffected == 1, nil
}

func CreatePublicStatusProbeResultIfLeaseOwner(ctx context.Context, result *PublicStatusProbeResult, ownerID string, now int64) (bool, error) {
	if result == nil {
		return false, errors.New("public status probe result is nil")
	}
	ownerID = strings.TrimSpace(ownerID)
	if err := validatePublicStatusProbeIdentity("owner_id", ownerID, MaxPublicStatusProbeOwnerIDLength); err != nil {
		return false, err
	}
	if now <= 0 {
		return false, errors.New("lease current time must be positive")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	inserted := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lease PublicStatusProbeLease
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("target_key").
			Where("target_key = ? AND owner_id = ? AND lease_until > ?", result.TargetKey, ownerID, now).
			Take(&lease).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}

		created := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "target_key"},
				{Name: "slot_started_at"},
			},
			DoNothing: true,
		}).Create(result)
		if created.Error != nil {
			return created.Error
		}
		inserted = created.RowsAffected == 1
		return nil
	})
	return inserted, err
}

func PublicStatusProbeResultExists(targetKey string, slotStartedAt int64) (bool, error) {
	targetKey = strings.TrimSpace(targetKey)
	if err := validatePublicStatusProbeIdentity("target_key", targetKey, MaxPublicStatusProbeTargetKeyLength); err != nil {
		return false, err
	}
	if slotStartedAt <= 0 || slotStartedAt%60 != 0 {
		return false, errors.New("slot_started_at must be a positive UTC minute boundary")
	}

	var result PublicStatusProbeResult
	err := DB.Select("id").
		Where("target_key = ? AND slot_started_at = ?", targetKey, slotStartedAt).
		Take(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func GetLatestPublicStatusProbeResults(targetKey string, limit int) ([]PublicStatusProbeResult, error) {
	targetKey = strings.TrimSpace(targetKey)
	if err := validatePublicStatusProbeIdentity("target_key", targetKey, MaxPublicStatusProbeTargetKeyLength); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxPublicStatusProbeHistory {
		limit = MaxPublicStatusProbeHistory
	}

	var results []PublicStatusProbeResult
	err := DB.Where("target_key = ?", targetKey).
		Order("checked_at DESC, id DESC").
		Limit(limit).
		Find(&results).Error
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(results)-1; left < right; left, right = left+1, right-1 {
		results[left], results[right] = results[right], results[left]
	}
	return results, nil
}

func DeletePublicStatusProbeResultsBefore(cutoff int64) (int64, error) {
	if cutoff <= 0 {
		return 0, errors.New("public status probe retention cutoff must be positive")
	}
	result := DB.Where("checked_at < ?", cutoff).Delete(&PublicStatusProbeResult{})
	return result.RowsAffected, result.Error
}

func AcquirePublicStatusProbeLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error) {
	targetKey, ownerID, err := validatePublicStatusProbeLeaseInput(targetKey, ownerID, now, leaseUntil)
	if err != nil {
		return false, err
	}

	lease := PublicStatusProbeLease{TargetKey: targetKey}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "target_key"}},
		DoNothing: true,
	}).Create(&lease).Error; err != nil {
		return false, err
	}

	result := DB.Model(&PublicStatusProbeLease{}).
		Where("target_key = ? AND (lease_until <= ? OR owner_id = ?)", targetKey, now, ownerID).
		Updates(map[string]any{
			"owner_id":    ownerID,
			"lease_until": leaseUntil,
			"updated_at":  now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	if result.RowsAffected != 0 {
		return false, nil
	}
	return confirmPublicStatusProbeLease(targetKey, ownerID, now, leaseUntil)
}

func RenewPublicStatusProbeLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error) {
	targetKey, ownerID, err := validatePublicStatusProbeLeaseInput(targetKey, ownerID, now, leaseUntil)
	if err != nil {
		return false, err
	}
	result := DB.Model(&PublicStatusProbeLease{}).
		Where("target_key = ? AND owner_id = ? AND lease_until > ?", targetKey, ownerID, now).
		Updates(map[string]any{
			"lease_until": leaseUntil,
			"updated_at":  now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	if result.RowsAffected != 0 {
		return false, nil
	}
	return confirmPublicStatusProbeLease(targetKey, ownerID, now, leaseUntil)
}

func confirmPublicStatusProbeLease(targetKey, ownerID string, now, leaseUntil int64) (bool, error) {
	var lease PublicStatusProbeLease
	err := DB.Select("target_key", "owner_id", "lease_until").
		Where("target_key = ? AND owner_id = ? AND lease_until >= ? AND lease_until > ?", targetKey, ownerID, leaseUntil, now).
		Take(&lease).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func ReleasePublicStatusProbeLease(targetKey, ownerID string) (bool, error) {
	targetKey = strings.TrimSpace(targetKey)
	ownerID = strings.TrimSpace(ownerID)
	if err := validatePublicStatusProbeIdentity("target_key", targetKey, MaxPublicStatusProbeTargetKeyLength); err != nil {
		return false, err
	}
	if err := validatePublicStatusProbeIdentity("owner_id", ownerID, MaxPublicStatusProbeOwnerIDLength); err != nil {
		return false, err
	}

	result := DB.Model(&PublicStatusProbeLease{}).
		Where("target_key = ? AND owner_id = ?", targetKey, ownerID).
		Updates(map[string]any{
			"owner_id":    "",
			"lease_until": int64(0),
			"updated_at":  common.GetTimestamp(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func CompletePublicStatusProbeLease(targetKey, ownerID string, now, holdUntil int64) (bool, error) {
	return CompletePublicStatusProbeLeaseWithContext(context.Background(), targetKey, ownerID, now, holdUntil)
}

func CompletePublicStatusProbeLeaseWithContext(ctx context.Context, targetKey, ownerID string, now, holdUntil int64) (bool, error) {
	targetKey = strings.TrimSpace(targetKey)
	ownerID = strings.TrimSpace(ownerID)
	if err := validatePublicStatusProbeIdentity("target_key", targetKey, MaxPublicStatusProbeTargetKeyLength); err != nil {
		return false, err
	}
	if err := validatePublicStatusProbeIdentity("owner_id", ownerID, MaxPublicStatusProbeOwnerIDLength); err != nil {
		return false, err
	}
	if now <= 0 {
		return false, errors.New("lease current time must be positive")
	}
	if holdUntil < now {
		return false, errors.New("hold_until must not be before current time")
	}
	if holdUntil <= 0 || holdUntil%60 != 0 {
		return false, errors.New("hold_until must be a positive UTC minute boundary")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	result := DB.WithContext(ctx).Model(&PublicStatusProbeLease{}).
		Where("target_key = ? AND owner_id = ? AND lease_until > ?", targetKey, ownerID, now).
		Updates(map[string]any{
			"owner_id":    "",
			"lease_until": holdUntil,
			"updated_at":  now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func validatePublicStatusProbeResult(result *PublicStatusProbeResult) error {
	fields := []struct {
		name  string
		value string
		limit int
	}{
		{name: "target_key", value: result.TargetKey, limit: MaxPublicStatusProbeTargetKeyLength},
		{name: "group_name", value: result.GroupName, limit: MaxPublicStatusProbeGroupNameLength},
		{name: "display_name", value: result.DisplayName, limit: MaxPublicStatusProbeDisplayNameLength},
		{name: "model_name", value: result.ModelName, limit: MaxPublicStatusProbeModelNameLength},
	}
	for _, field := range fields {
		if err := validatePublicStatusProbeIdentity(field.name, field.value, field.limit); err != nil {
			return err
		}
	}
	if result.ChannelID <= 0 {
		return errors.New("channel_id must be positive")
	}
	if result.SlotStartedAt <= 0 || result.SlotStartedAt%60 != 0 {
		return errors.New("slot_started_at must be a positive UTC minute boundary")
	}
	if result.CheckedAt <= 0 {
		return errors.New("checked_at must be positive")
	}
	if !isValidPublicStatusProbeState(result.State) {
		return fmt.Errorf("state must be a normalized public status probe state with at most %d characters", MaxPublicStatusProbeStateLength)
	}
	if result.PingLatencyMS != nil && *result.PingLatencyMS < 0 {
		return errors.New("ping_latency_ms must not be negative")
	}
	if result.ChatLatencyMS != nil && *result.ChatLatencyMS < 0 {
		return errors.New("chat_latency_ms must not be negative")
	}
	if utf8.RuneCountInString(result.ErrorCode) > MaxPublicStatusProbeErrorCodeLength {
		return fmt.Errorf("error_code must contain at most %d characters", MaxPublicStatusProbeErrorCodeLength)
	}
	return nil
}

func validatePublicStatusProbeLeaseInput(targetKey, ownerID string, now, leaseUntil int64) (string, string, error) {
	targetKey = strings.TrimSpace(targetKey)
	ownerID = strings.TrimSpace(ownerID)
	if err := validatePublicStatusProbeIdentity("target_key", targetKey, MaxPublicStatusProbeTargetKeyLength); err != nil {
		return "", "", err
	}
	if err := validatePublicStatusProbeIdentity("owner_id", ownerID, MaxPublicStatusProbeOwnerIDLength); err != nil {
		return "", "", err
	}
	if now <= 0 {
		return "", "", errors.New("lease current time must be positive")
	}
	if leaseUntil <= now {
		return "", "", errors.New("lease_until must be after current time")
	}
	return targetKey, ownerID, nil
}

func validatePublicStatusProbeIdentity(name, value string, limit int) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return fmt.Errorf("%s must be valid UTF-8 and contain at most %d characters", name, limit)
	}
	return nil
}

func isValidPublicStatusProbeState(state PublicStatusProbeState) bool {
	switch state {
	case PublicStatusProbeStateOperational,
		PublicStatusProbeStateDegraded,
		PublicStatusProbeStateValidationFailed,
		PublicStatusProbeStateFailed,
		PublicStatusProbeStateUnknown:
		return true
	default:
		return false
	}
}

func sanitizePublicStatusProbeErrorCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return ""
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range code {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && builder.Len() > 0 {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	normalized := strings.Trim(builder.String(), "_")
	if normalized == "" {
		normalized = "probe_failed"
	}
	if len(normalized) > MaxPublicStatusProbeErrorCodeLength {
		normalized = normalized[:MaxPublicStatusProbeErrorCodeLength]
	}
	return normalized
}
