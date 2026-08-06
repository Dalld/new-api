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
  CircleCheck,
  Gift,
  Handshake,
  Info,
  Percent,
  ReceiptText,
  RefreshCw,
  Search,
  Share2,
  TrendingUp,
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
  getSelfAffiliateOverview,
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

function formatCommissionRate(rate: number) {
  return new Intl.NumberFormat(undefined, {
    style: 'percent',
    maximumFractionDigits: 2,
  }).format(rate)
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
                <TableHead>{t('User ID')}</TableHead>
                <TableHead>{t('Username')}</TableHead>
                <TableHead>{t('Registered At')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='font-mono text-xs'>{item.id}</TableCell>
                <TableCell className='font-medium'>
                  {item.masked_username || '-'}
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
                <Users
                  className='text-muted-foreground size-4 shrink-0'
                  aria-hidden='true'
                />
                <span className='truncate font-medium'>
                  {item.masked_username || '-'}
                </span>
              </div>
              <dl className='grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs'>
                <dt className='text-muted-foreground'>{t('User ID')}</dt>
                <dd className='truncate text-right font-mono'>{item.id}</dd>
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
  const overviewQuery = useQuery({
    queryKey: ['my-affiliate', 'overview'],
    queryFn: getSelfAffiliateOverview,
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
  const overview = overviewQuery.data?.success
    ? overviewQuery.data.data
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
    overviewQuery.isError ||
    summaryQuery.isError ||
    rechargeTotalQuery.isError ||
    (overviewQuery.data != null && !overviewQuery.data.success) ||
    (summaryQuery.data != null && !summaryQuery.data.success) ||
    (rechargeTotalQuery.data != null && !rechargeTotalQuery.data.success)
  const transferAllowed =
    transferPolicyQuery.data?.data?.payment_compliance_confirmed !== false
  const stats = [
    {
      key: 'invites',
      label: t('Total Invites'),
      value: overview ? String(overview.invitee_count) : '-',
      description: t('Users registered through your referral link'),
      icon: Users,
      className: 'border-blue-500/30',
    },
    {
      key: 'recharge',
      label: t('Referred Recharge Total'),
      value: rechargeTotal == null ? '-' : formatQuota(rechargeTotal),
      description: t('Total recharge amount from your invitees'),
      icon: CircleCheck,
      className: 'border-emerald-500/30',
    },
    {
      key: 'pending',
      label: t('Pending Transfer'),
      value: summary ? formatQuota(summary.aff_quota) : '-',
      description: t('Rewards available to transfer to your balance'),
      icon: Gift,
      className: 'border-amber-500/40',
    },
    {
      key: 'total',
      label: t('Total Rebate'),
      value: summary ? formatQuota(summary.aff_history_quota) : '-',
      description: t('Total rewards accumulated from your referrals'),
      icon: TrendingUp,
      className: 'border-teal-500/30',
    },
  ]

  const isRefreshing =
    codeQuery.isFetching ||
    overviewQuery.isFetching ||
    summaryQuery.isFetching ||
    rechargeTotalQuery.isFetching ||
    inviteesQuery.isFetching ||
    commissionsQuery.isFetching

  const handleRefresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['my-affiliate'] })
  }

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

  let rewardContent: ReactNode
  if (overview?.payment_compliance_confirmed) {
    rewardContent = (
      <>
        <div className='mt-3 grid gap-3 sm:grid-cols-2'>
          <div className='rounded-md border border-sky-200 bg-sky-50 p-3 dark:border-sky-900/60 dark:bg-sky-950/30'>
            <div className='flex items-center gap-2 text-sky-700 dark:text-sky-300'>
              <Percent aria-hidden='true' className='size-4' />
              <p className='text-xs font-semibold uppercase tracking-wide'>
                {t('Actual Recharge Rebate')}
              </p>
            </div>
            <p className='mt-1 text-2xl font-bold tabular-nums text-sky-700 dark:text-sky-300'>
              {formatCommissionRate(overview.commission_rate)}
            </p>
            <p className='text-muted-foreground mt-1 text-xs leading-4'>
              {t("Based on invitee's actual recharge amount")}
            </p>
          </div>
          <div className='rounded-md border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/60 dark:bg-amber-950/30'>
            <div className='flex items-center gap-2 text-amber-700 dark:text-amber-300'>
              <Gift aria-hidden='true' className='size-4' />
              <p className='text-xs font-semibold uppercase tracking-wide'>
                {t('Signup Bonus')}
              </p>
            </div>
            <div className='mt-2 grid grid-cols-2 gap-3'>
              <div>
                <p className='text-muted-foreground text-xs'>{t('Inviter')}</p>
                <p className='mt-0.5 text-lg font-bold tabular-nums text-amber-700 dark:text-amber-300'>
                  {formatQuota(overview.inviter_signup_reward_quota)}
                </p>
              </div>
              <div>
                <p className='text-muted-foreground text-xs'>{t('Invitee')}</p>
                <p className='mt-0.5 text-lg font-bold tabular-nums text-amber-700 dark:text-amber-300'>
                  {formatQuota(overview.invitee_signup_reward_quota)}
                </p>
              </div>
            </div>
          </div>
        </div>
        <p className='text-muted-foreground mt-2 text-xs leading-4'>
          {t(
            "Invitee actual recharge reward: {{rate}} of the invitee's actual recharge amount.",
            { rate: formatCommissionRate(overview.commission_rate) }
          )}
        </p>
      </>
    )
  } else if (overview) {
    rewardContent = (
      <p className='text-muted-foreground mt-1'>
        {t(
          'Referral rewards are currently disabled until payment compliance is confirmed.'
        )}
      </p>
    )
  } else {
    rewardContent = <p className='text-muted-foreground mt-1'>-</p>
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          <span className='flex items-center gap-2'>
            <Handshake aria-hidden='true' className='size-5' />
            {t('Referral Program')}
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleRefresh}
            disabled={isRefreshing}
            aria-label={t('Refresh')}
            title={t('Refresh')}
          >
            <RefreshCw
              aria-hidden='true'
              className={isRefreshing ? 'animate-spin' : undefined}
            />
            <span>{t('Refresh')}</span>
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex w-full flex-col gap-5'>
            <div
              className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'
              aria-busy={isRefreshing}
            >
              {stats.map(
                ({ key, label, value, description, icon: Icon, className }) => (
                  <Card key={key} className={`min-w-0 ${className}`}>
                    <CardContent className='flex min-h-36 flex-col justify-between gap-3 p-4'>
                      <div className='flex items-start justify-between gap-3'>
                        <span className='text-muted-foreground text-xs font-medium sm:text-sm'>
                          {label}
                        </span>
                        <Icon
                          className='text-muted-foreground size-4 shrink-0 sm:size-5'
                          aria-hidden='true'
                        />
                      </div>
                      <div className='space-y-2'>
                        <div className='text-2xl font-semibold tabular-nums sm:text-3xl'>
                          {value}
                        </div>
                        <p className='text-muted-foreground text-xs leading-5 sm:text-sm'>
                          {description}
                        </p>
                        {key === 'pending' ? (
                          <Button
                            type='button'
                            variant='default'
                            className='w-full bg-amber-500 text-white shadow-sm hover:bg-amber-600 sm:w-auto dark:bg-amber-500 dark:hover:bg-amber-400'
                            size='sm'
                            onClick={() => setTransferDialogOpen(true)}
                            disabled={
                              !summary ||
                              summary.aff_quota <= 0 ||
                              !transferAllowed ||
                              transferring
                            }
                          >
                            <ArrowRightLeft aria-hidden='true' />
                            {t('Transfer to Balance')}
                          </Button>
                        ) : null}
                      </div>
                    </CardContent>
                  </Card>
                )
              )}
            </div>

            {statsUnavailable ? (
              <p className='text-destructive text-sm' role='status'>
                {t('Failed to load referral data')}
              </p>
            ) : null}

            <Card>
              <CardContent className='space-y-4 p-4 sm:p-5'>
                <div className='flex items-start gap-3'>
                  <Share2
                    className='mt-0.5 size-4 shrink-0 sm:size-5'
                    aria-hidden='true'
                  />
                  <div>
                    <h2 className='text-base font-semibold sm:text-lg'>
                      {t('Your Referral Link')}
                    </h2>
                    <p className='text-muted-foreground mt-1 text-xs sm:text-sm'>
                      {t('Share this link with friends to earn rewards')}
                    </p>
                  </div>
                </div>
                <div className='flex min-w-0 items-center gap-2'>
                  <Input
                    id='affiliate-link'
                    value={inviteLink}
                    readOnly
                    aria-label={t('Your Referral Link')}
                    className='h-9 min-w-0 flex-1 font-mono text-xs sm:text-sm'
                  />
                  {inviteLink ? (
                    <CopyButton
                      value={inviteLink}
                      variant='outline'
                      className='size-9 shrink-0'
                      iconClassName='size-4'
                      tooltip={t('Copy referral link')}
                      aria-label={t('Copy referral link')}
                    />
                  ) : null}
                </div>
                <div className='bg-muted/30 flex items-start gap-3 rounded-lg border p-3 sm:p-4'>
                  <Info
                    className='text-muted-foreground mt-0.5 size-4 shrink-0 sm:size-5'
                    aria-hidden='true'
                  />
                  <div className='min-w-0 flex-1 text-sm'>
                    <p className='font-medium'>{t('Referral Reward')}</p>
                    {rewardContent}
                  </div>
                </div>
                {!transferAllowed ? (
                  <p className='text-muted-foreground text-xs' role='status'>
                    {t(
                      'Referral reward transfer is disabled until the administrator confirms compliance terms.'
                    )}
                  </p>
                ) : null}
              </CardContent>
            </Card>

            <Card>
              <CardContent className='space-y-4 p-4 sm:p-5'>
                <div className='flex items-start gap-3'>
                  <Gift className='mt-0.5 size-4 shrink-0 sm:size-5' aria-hidden='true' />
                  <div>
                    <h2 className='text-base font-semibold sm:text-lg'>
                      {t('Reward History')}
                    </h2>
                    <p className='text-muted-foreground mt-1 text-xs sm:text-sm'>
                      {t('Your referral records and reward status')}
                    </p>
                  </div>
                </div>
                <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                  <Tabs value={search.tab} onValueChange={changeTab}>
                    <TabsList className='grid w-full grid-cols-2 sm:w-auto'>
                      <TabsTrigger
                        value='invitees'
                        className='min-w-0 px-2 text-xs sm:text-sm'
                      >
                        {t('Invitees')}
                      </TabsTrigger>
                      <TabsTrigger
                        value='commissions'
                        className='min-w-0 px-2 text-xs sm:text-sm'
                      >
                        {t('Commission Records')}
                      </TabsTrigger>
                    </TabsList>
                  </Tabs>
                  <form
                    onSubmit={submitSearch}
                    className='flex w-full min-w-0 items-center gap-2 sm:w-auto'
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
                      className='h-9 min-w-0 flex-1 sm:w-64'
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

                <div className='min-h-[16rem]'>{records}</div>
                {activePage ? (
                  <Pager
                    page={search.page}
                    pageSize={search.pageSize}
                    total={activePage.total}
                    onPageChange={(page) => updateSearch({ page })}
                  />
                ) : null}
              </CardContent>
            </Card>
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
