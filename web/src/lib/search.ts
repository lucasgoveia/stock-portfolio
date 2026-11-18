// Search params shared between pages, so one page can open another prefilled.

export interface TradeSearch {
  asset?: number
  side?: 'buy' | 'sell'
  quantity?: string
  price?: string
  /** Prefill the whole position as the sell quantity. */
  full?: boolean
  tab?: 'trades' | 'cash' | 'positions' | 'dividends' | 'actions'
}

const decimalString = (v: unknown) =>
  (typeof v === 'string' || typeof v === 'number') && /^\d+(\.\d+)?$/.test(String(v)) ? String(v) : undefined

export function parseTradeSearch(s: Record<string, unknown>): TradeSearch {
  return {
    asset: Number(s.asset) > 0 ? Number(s.asset) : undefined,
    side: s.side === 'buy' || s.side === 'sell' ? s.side : undefined,
    quantity: decimalString(s.quantity),
    price: decimalString(s.price),
    full: s.full === true || s.full === 'true' ? true : undefined,
    tab: s.tab === 'cash' || s.tab === 'positions' || s.tab === 'trades' || s.tab === 'dividends' || s.tab === 'actions'
        ? s.tab
        : undefined,
  }
}

export type AssetTab = 'overview' | 'ratings' | 'thesis' | 'valuation' | 'fundamentals' | 'notes' | 'analyst' | 'trades'
const ASSET_TABS: AssetTab[] = ['overview', 'ratings', 'thesis', 'valuation', 'fundamentals', 'notes', 'analyst', 'trades']

export interface AssetSearch {
  tab?: AssetTab
}

export function parseAssetSearch(s: Record<string, unknown>): AssetSearch {
  return { tab: ASSET_TABS.includes(s.tab as AssetTab) ? (s.tab as AssetTab) : undefined }
}
