import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import {
  api,
  explorerAddr,
  explorerToken,
  explorerTx,
  shortAddr,
} from "./api";
import type { Collection, Status, Trade, Wallet } from "./api";
import "./index.css";

type Tab = "wallets" | "activity" | "collections";

export default function App() {
  const [tab, setTab] = useState<Tab>("wallets");
  const [status, setStatus] = useState<Status | null>(null);
  const [wallets, setWallets] = useState<Wallet[]>([]);
  const [trades, setTrades] = useState<Trade[]>([]);
  const [collections, setCollections] = useState<Collection[]>([]);
  const [error, setError] = useState("");
  const [toast, setToast] = useState("");
  const [busy, setBusy] = useState(false);
  const [q, setQ] = useState("");

  const [walletAddr, setWalletAddr] = useState("");
  const [walletLabel, setWalletLabel] = useState("");
  const [colAddr, setColAddr] = useState("");
  const [colName, setColName] = useState("");

  const notify = (msg: string) => {
    setToast(msg);
    window.setTimeout(() => setToast(""), 2400);
  };

  const refresh = useCallback(async () => {
    setError("");
    try {
      const [s, w, t, c] = await Promise.all([
        api.status(),
        api.wallets(),
        api.trades(120),
        api.collections(),
      ]);
      setStatus(s);
      setWallets(w);
      setTrades(t);
      setCollections(c);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load");
    }
  }, []);

  useEffect(() => {
    void refresh();
    const id = window.setInterval(() => void refresh(), 12000);
    return () => window.clearInterval(id);
  }, [refresh]);

  const filteredWallets = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return wallets;
    return wallets.filter(
      (w) =>
        w.address.includes(needle) ||
        w.label.toLowerCase().includes(needle) ||
        (w.collections ?? []).some((c) => c.toLowerCase().includes(needle)),
    );
  }, [wallets, q]);

  const filteredTrades = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return trades;
    return trades.filter(
      (t) =>
        t.wallet.includes(needle) ||
        t.collection.includes(needle) ||
        t.token_id.includes(needle) ||
        t.side.includes(needle),
    );
  }, [trades, q]);

  const filteredCollections = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return collections;
    return collections.filter(
      (c) => c.address.includes(needle) || c.name.toLowerCase().includes(needle),
    );
  }, [collections, q]);

  async function onAddWallet(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.addWallet({ address: walletAddr.trim(), label: walletLabel.trim() });
      setWalletAddr("");
      setWalletLabel("");
      notify("Wallet added to watch set");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "add failed");
    } finally {
      setBusy(false);
    }
  }

  async function onAddCollection(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.addCollection({ address: colAddr.trim(), name: colName.trim() });
      setColAddr("");
      setColName("");
      notify("Collection saved");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "add failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="app">
      <header className="hero">
        <h1 className="brand">
          bot<span>33</span>
        </h1>
        <p className="tagline">
          Robinhood Chain NFT smart-wallet watch. Track curated and discovered wallets,
          scan mint/buy flow, manage collections.
        </p>
        <div className="stats">
          <div className="stat">
            <span>Watching</span>
            <b>{status?.wallets_watching ?? "—"}</b>
          </div>
          <div className="stat">
            <span>Wallets</span>
            <b>{status?.wallets_total ?? "—"}</b>
          </div>
          <div className="stat">
            <span>NFT events</span>
            <b>{status?.trades_total ?? "—"}</b>
          </div>
          <div className="stat">
            <span>Cursor</span>
            <b>{status?.cursor_block ? status.cursor_block.toLocaleString() : "—"}</b>
          </div>
        </div>
      </header>

      <nav className="tabs">
        {(
          [
            ["wallets", "Wallets"],
            ["activity", "NFT activity"],
            ["collections", "Collections"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            className={`tab ${tab === id ? "active" : ""}`}
            onClick={() => setTab(id)}
            type="button"
          >
            {label}
          </button>
        ))}
        <button className="tab" type="button" onClick={() => void refresh()}>
          Refresh
        </button>
      </nav>

      <div className="panel" style={{ marginBottom: "0.85rem" }}>
        <input
          className="input"
          placeholder="Filter by address, label, collection…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        {error ? <p className="error">{error}</p> : null}
      </div>

      {tab === "wallets" ? (
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>Smart wallets</h2>
              <p>Add a curated address to the live watch set.</p>
            </div>
            <div className="toolbar">
              <button
                className="btn btn-ghost"
                type="button"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  try {
                    const r = await api.seedWallets();
                    notify(`Seeded ${r.loaded} wallets`);
                    await refresh();
                  } catch (err) {
                    setError(err instanceof Error ? err.message : "seed failed");
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                Reload seed
              </button>
            </div>
          </div>

          <form className="form" onSubmit={onAddWallet}>
            <input
              className="input"
              required
              placeholder="0x wallet address"
              value={walletAddr}
              onChange={(e) => setWalletAddr(e.target.value)}
            />
            <input
              className="input"
              placeholder="Label (optional)"
              value={walletLabel}
              onChange={(e) => setWalletLabel(e.target.value)}
              style={{ fontFamily: "var(--font-display)" }}
            />
            <button className="btn btn-primary" type="submit" disabled={busy}>
              Add wallet
            </button>
          </form>

          <div className="list">
            {filteredWallets.length === 0 ? (
              <div className="empty">No wallets yet. Add one or reload the seed.</div>
            ) : (
              filteredWallets.map((w) => (
                <article className="row" key={w.address}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className="label">{w.label || "unnamed"}</span>
                      <span className={`chip ${w.source}`}>{w.source}</span>
                      {!w.active ? <span className="chip blocked">paused</span> : null}
                      {w.score > 0 ? <span className="chip">score {Math.round(w.score)}</span> : null}
                    </div>
                    <a className="addr" href={explorerAddr(w.address)} target="_blank" rel="noreferrer">
                      {w.address}
                    </a>
                    <div className="meta">
                      {(w.collections ?? []).slice(0, 4).map((c) => (
                        <span className="chip" key={c}>
                          {c}
                        </span>
                      ))}
                    </div>
                  </div>
                  <div className="actions">
                    <button
                      className="btn btn-ghost"
                      type="button"
                      onClick={async () => {
                        await api.patchWallet(w.address, { active: !w.active });
                        notify(w.active ? "Paused" : "Resumed");
                        await refresh();
                      }}
                    >
                      {w.active ? "Pause" : "Resume"}
                    </button>
                    <button
                      className="btn btn-danger"
                      type="button"
                      onClick={async () => {
                        if (!window.confirm(`Remove ${shortAddr(w.address)}?`)) return;
                        await api.deleteWallet(w.address);
                        notify("Removed");
                        await refresh();
                      }}
                    >
                      Remove
                    </button>
                  </div>
                </article>
              ))
            )}
          </div>
        </section>
      ) : null}

      {tab === "activity" ? (
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>NFT activity</h2>
              <p>Mint / buy / sell events from watched wallets.</p>
            </div>
          </div>
          <div className="list">
            {filteredTrades.length === 0 ? (
              <div className="empty">
                No events yet. Keep the watcher running — activity appears as smart wallets move.
              </div>
            ) : (
              filteredTrades.map((t) => (
                <article className="row" key={`${t.tx_hash}-${t.id}`}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className={`chip ${t.side}`}>{t.side}</span>
                      <span className="label">#{t.token_id}</span>
                    </div>
                    <div className="addr">
                      wallet{" "}
                      <a href={explorerAddr(t.wallet)} target="_blank" rel="noreferrer">
                        {shortAddr(t.wallet)}
                      </a>
                      {" · "}
                      <a
                        href={explorerToken(t.collection, t.token_id)}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {shortAddr(t.collection)}
                      </a>
                    </div>
                    <div className="meta">
                      <span className="chip">block {t.block_number}</span>
                      <a className="chip" href={explorerTx(t.tx_hash)} target="_blank" rel="noreferrer">
                        tx {shortAddr(t.tx_hash)}
                      </a>
                      <span className="chip">{new Date(t.created_at).toLocaleString()}</span>
                    </div>
                  </div>
                </article>
              ))
            )}
          </div>
        </section>
      ) : null}

      {tab === "collections" ? (
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>Collections</h2>
              <p>Known Robinhood NFT contracts for research and context.</p>
            </div>
            <div className="toolbar">
              <button
                className="btn btn-ghost"
                type="button"
                disabled={busy}
                onClick={async () => {
                  setBusy(true);
                  try {
                    const r = await api.seedCollections();
                    notify(`Loaded ${r.loaded} collections`);
                    await refresh();
                  } catch (err) {
                    setError(err instanceof Error ? err.message : "seed failed");
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                Load defaults
              </button>
            </div>
          </div>

          <form className="form" onSubmit={onAddCollection}>
            <input
              className="input"
              required
              placeholder="0x collection address"
              value={colAddr}
              onChange={(e) => setColAddr(e.target.value)}
            />
            <input
              className="input"
              placeholder="Name"
              value={colName}
              onChange={(e) => setColName(e.target.value)}
              style={{ fontFamily: "var(--font-display)" }}
            />
            <button className="btn btn-primary" type="submit" disabled={busy}>
              Add collection
            </button>
          </form>

          <div className="list">
            {filteredCollections.length === 0 ? (
              <div className="empty">No collections yet. Load defaults or add one.</div>
            ) : (
              filteredCollections.map((c) => (
                <article className="row" key={c.address}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className="label">{c.name || "unnamed collection"}</span>
                    </div>
                    <a className="addr" href={explorerAddr(c.address)} target="_blank" rel="noreferrer">
                      {c.address}
                    </a>
                  </div>
                  <div className="actions">
                    <button
                      className="btn btn-danger"
                      type="button"
                      onClick={async () => {
                        await api.deleteCollection(c.address);
                        notify("Collection removed");
                        await refresh();
                      }}
                    >
                      Remove
                    </button>
                  </div>
                </article>
              ))
            )}
          </div>
        </section>
      ) : null}

      {toast ? <div className="toast">{toast}</div> : null}
    </div>
  );
}
