package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type AffiliateBindResult struct {
	InviteeID         int `json:"invitee_id"`
	InviterID         int `json:"inviter_id"`
	PreviousInviterID int `json:"previous_inviter_id"`
}

func collectInviterChain(tx *gorm.DB, inviteeID int, inviterID int) ([]int, error) {
	visited := map[int]struct{}{
		inviteeID: {},
	}
	chain := make([]int, 0)
	currentID := inviterID
	first := true
	for currentID > 0 {
		if _, ok := visited[currentID]; ok {
			return nil, ErrAffiliateBindCycle
		}
		visited[currentID] = struct{}{}

		var current User
		if err := tx.
			Select("id", "inviter_id").
			Where("id = ?", currentID).
			First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if first {
					return nil, ErrAffiliateBindInviterMissing
				}
				common.SysLog(fmt.Sprintf("affiliate bind found broken inviter chain at user %d", currentID))
				return nil, ErrAffiliateBindCycle
			}
			return nil, err
		}
		chain = append(chain, currentID)
		first = false
		currentID = current.InviterId
	}
	return chain, nil
}

func lockAffiliateBindUsers(tx *gorm.DB, ids []int) (map[int]*User, error) {
	orderedIDs := append([]int(nil), ids...)
	sort.Ints(orderedIDs)

	var users []User
	if err := lockForUpdate(tx).
		Where("id IN ?", orderedIDs).
		Order("id ASC").
		Find(&users).Error; err != nil {
		return nil, err
	}

	lockedUsers := make(map[int]*User, len(users))
	for i := range users {
		lockedUsers[users[i].Id] = &users[i]
	}
	return lockedUsers, nil
}

func validateLockedInviterChain(lockedUsers map[int]*User, inviteeID int, inviterID int) error {
	visited := map[int]struct{}{
		inviteeID: {},
	}
	currentID := inviterID
	for currentID > 0 {
		if _, ok := visited[currentID]; ok {
			return ErrAffiliateBindCycle
		}
		visited[currentID] = struct{}{}

		current, ok := lockedUsers[currentID]
		if !ok {
			// The chain changed between discovery and the ordered lock query. Fail
			// closed instead of acquiring another row lock out of order.
			return ErrAffiliateBindConflict
		}
		currentID = current.InviterId
	}
	return nil
}

func BindAffiliateInviter(inviteeID int, inviterID int) (*AffiliateBindResult, error) {
	if inviteeID <= 0 || inviterID <= 0 {
		return nil, ErrAffiliateBindInvalidInput
	}
	if inviteeID == inviterID {
		return nil, ErrAffiliateBindSelfReference
	}

	result := &AffiliateBindResult{
		InviteeID: inviteeID,
		InviterID: inviterID,
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var target User
		if err := tx.Select("id", "inviter_id").Where("id = ?", inviteeID).First(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAffiliateBindTargetMissing
			}
			return err
		}
		if target.InviterId != 0 {
			return ErrAffiliateBindAlreadyBound
		}

		chain, err := collectInviterChain(tx, inviteeID, inviterID)
		if err != nil {
			return err
		}
		lockIDs := append(chain, inviteeID)
		lockedUsers, err := lockAffiliateBindUsers(tx, lockIDs)
		if err != nil {
			return err
		}

		invitee, inviteeFound := lockedUsers[inviteeID]
		if !inviteeFound {
			return ErrAffiliateBindTargetMissing
		}
		if invitee.InviterId != 0 {
			return ErrAffiliateBindAlreadyBound
		}

		if _, inviterFound := lockedUsers[inviterID]; !inviterFound {
			return ErrAffiliateBindInviterMissing
		}

		if err := validateLockedInviterChain(lockedUsers, inviteeID, inviterID); err != nil {
			return err
		}

		updateResult := tx.Model(&User{}).
			Where("id = ? AND inviter_id = 0", inviteeID).
			Update("inviter_id", inviterID)
		if updateResult.Error != nil {
			return updateResult.Error
		}
		if updateResult.RowsAffected != 1 {
			return ErrAffiliateBindConflict
		}

		incrementResult := tx.Model(&User{}).
			Where("id = ?", inviterID).
			Update("aff_count", gorm.Expr("aff_count + ?", 1))
		if incrementResult.Error != nil {
			return incrementResult.Error
		}
		if incrementResult.RowsAffected != 1 {
			return ErrAffiliateBindInviterMissing
		}

		result.PreviousInviterID = invitee.InviterId
		return nil
	})
	if err != nil {
		if isAffiliateBindLockConflict(err) {
			common.SysLog(fmt.Sprintf(
				"affiliate bind transaction conflict: invitee_id=%d inviter_id=%d",
				inviteeID,
				inviterID,
			))
			return nil, ErrAffiliateBindConflict
		}
		if !isAffiliateBindDomainError(err) {
			common.SysError(fmt.Sprintf(
				"affiliate bind database failure: invitee_id=%d inviter_id=%d err=%v",
				inviteeID,
				inviterID,
				err,
			))
			return nil, ErrDatabase
		}
		common.SysLog(fmt.Sprintf(
			"affiliate bind rejected: invitee_id=%d inviter_id=%d reason=%v",
			inviteeID,
			inviterID,
			err,
		))
		return nil, err
	}

	if err := InvalidateUserCache(inviteeID); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate invitee cache after affiliate bind: %v", err))
	}
	if err := InvalidateUserCache(inviterID); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate inviter cache after affiliate bind: %v", err))
	}
	return result, nil
}

func isAffiliateBindDomainError(err error) bool {
	for _, domainErr := range []error{
		ErrAffiliateBindInvalidInput,
		ErrAffiliateBindTargetMissing,
		ErrAffiliateBindInviterMissing,
		ErrAffiliateBindAlreadyBound,
		ErrAffiliateBindSelfReference,
		ErrAffiliateBindCycle,
		ErrAffiliateBindConflict,
	} {
		if errors.Is(err, domainErr) {
			return true
		}
	}
	return false
}

func isAffiliateBindLockConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database is locked",
		"database table is locked",
		"database is deadlocked",
		"deadlock",
		"lock wait timeout",
		"lock timeout",
		"serialization failure",
		"could not serialize access",
		"serialization error",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
