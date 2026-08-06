/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ChevronLeft, Loader2, UserRoundPlus, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useDebounce } from '@/hooks/use-debounce'
import { useAuthStore } from '@/stores/auth-store'

import { bindUserInviter, searchUsers } from '../../api'
import {
  filterInviterCandidates,
  shouldShowBindInviterAction,
} from '../../lib/user-inviter-binding'
import type { User } from '../../types'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: User
  onSuccess?: () => void
}

type SearchStage = 'search' | 'confirm'

function userLabel(user: User) {
  return `${user.username} (ID: ${user.id})`
}

export function UserInviterBindingDialog({
  open,
  onOpenChange,
  user,
  onSuccess,
}: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const operator = useAuthStore((state) => state.auth.user)
  const [stage, setStage] = useState<SearchStage>('search')
  const [keyword, setKeyword] = useState('')
  const [selected, setSelected] = useState<User | null>(null)
  const [committedKeyword, setCommittedKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword.trim(), 300)
  const normalizedKeyword = debouncedKeyword.trim()

  useEffect(() => {
    if (!open) {
      setStage('search')
      setKeyword('')
      setSelected(null)
      setCommittedKeyword('')
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    setSelected(null)
  }, [keyword, normalizedKeyword, open])

  useEffect(() => {
    if (keyword.trim().length === 0) {
      setCommittedKeyword('')
    } else if (normalizedKeyword.length > 0) {
      setCommittedKeyword(normalizedKeyword)
    }
  }, [keyword, normalizedKeyword])

  const searchEnabled =
    open && stage === 'search' && committedKeyword.length > 0
  const searchTermPending = keyword.trim() !== normalizedKeyword
  const searchQuery = useQuery({
    queryKey: ['user-search', committedKeyword],
    queryFn: async () => {
      const result = await searchUsers({
        keyword: committedKeyword,
        p: 1,
        page_size: 20,
      })
      if (!result.success) {
        throw new Error(result.message || t('Failed to search users'))
      }
      return result.data?.items ?? []
    },
    enabled: searchEnabled,
    retry: false,
    staleTime: 0,
  })

  const visibleCandidates = useMemo(() => {
    const items = searchQuery.data ?? []
    return filterInviterCandidates(items, user.id)
  }, [searchQuery.data, user.id])

  const bindMutation = useMutation({
    mutationFn: async () => {
      if (!selected) throw new Error('No inviter selected')
      const result = await bindUserInviter(user.id, selected.id)
      if (!result.success) {
        throw new Error(result.message || t('Binding failed'))
      }
      return result.data
    },
    onSuccess: async () => {
      toast.success(t('Inviter bound successfully'))
      onOpenChange(false)
      onSuccess?.()
      const refreshResults = await Promise.allSettled([
        queryClient.invalidateQueries({ queryKey: ['affiliate'] }),
        queryClient.invalidateQueries({ queryKey: ['my-affiliate'] }),
        queryClient.invalidateQueries({ queryKey: ['users'] }),
      ])
      if (refreshResults.some((result) => result.status === 'rejected')) {
        toast.warning(
          t('Inviter bound successfully, but refreshing the list failed')
        )
      }
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : ''
      toast.error(message ? t(message) : t('Binding failed'))
    },
  })

  const handleSearchReset = () => {
    setKeyword('')
    setSelected(null)
    setCommittedKeyword('')
  }

  const renderSearchResults = () => {
    if (committedKeyword.length === 0) {
      return (
        <p className='text-muted-foreground py-6 text-center text-sm'>
          {t('Enter at least one character to search')}
        </p>
      )
    }

    if (searchQuery.isLoading) {
      return (
        <div className='text-muted-foreground flex items-center justify-center gap-2 py-6 text-sm'>
          <Loader2 className='h-4 w-4 animate-spin' />
          {t('Searching')}
        </div>
      )
    }

    if (searchQuery.isError) {
      return (
        <div className='space-y-2 py-6 text-center text-sm'>
          <p className='text-muted-foreground'>{t('Failed to search users')}</p>
          <Button
            variant='outline'
            size='sm'
            onClick={() => searchQuery.refetch()}
          >
            {t('Retry')}
          </Button>
        </div>
      )
    }

    if (visibleCandidates.length === 0) {
      return (
        <p className='text-muted-foreground py-6 text-center text-sm'>
          {t('No Users Found')}
        </p>
      )
    }

    return (
      <div className='space-y-2 pr-2'>
        {visibleCandidates.map((candidate) => (
          <button
            key={candidate.id}
            type='button'
            className='hover:bg-accent flex w-full items-center justify-between rounded-md border px-3 py-2 text-left text-sm'
            aria-pressed={selected?.id === candidate.id}
            onClick={() => setSelected(candidate)}
          >
            <span className='min-w-0'>
              <span className='block truncate font-medium'>
                {candidate.username}
              </span>
              <span className='text-muted-foreground block truncate text-xs'>
                {candidate.display_name || t('No display name')} / ID:{' '}
                {candidate.id}
              </span>
            </span>
            <Check
              className={`h-4 w-4 ${selected?.id === candidate.id ? 'opacity-100' : 'opacity-0'}`}
            />
          </button>
        ))}
      </div>
    )
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title={
          <>
            <UserRoundPlus className='h-5 w-5' />
            {t('Bind inviter')}
          </>
        }
        description={t('Bind an inviter for {{username}}', {
          username: user.username,
        })}
        contentClassName='sm:max-w-xl'
        titleClassName='flex items-center gap-2'
        bodyClassName='space-y-4'
        contentHeight='auto'
      >
        {!shouldShowBindInviterAction(operator?.role, user) ? (
          <p className='text-muted-foreground text-sm'>
            {t('Admin access required')}
          </p>
        ) : (
          <div className='space-y-4'>
            <div className='text-muted-foreground text-sm'>
              {t('Target User')}: {userLabel(user)}
            </div>
            <div className='flex gap-2'>
              <Input
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                placeholder={t('Search by username, display name or ID...')}
              />
              <Button
                variant='outline'
                size='icon'
                onClick={handleSearchReset}
                aria-label={t('Clear')}
              >
                <X className='h-4 w-4' />
              </Button>
            </div>

            <ScrollArea className='max-h-72'>
              {renderSearchResults()}
            </ScrollArea>

            <div className='flex items-center justify-between'>
              <div className='text-muted-foreground text-sm'>
                {selected
                  ? `${t('selected')}: ${userLabel(selected)}`
                  : t('No inviter selected')}
              </div>
              <Button
                disabled={!selected || searchTermPending}
                onClick={() => setStage('confirm')}
              >
                {t('Continue')}
              </Button>
            </div>
          </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={open && stage === 'confirm'}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setStage('search')
          }
        }}
        title={t('Confirm inviter binding')}
        desc={t(
          'Bind {{inviter}} to {{invitee}}? This can only be done once.',
          {
            inviter: userLabel(selected || user),
            invitee: userLabel(user),
          }
        )}
        confirmText={t('Bind inviter')}
        handleConfirm={() => bindMutation.mutate()}
        disabled={!selected}
        isLoading={bindMutation.isPending}
      >
        <Button
          variant='ghost'
          className='mb-2'
          disabled={bindMutation.isPending}
          onClick={() => setStage('search')}
        >
          <ChevronLeft className='h-4 w-4' />
          {t('Back')}
        </Button>
      </ConfirmDialog>
    </>
  )
}
