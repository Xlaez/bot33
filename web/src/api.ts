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
  trades: (limit = 100) => req<Trade[]>(`/trades?limit=${limit}`),
  collections: () => req<Collection[]>("/collections"),
  addCollection: (body: { address: string; name?: string; notes?: string }) =>
    req<Collection>("/collections", { method: "POST", body: JSON.stringify(body) }),
  deleteCollection: (address: string) =>
    req<void>(`/collections/${address}`, { method: "DELETE" }),
  seedCollections: () => req<{ loaded: number }>("/collections/seed", { method: "POST" }),
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
