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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowRightLeft,
  ChevronLeft,
  ChevronRight,
  ReceiptText,
  Search,
  Users,
} from 'lucide-react'
import {
  useCallback,
  useEffect,
  useState,
  type FormEvent,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  getAffiliateCode,
  getAffiliateTransferPolicy,
  getSelfAffiliateSummary,
  getSelfCommissions,
  getSelfInvitees,
  getSelfRechargeTotal,
} from './api'
import { TransferDialog } from './components/transfer-dialog'
import { useAffiliateTransfer } from './hooks/use-affiliate-transfer'
import { createInviteLink, getTotalPages } from './lib'
import type {
  ApiResponse,
  MyAffiliateSearchState,
  MyAffiliateTab,
  SelfAffiliateSummary,
  SelfCommissionRecord,
  SelfInvitee,
} from './types'

interface MyAffiliateProps {
  search: MyAffiliateSearchState
  onSearchChange: (next: MyAffiliateSearchState) => void
}

interface PagerProps {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}

function formatTime(timestamp: number) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(timestamp * 1000))
}

function Pager({ page, pageSize, total, onPageChange }: PagerProps) {
  const { t } = useTranslation()
  const totalPages = getTotalPages(total, pageSize)

  return (
    <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
      <span className='text-muted-foreground text-sm'>
        {t('Total: {{count}}', { count: total })}
      </span>
      <div className='flex items-center gap-1.5'>
        <Button
          type='button'
          variant='outline'
          size='icon'
          disabled={page <= 1}
          aria-label={t('Previous page')}
          title={t('Previous page')}
          onClick={() => onPageChange(Math.max(1, page - 1))}
        >
          <ChevronLeft aria-hidden='true' />
        </Button>
        <span className='text-muted-foreground min-w-20 text-center text-sm tabular-nums'>
          {t('{{current}} / {{total}}', { current: page, total: totalPages })}
        </span>
        <Button
          type='button'
          variant='outline'
          size='icon'
          disabled={page >= totalPages}
          aria-label={t('Next page')}
          title={t('Next page')}
          onClick={() => onPageChange(Math.min(totalPages, page + 1))}
        >
          <ChevronRight aria-hidden='true' />
        </Button>
      </div>
    </div>
  )
}

function LoadingRows({ columns }: { columns: number }) {
  return Array.from({ length: 5 }, (_, index) => (
    <TableRow key={index}>
      <TableCell colSpan={columns}>
        <Skeleton className='h-5 w-full' />
      </TableCell>
    </TableRow>
  ))
}

function MobileLoading() {
  return (
    <div className='space-y-2 md:hidden'>
      {Array.from({ length: 3 }, (_, index) => (
        <Skeleton key={index} className='h-28 w-full rounded-lg' />
      ))}
    </div>
  )
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className='text-muted-foreground flex min-h-36 items-center justify-center border-y text-center text-sm'>
      {message}
    </div>
  )
}

function ErrorState() {
  const { t } = useTranslation()
  return (
    <div className='text-destructive flex min-h-36 items-center justify-center border-y text-center text-sm'>
      {t('Failed to load referral data')}
    </div>
  )
}

