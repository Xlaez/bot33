const API_BASE = import.meta.env.VITE_API_BASE ?? "/api";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const body = await res.json();
      msg = body.error || msg;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return res.json() as Promise<T>;
}

export type Status = {
  wallets_total: number;
  wallets_watching: number;
  trades_total: number;
  collections: number;
  cursor_block: number;
  chain_id: number;
};

export type Wallet = {
  address: string;
  label: string;
  source: string;
  tags?: string[];
  collections?: string[];
  evidence?: string[];
  score: number;
  active: boolean;
  updated_at?: string;
};

export type Trade = {
  id: number;
  wallet: string;
  collection: string;
  token_id: string;
  side: string;
  tx_hash: string;
  block_number: number;
  value_wei: string;
  created_at: string;
};

export type Collection = {
  address: string;
  name: string;
  notes: string;
  active: boolean;
  updated_at?: string;
};

export type Settings = {
  max_spend_wei: string;
  max_spend_eth?: string;
  execute_live: boolean;
  auto_copy_mint: boolean;
  mint_quantity: number;
  mint_max_wallets?: number;
  mint_max_total?: number;
  smart_wallet_min?: number;
  smart_mint_window_min?: number;
  smart_buy_window_min?: number;
  new_collection_max_age_h?: number;
  meme_max_spend_wei?: string;
  meme_max_spend_eth?: string;
  meme_execute_live?: boolean;
  meme_auto_buy?: boolean;
  meme_slippage_bps?: number;
  has_signer?: boolean;
  signer_address?: string;
  signer_addresses?: string[];
  signer_count?: number;
};

export type MintOrder = {
  id: number;
  source: string;
  collection: string;
  quantity: number;
  value_wei: string;
  fee_recipient: string;
  signal_tx: string;
  tx_hash: string;
  status: string;
  error: string;
  dry_run: boolean;
  signer?: string;
  created_at: string;
};

export type MemeToken = {
  address: string;
  symbol: string;
  name: string;
  decimals: number;
  paired_with: string;
  pool_address: string;
  dex: string;
  fee_tier: number;
  launch_tx: string;
  launch_block: number;
  first_liquidity_at?: string;
  lp_locked: boolean;
  lp_lock_pct: number;
  lp_lock_evidence: string;
  owner_renounced: boolean;
  score: number;
  flags: string[];
  status: string;
  volume_wei: string;
  swap_count: number;
  unique_traders: number;
  last_swap_at?: string;
  alerted_at?: string;
  updated_at?: string;
  created_at?: string;
};

export type MemeStats = {
  total: number;
  lp_locked: number;
  alerted: number;
};

export type MemeOrder = {
  id: number;
  source: string;
  token: string;
  pool_address: string;
  dex: string;
  spend_wei: string;
  min_out_wei: string;
  tx_hash: string;
  status: string;
  error: string;
  dry_run: boolean;
  signal_tx: string;
  created_at: string;
};


export const api = {
  status: () => req<Status>("/status"),
  wallets: () => req<Wallet[]>("/wallets"),
  addWallet: (body: { address: string; label?: string }) =>
    req<Wallet>("/wallets", { method: "POST", body: JSON.stringify(body) }),
  patchWallet: (address: string, body: { active?: boolean; label?: string }) =>
    req<void>(`/wallets/${address}`, { method: "PATCH", body: JSON.stringify(body) }),
  deleteWallet: (address: string) =>
    req<void>(`/wallets/${address}`, { method: "DELETE" }),
  seedWallets: () => req<{ loaded: number }>("/wallets/seed", { method: "POST" }),
  trades: (limit = 100, scope: "watched" | "all" = "watched") =>
    req<Trade[]>(`/trades?limit=${limit}&scope=${scope}`),
  collections: () => req<Collection[]>("/collections"),
  addCollection: (body: { address: string; name?: string; notes?: string }) =>
    req<Collection>("/collections", { method: "POST", body: JSON.stringify(body) }),
  deleteCollection: (address: string) =>
    req<void>(`/collections/${address}`, { method: "DELETE" }),
  seedCollections: () => req<{ loaded: number }>("/collections/seed", { method: "POST" }),
  settings: () => req<Settings>("/settings"),
  saveSettings: (body: Partial<Settings> & { max_spend_eth?: string; meme_max_spend_eth?: string }) =>
    req<Settings>("/settings", { method: "PUT", body: JSON.stringify(body) }),
  orders: (limit = 50) => req<MintOrder[]>(`/orders?limit=${limit}`),
  mint: (body: { collection: string; quantity?: number; wallet_count?: number; sweep?: boolean }) =>
    req<{ queued: boolean }>("/mint", { method: "POST", body: JSON.stringify(body) }),
  memes: (limit = 100) => req<MemeToken[]>(`/memes?limit=${limit}`),
  memeStats: () => req<MemeStats>("/memes/stats"),
  memeOrders: (limit = 50) => req<MemeOrder[]>(`/memes/orders?limit=${limit}`),
  memeBuy: (token: string) =>
    req<{ queued: boolean; token: string }>("/memes/buy", {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
};

export function shortAddr(a: string) {
  if (!a || a.length < 12) return a;
  return `${a.slice(0, 6)}…${a.slice(-4)}`;
}

export function explorerTx(hash: string) {
  return `https://robinhoodchain.blockscout.com/tx/${hash}`;
}

export function explorerAddr(addr: string) {
  return `https://robinhoodchain.blockscout.com/address/${addr}`;
}

export function explorerToken(collection: string, tokenId: string) {
  return `https://robinhoodchain.blockscout.com/token/${collection}/instance/${tokenId}`;
}
