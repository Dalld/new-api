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
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, RefreshCw, Search } from 'lucide-react'
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
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

import { getAffiliateRelations, getCommissionRecords } from './api'
import { formatCommissionRate, getAdminTotalPages } from './lib'
import type {
  AffiliateRelation,
  AffiliateSearchState,
  AffiliateTab,
  CommissionRecordDetail,
} from './types'

interface AffiliateProps {
  search: AffiliateSearchState
  onSearchChange: (next: AffiliateSearchState) => void
}

function formatTime(timestamp: number) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(timestamp * 1000))
}

function LoadingTable({ columns }: { columns: number }) {
  return (
    <div className='rounded-lg border'>
      <Table>
        <TableBody>
          {Array.from({ length: 5 }, (_, index) => (
            <TableRow key={index}>
              <TableCell colSpan={columns}>
                <Skeleton className='h-5 w-full' />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function Relations({ items }: { items: AffiliateRelation[] }) {
  const { t } = useTranslation()
  return (
    <>
      <div className='hidden overflow-x-auto rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('ID')}</TableHead>
              <TableHead>{t('Username')}</TableHead>
              <TableHead>{t('Display Name')}</TableHead>
              <TableHead>{t('Invitee Count')}</TableHead>
              <TableHead>{t('Pending Rebate')}</TableHead>
              <TableHead>{t('Total Rebate')}</TableHead>
              <TableHead>{t('Registered At')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.inviter_id}>
                <TableCell>{item.inviter_id}</TableCell>
                <TableCell className='font-medium'>
                  {item.username || '-'}
                </TableCell>
                <TableCell>{item.display_name || '-'}</TableCell>
                <TableCell>{item.invitee_count}</TableCell>
                <TableCell>{formatQuota(item.aff_quota)}</TableCell>
                <TableCell>{formatQuota(item.aff_history_quota)}</TableCell>
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
          <Card key={item.inviter_id} size='sm' className='rounded-lg'>
            <CardContent className='space-y-2'>
              <div className='flex justify-between gap-3'>
                <span className='truncate font-medium'>
                  {item.username || '-'}
                </span>
                <span className='text-muted-foreground'>
                  #{item.inviter_id}
                </span>
              </div>
              <dl className='grid grid-cols-2 gap-1 text-xs'>
                <dt className='text-muted-foreground'>{t('Invitee Count')}</dt>
                <dd className='text-right'>{item.invitee_count}</dd>
                <dt className='text-muted-foreground'>{t('Pending Rebate')}</dt>
                <dd className='text-right'>{formatQuota(item.aff_quota)}</dd>
                <dt className='text-muted-foreground'>{t('Total Rebate')}</dt>
                <dd className='text-right'>
                  {formatQuota(item.aff_history_quota)}
                </dd>
              </dl>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  )
}

function Commissions({ items }: { items: CommissionRecordDetail[] }) {
  const { t } = useTranslation()
  return (
    <>
      <div className='hidden overflow-x-auto rounded-lg border md:block'>
        <Table className='min-w-[1100px]'>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Order No.')}</TableHead>
              <TableHead>{t('Inviter')}</TableHead>
              <TableHead>{t('Invitee')}</TableHead>
              <TableHead>{t('Payment Provider')}</TableHead>
              <TableHead>{t('Paid Amount')}</TableHead>
              <TableHead>{t('Commission Rate')}</TableHead>
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
                <TableCell>
                  {item.inviter_username || '-'}{' '}
                  <span className='text-muted-foreground text-xs'>
                    #{item.inviter_id}
                  </span>
                </TableCell>
                <TableCell>
                  {item.invitee_username || '-'}{' '}
                  <span className='text-muted-foreground text-xs'>
                    #{item.invitee_id}
                  </span>
                </TableCell>
                <TableCell>
                  <Badge variant='outline'>
                    {item.payment_provider || '-'}
                  </Badge>
                </TableCell>
                <TableCell>{item.paid_money || '-'}</TableCell>
                <TableCell>
                  {formatCommissionRate(item.commission_rate)}
                </TableCell>
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
              <div className='flex items-center justify-between gap-2'>
                <span className='truncate font-mono text-xs'>
                  {item.order_no || '-'}
                </span>
                <Badge variant='outline'>{item.payment_provider || '-'}</Badge>
              </div>
              <dl className='grid grid-cols-2 gap-1 text-xs'>
                <dt className='text-muted-foreground'>{t('Inviter')}</dt>
                <dd className='truncate text-right'>
                  {item.inviter_username || '-'}
                </dd>
                <dt className='text-muted-foreground'>{t('Invitee')}</dt>
                <dd className='truncate text-right'>
                  {item.invitee_username || '-'}
                </dd>
                <dt className='text-muted-foreground'>
                  {t('Commission Rate')}
                </dt>
                <dd className='text-right'>
                  {formatCommissionRate(item.commission_rate)}
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

export function Affiliate({ search, onSearchChange }: AffiliateProps) {
  const { t } = useTranslation()
  const [keyword, setKeyword] = useState(search.keyword)
  useEffect(() => setKeyword(search.keyword), [search.keyword])

  const relationsQuery = useQuery({
    queryKey: [
      'affiliate',
      'relations',
      search.page,
      search.pageSize,
      search.keyword,
    ],
    queryFn: () => getAffiliateRelations(search),
    enabled: search.tab === 'relations',
    placeholderData: (previousData) => previousData,
  })
  const commissionsQuery = useQuery({
    queryKey: [
      'affiliate',
      'commissions',
      search.page,
      search.pageSize,
      search.keyword,
    ],
    queryFn: () => getCommissionRecords(search),
    enabled: search.tab === 'commissions',
    placeholderData: (previousData) => previousData,
  })
  const activeQuery =
    search.tab === 'relations' ? relationsQuery : commissionsQuery
  const pageData = activeQuery.data?.success ? activeQuery.data.data : undefined
  const totalPages = getAdminTotalPages(pageData?.total ?? 0, search.pageSize)
  const update = (patch: Partial<AffiliateSearchState>) =>
    onSearchChange({ ...search, ...patch })
  const changeTab = (value: string) => {
    const tab: AffiliateTab =
      value === 'commissions' ? 'commissions' : 'relations'
    setKeyword('')
    update({ tab, page: 1, keyword: '' })
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    update({ page: 1, keyword: keyword.trim() })
  }

  let content: ReactNode
  if (activeQuery.isPending) {
    content = <LoadingTable columns={search.tab === 'relations' ? 7 : 9} />
  } else if (activeQuery.isError || !activeQuery.data?.success) {
    content = (
      <div
        role='alert'
        className='flex min-h-36 flex-col items-center justify-center gap-3 border-y px-4 text-center text-sm'
      >
        <span className='text-destructive'>
          {t('Failed to load affiliate data')}
        </span>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={activeQuery.isFetching}
          onClick={() => void activeQuery.refetch()}
        >
          <RefreshCw aria-hidden='true' />
          {t('Retry')}
        </Button>
      </div>
    )
  } else if (!pageData?.items.length) {
    content = (
      <div className='text-muted-foreground flex min-h-36 items-center justify-center border-y text-sm'>
        {t('No data')}
      </div>
    )
  } else if (search.tab === 'relations' && relationsQuery.data?.success) {
    content = <Relations items={relationsQuery.data.data?.items ?? []} />
  } else if (search.tab === 'commissions' && commissionsQuery.data?.success) {
    content = <Commissions items={commissionsQuery.data.data?.items ?? []} />
  } else {
    content = null
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Affiliate Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-3'>
          <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
            <Tabs value={search.tab} onValueChange={changeTab}>
              <TabsList>
                <TabsTrigger value='relations'>
                  {t('Referral Summary')}
                </TabsTrigger>
                <TabsTrigger value='commissions'>
                  {t('Commission Details')}
                </TabsTrigger>
              </TabsList>
            </Tabs>
            <form onSubmit={submit} className='flex min-w-0 items-center gap-2'>
              <Input
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                aria-label={t('Search affiliate records')}
                placeholder={t('Search by username or order no...')}
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
          {content}
          <div className='flex items-center justify-between gap-3 border-t pt-3'>
            <span className='text-muted-foreground text-sm'>
              {t('Total: {{count}}', { count: pageData?.total ?? 0 })}
            </span>
            <div className='flex items-center gap-1.5'>
              <Button
                variant='outline'
                size='icon'
                disabled={search.page <= 1}
                aria-label={t('Previous page')}
                title={t('Previous page')}
                onClick={() => update({ page: Math.max(1, search.page - 1) })}
              >
                <ChevronLeft aria-hidden='true' />
              </Button>
              <span className='text-muted-foreground min-w-20 text-center text-sm tabular-nums'>
                {t('{{current}} / {{total}}', {
                  current: search.page,
                  total: totalPages,
                })}
              </span>
              <Button
                variant='outline'
                size='icon'
                disabled={search.page >= totalPages}
                aria-label={t('Next page')}
                title={t('Next page')}
                onClick={() =>
                  update({ page: Math.min(totalPages, search.page + 1) })
                }
              >
                <ChevronRight aria-hidden='true' />
              </Button>
            </div>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
