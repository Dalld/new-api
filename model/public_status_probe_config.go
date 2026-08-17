package model

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var (
	ErrPublicStatusProbeConfigConflict    = errors.New("public status probe configuration conflict")
	ErrPublicStatusProbeConfigRequiresCAS = errors.New("public status probe configuration requires compare-and-swap")
)

var publicStatusProbePublishMu sync.Mutex
var publicStatusProbeConfigLoaded atomic.Bool

func EnsurePublicStatusProbeConfig(bootstrap publicstatusprobesetting.Document) (publicstatusprobesetting.Document, bool, error) {
	db := publicStatusProbeDB(DB)
	existing, existingRaw, err := getPublicStatusProbeConfig(db)
	if err == nil {
		if _, err := publishPublicStatusProbeConfig(existingRaw); err != nil {
			return publicstatusprobesetting.Document{}, false, err
		}
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
	result := createPublicStatusProbeConfig(db, &Option{
		Key:   publicstatusprobesetting.OptionKey,
		Value: candidate,
	})
	if result.Error != nil {
		return publicstatusprobesetting.Document{}, false, result.Error
	}

	var authoritative Option
	if err := db.Where(clause.Eq{
		Column: clause.Column{Name: "key"},
		Value:  publicstatusprobesetting.OptionKey,
	}).Take(&authoritative).Error; err != nil {
		return publicstatusprobesetting.Document{}, false, err
	}
	created := authoritative.Value == candidate
	if created {
		normalized := updatePublicStatusProbeConfig(db, candidate, raw)
		if normalized.Error != nil {
			return publicstatusprobesetting.Document{}, false, normalized.Error
		}
		if normalized.RowsAffected == 1 {
			authoritative.Value = raw
		} else if err := db.Where(clause.Eq{
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
	if _, err := publishPublicStatusProbeConfig(authoritative.Value); err != nil {
		return publicstatusprobesetting.Document{}, false, err
	}
	return existing, created, nil
}

func GetPublicStatusProbeConfig() (publicstatusprobesetting.Document, error) {
	document, _, err := getPublicStatusProbeConfig(publicStatusProbeDB(DB))
	return document, err
}

func getPublicStatusProbeConfig(db *gorm.DB) (publicstatusprobesetting.Document, string, error) {
	var option Option
	if err := db.Where(clause.Eq{
		Column: clause.Column{Name: "key"},
		Value:  publicstatusprobesetting.OptionKey,
	}).Take(&option).Error; err != nil {
		return publicstatusprobesetting.Document{}, "", err
	}
	document, err := publicstatusprobesetting.DecodeDocument(option.Value)
	return document, option.Value, err
}

func CompareAndSwapPublicStatusProbeConfig(
	expectedVersion int64,
	mutate func(*publicstatusprobesetting.Document) error,
) (publicstatusprobesetting.Document, error) {
	db := publicStatusProbeDB(DB)
	var option Option
	if err := db.Where(clause.Eq{
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

	result := updatePublicStatusProbeConfig(db, option.Value, nextRaw)
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

	current := publicstatusprobesetting.CurrentDocument()
	currentRaw, err := publicstatusprobesetting.EncodeDocument(current)
	if err != nil {
		publicStatusProbePublishMu.Unlock()
		return false, err
	}
	if document.Version < current.Version {
		publicStatusProbePublishMu.Unlock()
		return false, nil
	}
	if document.Version == current.Version {
		if canonicalRaw == currentRaw {
			setPublicStatusProbeOptionMap(raw)
			publicStatusProbeConfigLoaded.Store(true)
			publicStatusProbePublishMu.Unlock()
			return true, nil
		}
		if publicStatusProbeConfigLoaded.Load() {
			publicStatusProbePublishMu.Unlock()
			return false, ErrPublicStatusProbeConfigConflict
		}
	}

	drain, err := publicstatusprobesetting.StageDocument(document)
	if err != nil {
		publicStatusProbePublishMu.Unlock()
		return false, err
	}
	setPublicStatusProbeOptionMap(raw)
	publicStatusProbeConfigLoaded.Store(true)
	publicStatusProbePublishMu.Unlock()
	if drain {
		publicstatusprobesetting.DrainPublishNotifications()
	}
	return true, nil
}

func createPublicStatusProbeConfig(db *gorm.DB, option *Option) *gorm.DB {
	return publicStatusProbeDB(db).Clauses(clause.OnConflict{DoNothing: true}).Create(option)
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
	return publicStatusProbeDB(db).Model(&Option{}).Where(clause.And(conditions...)).Update("value", newRaw)
}

func publicStatusProbeDB(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
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
