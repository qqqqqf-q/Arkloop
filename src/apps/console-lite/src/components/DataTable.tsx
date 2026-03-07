import type { ReactNode } from 'react'
import { EmptyState } from './EmptyState'

export type Column<T> = {
  key: string
  header: string
  render: (row: T) => ReactNode
  headerClassName?: string
  cellClassName?: string
}

type Props<T> = {
  columns: Column<T>[]
  data: T[]
  rowKey: (row: T) => string
  loading?: boolean
  loadingLabel?: string
  emptyMessage?: string
  emptyIcon?: ReactNode
  onRowClick?: (row: T) => void
  activeRowKey?: string
  tableClassName?: string
  headerCellClassName?: string
  bodyCellClassName?: string
  rowClassName?: string
}

export function DataTable<T>({
  columns,
  data,
  rowKey,
  loading = false,
  loadingLabel = 'Loading...',
  emptyMessage,
  emptyIcon,
  onRowClick,
  activeRowKey,
  tableClassName,
  headerCellClassName,
  bodyCellClassName,
  rowClassName,
}: Props<T>) {
  if (loading) {
    return (
      <div className="flex flex-1 items-center justify-center py-16">
        <p className="text-sm text-[var(--c-text-muted)]">{loadingLabel}</p>
      </div>
    )
  }

  if (data.length === 0) {
    return <EmptyState icon={emptyIcon} message={emptyMessage} />
  }

  return (
    <div className="overflow-auto">
      <table className={['w-full text-left text-sm', tableClassName ?? ''].join(' ')}>
        <thead>
          <tr className="border-b border-[var(--c-border-console)]">
            {columns.map((col) => (
              <th
                key={col.key}
                className={[
                  'whitespace-nowrap px-4 py-2.5 text-xs font-medium text-[var(--c-text-muted)]',
                  headerCellClassName ?? '',
                  col.headerClassName ?? '',
                ].join(' ')}
              >
                {col.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((row) => {
            const key = rowKey(row)
            const isActive = activeRowKey === key
            return (
              <tr
                key={key}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                className={[
                  'border-b border-[var(--c-border-console)] transition-colors',
                  onRowClick ? 'cursor-pointer' : '',
                  isActive ? 'bg-[var(--c-bg-sub)]' : 'hover:bg-[var(--c-bg-sub)]',
                  rowClassName ?? '',
                ].join(' ')}
              >
                {columns.map((col) => (
                  <td
                    key={col.key}
                    className={[
                      'px-4 py-2.5 text-[var(--c-text-secondary)]',
                      bodyCellClassName ?? '',
                      col.cellClassName ?? 'whitespace-nowrap',
                    ].join(' ')}
                  >
                    {col.render(row)}
                  </td>
                ))}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
