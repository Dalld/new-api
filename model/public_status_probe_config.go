package model

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrPublicStatusProbeConfigConflict = errors.New("public status probe configuration conflict")

var publicStatusProbePublishMu sync.Mutex

func EnsurePublicStatusProbeConfig(bootstrap publicstatusprobesetting.Document) (publicstatusprobesetting.Document, bool, error) {
	existing, err := GetPublicStatusProbeConfig()
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return publicstatusprobesetting.Document{}, false, err
	}

	raw, err := publicstatusprobesetting.EncodeDocument(bootstrap)
	if err != nil {
		return publicstatusprobesetting.Document{}, false, err
	}
	candidate, err := newPublicStatusProbeCreateCandidate(raw)
	if err != nil {
		return publicstatusprobesetting.Document{}, false, err
	}
	result := createPublicStatusProbeConfig(DB, &Option{
		Key:   publicstatusprobesetting.OptionKey,
		Value: candidate,
	})
	if result.Error != nil {
		return publicstatusprobesetting.Document{}, false, result.Error
	}

	var authoritative Option
	if err := DB.Where(clause.Eq{
		Column: clause.Column{Name: "key"},
		Value:  publicstatusprobesetting.OptionKey,
	}).Take(&authoritative).Error; err != nil {
		return publicstatusprobesetting.Document{}, false, err
	}
	created := authoritative.Value == candidate
	if created {
		normalized := updatePublicStatusProbeConfig(DB, candidate, raw)
		if normalized.Error != nil {
			return publicstatusprobesetting.Document{}, false, normalized.Error
		}
		if normalized.RowsAffected == 1 {
			authoritative.Value = raw
		} else if err := DB.Where(clause.Eq{
			Column: clause.Column{Name: "key"},
			Value:  publicstatusprobesetting.OptionKey,
		}).Take(&authoritative).Error; err != nil {
			return publicstatusprobesetting.Document{}, false, err
		}
		created = authoritative.Value == raw || authoritative.Value == candidate
	}

	existing, err = publicstatusprobesetting.DecodeDocument(authoritative.Value)
	if err != nil {
		return publicstatusprobesetting.Document{}, false, err
	}
	return existing, created, nil
}

func GetPublicStatusProbeConfig() (publicstatusprobesetting.Document, error) {
	var option Option
	if err := DB.Where(clause.Eq{
		Column: clause.Column{Name: "key"},
		Value:  publicstatusprobesetting.OptionKey,
	}).Take(&option).Error; err != nil {
		return publicstatusprobesetting.Document{}, err
	}
	return publicstatusprobesetting.DecodeDocument(option.Value)
}

func CompareAndSwapPublicStatusProbeConfig(
	expectedVersion int64,
	mutate func(*publicstatusprobesetting.Document) error,
) (publicstatusprobesetting.Document, error) {
	var option Option
	if err := DB.Where(clause.Eq{
		Column: clause.Column{Name: "key"},
		Value:  publicstatusprobesetting.OptionKey,
	}).Take(&option).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return publicstatusprobesetting.Document{}, ErrPublicStatusProbeConfigConflict
		}
		return publicstatusprobesetting.Document{}, err
	}

	current, err := publicstatusprobesetting.DecodeDocument(option.Value)
	if err != nil {
		return publicstatusprobesetting.Document{}, err
	}
	if current.Version != expectedVersion {
		return publicstatusprobesetting.Document{}, ErrPublicStatusProbeConfigConflict
	}
	if mutate == nil {
		return publicstatusprobesetting.Document{}, errors.New("public status probe mutation is required")
	}

	next := current
	next.Targets = append([]publicstatusprobesetting.Target(nil), current.Targets...)
	if err := mutate(&next); err != nil {
		return publicstatusprobesetting.Document{}, err
	}
	next.Version = current.Version + 1
	nextRaw, err := publicstatusprobesetting.EncodeDocument(next)
	if err != nil {
		return publicstatusprobesetting.Document{}, err
	}
	next, err = publicstatusprobesetting.DecodeDocument(nextRaw)
	if err != nil {
		return publicstatusprobesetting.Document{}, err
	}

	result := updatePublicStatusProbeConfig(DB, option.Value, nextRaw)
	if result.Error != nil {
		return publicstatusprobesetting.Document{}, result.Error
	}
	if result.RowsAffected != 1 {
		return publicstatusprobesetting.Document{}, ErrPublicStatusProbeConfigConflict
	}
	if _, err := publishPublicStatusProbeConfig(nextRaw); err != nil {
		return publicstatusprobesetting.Document{}, err
	}
	return next, nil
}

func ApplyPublicStatusProbeConfigOption(raw string) error {
	_, err := publishPublicStatusProbeConfig(raw)
	return err
}

func publishPublicStatusProbeConfig(raw string) (bool, error) {
	document, err := publicstatusprobesetting.DecodeDocument(raw)
	if err != nil {
		return false, err
	}
	canonicalRaw, err := publicstatusprobesetting.EncodeDocument(document)
	if err != nil {
		return false, err
	}

	publicStatusProbePublishMu.Lock()
	defer publicStatusProbePublishMu.Unlock()

	current := publicstatusprobesetting.CurrentDocument()
	currentRaw, err := publicstatusprobesetting.EncodeDocument(current)
	if err != nil {
		return false, err
	}
	if document.Version < current.Version {
		return false, nil
	}
	if document.Version == current.Version {
		if canonicalRaw == currentRaw {
			setPublicStatusProbeOptionMap(raw)
			return true, nil
		}
		return false, ErrPublicStatusProbeConfigConflict
	}

	setPublicStatusProbeOptionMap(raw)
	if err := publicstatusprobesetting.PublishDocument(document); err != nil {
		return false, err
	}
	return true, nil
}

func createPublicStatusProbeConfig(db *gorm.DB, option *Option) *gorm.DB {
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(option)
}

func updatePublicStatusProbeConfig(db *gorm.DB, oldRaw string, newRaw string) *gorm.DB {
	conditions := []clause.Expression{
		clause.Eq{
			Column: clause.Column{Name: "key"},
			Value:  publicstatusprobesetting.OptionKey,
		},
	}
	if db.Dialector.Name() == "mysql" {
		conditions = append(conditions, clause.Expr{
			SQL:  "BINARY ? = BINARY ?",
			Vars: []any{clause.Column{Name: "value"}, oldRaw},
		})
	} else {
		conditions = append(conditions, clause.Eq{
			Column: clause.Column{Name: "value"},
			Value:  oldRaw,
		})
	}
	return db.Model(&Option{}).Where(clause.And(conditions...)).Update("value", newRaw)
}

func setPublicStatusProbeOptionMap(raw string) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[publicstatusprobesetting.OptionKey] = raw
	common.OptionMapRWMutex.Unlock()
}

func newPublicStatusProbeCreateCandidate(raw string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	var candidate strings.Builder
	candidate.Grow(len(raw) + 1 + len(nonce)*8)
	candidate.WriteString(raw)
	candidate.WriteByte('\n')
	for _, value := range nonce {
		for bit := byte(1); bit != 0; bit <<= 1 {
			if value&bit == 0 {
				candidate.WriteByte(' ')
			} else {
				candidate.WriteByte('\t')
			}
		}
	}
	return candidate.String(), nil
}