function InviteeList({ items }: { items: SelfInvitee[] }) {
  const { t } = useTranslation()
  return (
    <>
      <div className='hidden overflow-x-auto rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Username')}</TableHead>
              <TableHead>{t('Display Name')}</TableHead>
              <TableHead>{t('Registered At')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='font-medium'>
                  {item.username || '-'}
                </TableCell>
                <TableCell>{item.display_name || '-'}</TableCell>
                <TableCell className='whitespace-nowrap'>
                  {formatTime(item.created_at)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className='space-y-2 md:hidden'>
        {items.map((item) => (
          <Card key={item.id} size='sm' className='rounded-lg'>
            <CardContent className='space-y-2'>
              <div className='flex min-w-0 items-center gap-2'>
                <Users
                  className='text-muted-foreground size-4 shrink-0'
                  aria-hidden='true'
                />
                <span className='truncate font-medium'>
                  {item.username || '-'}
                </span>
              </div>
              <dl className='grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs'>
                <dt className='text-muted-foreground'>{t('Display Name')}</dt>
                <dd className='truncate text-right'>
                  {item.display_name || '-'}
                </dd>
                <dt className='text-muted-foreground'>{t('Registered At')}</dt>
                <dd className='text-right'>{formatTime(item.created_at)}</dd>
              </dl>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  )
}

function CommissionList({ items }: { items: SelfCommissionRecord[] }) {
  const { t } = useTranslation()
  return (
    <>
      <div className='hidden overflow-x-auto rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Order No.')}</TableHead>
              <TableHead>{t('Invitee')}</TableHead>
              <TableHead>{t('Recharge Amount')}</TableHead>
              <TableHead>{t('Commission Amount')}</TableHead>
              <TableHead>{t('Created At')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='max-w-48 truncate font-mono text-xs'>
                  {item.order_no || '-'}
                </TableCell>
                <TableCell>{item.invitee_username || '-'}</TableCell>
                <TableCell>{formatQuota(item.commission_base_quota)}</TableCell>
                <TableCell className='font-medium'>
                  {formatQuota(item.commission_quota)}
                </TableCell>
                <TableCell className='whitespace-nowrap'>
                  {formatTime(item.created_at)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className='space-y-2 md:hidden'>
        {items.map((item) => (
          <Card key={item.id} size='sm' className='rounded-lg'>
            <CardContent className='space-y-2'>
              <div className='flex min-w-0 items-center gap-2'>
                <ReceiptText
                  className='text-muted-foreground size-4 shrink-0'
                  aria-hidden='true'
                />
                <span className='truncate font-mono text-xs'>
                  {item.order_no || '-'}
                </span>
              </div>
              <dl className='grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs'>
                <dt className='text-muted-foreground'>{t('Invitee')}</dt>
                <dd className='truncate text-right'>
                  {item.invitee_username || '-'}
                </dd>
                <dt className='text-muted-foreground'>
                  {t('Recharge Amount')}
                </dt>
                <dd className='text-right'>
                  {formatQuota(item.commission_base_quota)}
                </dd>
                <dt className='text-muted-foreground'>
                  {t('Commission Amount')}
                </dt>
                <dd className='text-right font-medium'>
                  {formatQuota(item.commission_quota)}
                </dd>
                <dt className='text-muted-foreground'>{t('Created At')}</dt>
                <dd className='text-right'>{formatTime(item.created_at)}</dd>
              </dl>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  )
}

export function MyAffiliate({ search, onSearchChange }: MyAffiliateProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const authUser = useAuthStore((state) => state.auth.user)
  const setAuthUser = useAuthStore((state) => state.auth.setUser)
  const [keyword, setKeyword] = useState(search.keyword)
  const [transferDialogOpen, setTransferDialogOpen] = useState(false)

  useEffect(() => setKeyword(search.keyword), [search.keyword])

  const codeQuery = useQuery({
    queryKey: ['my-affiliate', 'code'],
    queryFn: getAffiliateCode,
  })
  const summaryQuery = useQuery({
    queryKey: ['my-affiliate', 'summary'],
    queryFn: getSelfAffiliateSummary,
  })
  const transferPolicyQuery = useQuery({
    queryKey: ['my-affiliate', 'transfer-policy'],
    queryFn: getAffiliateTransferPolicy,
  })
  const rechargeTotalQuery = useQuery({
    queryKey: ['my-affiliate', 'recharge-total'],
    queryFn: getSelfRechargeTotal,
  })
  const inviteesQuery = useQuery({
    queryKey: [
      'my-affiliate',
      'invitees',
      search.page,
      search.pageSize,
      search.keyword,
    ],
    queryFn: () => getSelfInvitees(search),
    enabled: search.tab === 'invitees',
    placeholderData: (previousData) => previousData,
  })
  const commissionsQuery = useQuery({
    queryKey: [
      'my-affiliate',
      'commissions',
      search.page,
      search.pageSize,
      search.keyword,
    ],
    queryFn: () => getSelfCommissions(search),
    enabled: search.tab === 'commissions',
    placeholderData: (previousData) => previousData,
  })

  const affiliateCode = codeQuery.data?.success
    ? (codeQuery.data.data ?? '')
    : ''
  const summary = summaryQuery.data?.success
    ? summaryQuery.data.data
    : undefined
  const rechargeTotal = rechargeTotalQuery.data?.success
    ? rechargeTotalQuery.data.data
    : undefined
  const inviteLink = createInviteLink(
    typeof window === 'undefined' ? '' : window.location.origin,
    affiliateCode
  )
  const activeQuery =
    search.tab === 'invitees' ? inviteesQuery : commissionsQuery
  const activePage = activeQuery.data?.success
    ? activeQuery.data.data
    : undefined
  const isInvalidResponse =
    activeQuery.data != null && !activeQuery.data.success

  const syncAuthenticatedUser = useCallback(
    (nextSummary: SelfAffiliateSummary) => {
      if (!authUser) return
      setAuthUser({
        ...authUser,
        quota: nextSummary.quota,
        aff_count: nextSummary.aff_count,
        aff_quota: nextSummary.aff_quota,
        aff_history_quota: nextSummary.aff_history_quota,
      })
    },
    [authUser, setAuthUser]
  )

  const handleTransferSuccess = useCallback(
    async (quota: number) => {
      const queryKey = ['my-affiliate', 'summary'] as const
      const current =
        queryClient.getQueryData<ApiResponse<SelfAffiliateSummary>>(queryKey)

      if (current?.success && current.data) {
        const nextSummary = {
          ...current.data,
          quota: current.data.quota + quota,
          aff_quota: Math.max(0, current.data.aff_quota - quota),
        }
        queryClient.setQueryData(queryKey, { ...current, data: nextSummary })
        syncAuthenticatedUser(nextSummary)
      }

      await queryClient.invalidateQueries({ queryKey, exact: true })
      const refreshed =
        queryClient.getQueryData<ApiResponse<SelfAffiliateSummary>>(queryKey)
      if (refreshed?.success && refreshed.data) {
        syncAuthenticatedUser(refreshed.data)
      }
    },
    [queryClient, syncAuthenticatedUser]
  )
  const { transferring, transferQuota } = useAffiliateTransfer({
    onSuccess: handleTransferSuccess,
  })

  const updateSearch = useCallback(
    (patch: Partial<MyAffiliateSearchState>) => {
      onSearchChange({ ...search, ...patch })
    },
    [onSearchChange, search]
  )

  useEffect(() => {
    if (!activePage) return
    const totalPages = getTotalPages(activePage.total, search.pageSize)
    if (search.page > totalPages) {
      updateSearch({ page: totalPages })
    }
  }, [activePage, search.page, search.pageSize, updateSearch])

  const changeTab = (value: string) => {
    const tab: MyAffiliateTab =
      value === 'commissions' ? 'commissions' : 'invitees'
    setKeyword('')
    updateSearch({ tab, page: 1, keyword: '' })
  }
  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    updateSearch({ keyword: keyword.trim(), page: 1 })
  }

  const statsUnavailable =
    summaryQuery.isError ||
    rechargeTotalQuery.isError ||
    (summaryQuery.data != null && !summaryQuery.data.success) ||
    (rechargeTotalQuery.data != null && !rechargeTotalQuery.data.success)
  const transferAllowed =
    transferPolicyQuery.data?.data?.payment_compliance_confirmed !== false
  const stats = [
    {
      key: 'invites',
      label: t('Invites'),
      value: summary ? String(summary.aff_count) : '-',
    },
    {
      key: 'pending',
      label: t('Pending Rebate'),
      value: summary ? formatQuota(summary.aff_quota) : '-',
    },
    {
      key: 'total',
      label: t('Total Rebate'),
      value: summary ? formatQuota(summary.aff_history_quota) : '-',
    },
    {
      key: 'recharge',
      label: t('Referred Recharge Total'),
      value: rechargeTotal == null ? '-' : formatQuota(rechargeTotal),
    },
  ]

  let records: ReactNode
  if (activeQuery.isPending) {
    records = (
      <>
        <div className='hidden rounded-lg border md:block'>
          <Table>
            <TableBody>
              <LoadingRows columns={search.tab === 'invitees' ? 3 : 5} />
            </TableBody>
          </Table>
        </div>
        <MobileLoading />
      </>
    )
  } else if (activeQuery.isError || isInvalidResponse) {
    records = <ErrorState />
  } else if (!activePage || activePage.items.length === 0) {
    records = (
      <EmptyState
        message={
          search.tab === 'invitees'
            ? t('No invitation records')
            : t('No commission records')
        }
      />
    )
  } else if (search.tab === 'invitees' && inviteesQuery.data?.success) {
    records = <InviteeList items={inviteesQuery.data.data?.items ?? []} />
  } else if (search.tab === 'commissions' && commissionsQuery.data?.success) {
    records = <CommissionList items={commissionsQuery.data.data?.items ?? []} />
  } else {
    records = <ErrorState />
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('My Referrals')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-6xl flex-col gap-5'>
            <Card className='overflow-hidden border-amber-500/30 py-0 shadow-lg shadow-amber-500/10'>
              <CardContent className='p-0'>
                <div
                  className='flex min-w-0 flex-col gap-5 p-4 sm:p-6 lg:flex-row lg:items-stretch lg:gap-6'
                  style={{
                    background:
                      'linear-gradient(135deg, #1a1a1a 0%, #2d2410 40%, #1a1a1a 100%)',
                  }}
                >
                  <div className='flex min-w-0 flex-1 flex-col gap-4'>
                    <div className='space-y-1.5'>
                      <h3 className='text-base font-semibold text-amber-50'>
                        {t('Invite Link')}
                      </h3>
                      <p className='text-xs text-amber-200/60'>
                        {t(
                          'Share your invite link with friends. When they register and recharge, you earn commission rewards.'
                        )}
                      </p>
                    </div>

                    <div className='grid min-w-0 gap-3 sm:grid-cols-2'>
                      <div className='min-w-0 space-y-1.5'>
                        <label
                          className='text-xs font-medium text-amber-200/70'
                          htmlFor='affiliate-code'
                        >
                          {t('Affiliate Code')}
                        </label>
                        <div className='flex min-w-0 items-center gap-2'>
                          <Input
                            id='affiliate-code'
                            value={affiliateCode}
                            readOnly
                            className='h-9 min-w-0 flex-1 border-amber-500/20 bg-black/30 font-mono text-xs text-amber-50 placeholder:text-amber-200/30'
                          />
                          {affiliateCode ? (
                            <CopyButton
                              value={affiliateCode}
                              variant='outline'
                              className='size-9 border-amber-500/30 bg-amber-500/10 text-amber-100 hover:bg-amber-500/20 hover:text-amber-50'
                              iconClassName='size-4'
                              tooltip={t('Copy affiliate code')}
                              aria-label={t('Copy affiliate code')}
                            />
                          ) : null}
                        </div>
                      </div>

                      <div className='min-w-0 space-y-1.5'>
                        <label
                          className='text-xs font-medium text-amber-200/70'
                          htmlFor='affiliate-link'
                        >
                          {t('Invite Link')}
                        </label>
                        <div className='flex min-w-0 items-center gap-2'>
                          <Input
                            id='affiliate-link'
                            value={inviteLink}
                            readOnly
                            className='h-9 min-w-0 flex-1 border-amber-500/20 bg-black/30 font-mono text-xs text-amber-50 placeholder:text-amber-200/30'
                          />
                          {inviteLink ? (
                            <CopyButton
                              value={inviteLink}
                              variant='outline'
                              className='size-9 border-amber-500/30 bg-amber-500/10 text-amber-100 hover:bg-amber-500/20 hover:text-amber-50'
                              iconClassName='size-4'
                              tooltip={t('Copy referral link')}
                              aria-label={t('Copy referral link')}
                            />
                          ) : null}
                        </div>
                      </div>
                    </div>
                  </div>

                  <div className='grid min-w-0 grid-cols-2 gap-2 lg:w-80 lg:shrink-0 lg:border-l lg:border-amber-500/20 lg:pl-6'>
                    {stats.map(({ key, label, value }) => (
                      <div
                        key={key}
                        className='flex min-w-0 flex-col justify-center gap-1 rounded-lg bg-black/20 p-3 ring-1 ring-amber-500/10'
                      >
                        <div className='truncate text-xs font-medium text-amber-200/60'>
                          {label}
                        </div>
                        <div className='truncate text-lg font-semibold text-amber-50 tabular-nums'>
                          {value}
                        </div>
                        {key === 'pending' ? (
                          <Button
                            type='button'
                            size='sm'
                            onClick={() => setTransferDialogOpen(true)}
                            disabled={
                              !summary ||
                              summary.aff_quota <= 0 ||
                              !transferAllowed ||
                              transferring
                            }
                            className='mt-1 h-auto min-h-8 w-full bg-amber-500 px-2 py-1.5 text-xs whitespace-normal text-black hover:bg-amber-400'
                          >
                            <ArrowRightLeft
                              className='size-3.5 shrink-0'
                              aria-hidden='true'
                            />
                            <span>{t('Transfer to Balance')}</span>
                          </Button>
                        ) : null}
                      </div>
                    ))}
                    {statsUnavailable ? (
                      <p
                        className='col-span-2 text-xs text-amber-100/80'
                        role='status'
                      >
                        {t('Failed to load referral data')}
                      </p>
                    ) : null}
                    {!transferAllowed ? (
                      <p
                        className='col-span-2 text-xs text-amber-100/80'
                        role='status'
                      >
                        {t(
                          'Referral reward transfer is disabled until the administrator confirms compliance terms.'
                        )}
                      </p>
                    ) : null}
                  </div>
                </div>
              </CardContent>
            </Card>

            <section className='space-y-3'>
              <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                <Tabs value={search.tab} onValueChange={changeTab}>
                  <TabsList>
                    <TabsTrigger value='invitees'>
                      {t('My Invitees')}
                    </TabsTrigger>
                    <TabsTrigger value='commissions'>
                      {t('My Commissions')}
                    </TabsTrigger>
                  </TabsList>
                </Tabs>
                <form
                  onSubmit={submitSearch}
                  className='flex min-w-0 items-center gap-2'
                >
                  <Input
                    value={keyword}
                    onChange={(event) => setKeyword(event.target.value)}
                    aria-label={t('Search referrals')}
                    placeholder={
                      search.tab === 'invitees'
                        ? t('Search by username...')
                        : t('Search by username or order no...')
                    }
                    className='h-9 min-w-0 flex-1 sm:w-72'
                  />
                  <Button
                    type='submit'
                    variant='outline'
                    size='icon'
                    aria-label={t('Search')}
                    title={t('Search')}
                  >
                    <Search aria-hidden='true' />
                  </Button>
                </form>
              </div>

              {records}
              {activePage ? (
                <Pager
                  page={search.page}
                  pageSize={search.pageSize}
                  total={activePage.total}
                  onPageChange={(page) => updateSearch({ page })}
                />
              ) : null}
            </section>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <TransferDialog
        open={transferDialogOpen}
        onOpenChange={setTransferDialogOpen}
        onConfirm={transferQuota}
        availableQuota={summary?.aff_quota ?? 0}
        transferring={transferring}
      />
    </>
  )
}
