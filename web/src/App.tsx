import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import {
  api,
  explorerAddr,
  explorerToken,
  explorerTx,
  shortAddr,
} from "./api";
import type { Collection, MemeOrder, MemeStats, MemeToken, MintOrder, Settings, Status, Trade, Wallet } from "./api";
import "./index.css";

type Tab = "wallets" | "activity" | "collections" | "memes" | "execute";

export default function App() {
  const [tab, setTab] = useState<Tab>("wallets");
  const [status, setStatus] = useState<Status | null>(null);
  const [wallets, setWallets] = useState<Wallet[]>([]);
  const [trades, setTrades] = useState<Trade[]>([]);
  const [collections, setCollections] = useState<Collection[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [orders, setOrders] = useState<MintOrder[]>([]);
  const [memes, setMemes] = useState<MemeToken[]>([]);
  const [memeStats, setMemeStats] = useState<MemeStats | null>(null);
  const [memeOrders, setMemeOrders] = useState<MemeOrder[]>([]);
  const [error, setError] = useState("");
  const [toast, setToast] = useState("");
  const [busy, setBusy] = useState(false);
  const [q, setQ] = useState("");

  const [walletAddr, setWalletAddr] = useState("");
  const [walletLabel, setWalletLabel] = useState("");
  const [colAddr, setColAddr] = useState("");
  const [colName, setColName] = useState("");
  const [maxSpendEth, setMaxSpendEth] = useState("0.05");
  const [memeMaxSpendEth, setMemeMaxSpendEth] = useState("0.02");
  const [mintCollection, setMintCollection] = useState("");
  const [mintQty, setMintQty] = useState("1");
  const [sweepWallets, setSweepWallets] = useState("3");
  const [manualMemeToken, setManualMemeToken] = useState("");

  const notify = (msg: string) => {
    setToast(msg);
    window.setTimeout(() => setToast(""), 2400);
  };

  const refresh = useCallback(async () => {
    setError("");
    try {
      const [s, w, t, c, set, o, m, ms, mo] = await Promise.all([
        api.status(),
        api.wallets(),
        api.trades(120),
        api.collections(),
        api.settings(),
        api.orders(40),
        api.memes(120),
        api.memeStats(),
        api.memeOrders(40),
      ]);
      setStatus(s);
      setWallets(w);
      setTrades(t);
      setCollections(c);
      setSettings(set);
      setOrders(o);
      setMemes(m);
      setMemeStats(ms);
      setMemeOrders(mo);
      if (set.max_spend_eth) setMaxSpendEth(set.max_spend_eth);
      if (set.meme_max_spend_eth) setMemeMaxSpendEth(set.meme_max_spend_eth);
      if (set.mint_max_wallets) setSweepWallets(String(set.mint_max_wallets));
      if (set.mint_quantity) setMintQty(String(set.mint_quantity));
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load");
    }
  }, []);

  useEffect(() => {
    void refresh();
    const id = window.setInterval(() => void refresh(), 10000);
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

  const filteredMemes = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return memes;
    return memes.filter(
      (m) =>
        m.address.includes(needle) ||
        m.symbol.toLowerCase().includes(needle) ||
        m.name.toLowerCase().includes(needle) ||
        m.dex.includes(needle),
    );
  }, [memes, q]);

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
      notify("Wallet added");
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

  async function saveSettings(patch: Record<string, unknown>) {
    setBusy(true);
    setError("");
    try {
      const next = await api.saveSettings(patch);
      setSettings(next);
      if (next.max_spend_eth) setMaxSpendEth(next.max_spend_eth);
      if (next.meme_max_spend_eth) setMemeMaxSpendEth(next.meme_max_spend_eth);
      notify("Settings saved");
    } catch (err) {
      setError(err instanceof Error ? err.message : "save failed");
    } finally {
      setBusy(false);
    }
  }

  async function onManualMint(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.mint({
        collection: mintCollection.trim(),
        quantity: Number(mintQty) || 1,
      });
      notify("Mint queued");
      setTimeout(() => void refresh(), 1200);
    } catch (err) {
      setError(err instanceof Error ? err.message : "mint failed");
    } finally {
      setBusy(false);
    }
  }

  async function onSweepMint(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.mint({
        collection: mintCollection.trim(),
        quantity: Number(mintQty) || 1,
        wallet_count: Number(sweepWallets) || 1,
        sweep: true,
      });
      notify("Free-mint sweep queued");
      setTimeout(() => void refresh(), 1200);
    } catch (err) {
      setError(err instanceof Error ? err.message : "sweep failed");
    } finally {
      setBusy(false);
    }
  }

  async function onManualMemeBuy(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.memeBuy(manualMemeToken.trim());
      notify("Meme buy queued");
      setManualMemeToken("");
      setTimeout(() => void refresh(), 1200);
    } catch (err) {
      setError(err instanceof Error ? err.message : "buy failed");
    } finally {
      setBusy(false);
    }
  }

  async function queueMemeBuy(token: string) {
    setBusy(true);
    setError("");
    try {
      await api.memeBuy(token);
      notify("Meme buy queued");
      setTimeout(() => void refresh(), 1200);
    } catch (err) {
      setError(err instanceof Error ? err.message : "buy failed");
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
          Smart-wallet NFT priority (2+ wallets), free-mint sweeps, and memecoin watches.
          Sell alerts off. Memes buy only with locked LP.
        </p>
        <div className="stats">
          <div className="stat">
            <span>Watching</span>
            <b>{status?.wallets_watching ?? "—"}</b>
          </div>
          <div className="stat">
            <span>NFT events</span>
            <b>{status?.trades_total ?? "—"}</b>
          </div>
          <div className="stat">
            <span>Memes (&lt;30d)</span>
            <b>{memeStats?.total ?? "—"}</b>
          </div>
          <div className="stat">
            <span>LP locked</span>
            <b>{memeStats?.lp_locked ?? "—"}</b>
          </div>
        </div>
      </header>

      <nav className="tabs">
        {(
          [
            ["wallets", "Wallets"],
            ["activity", "NFT activity"],
            ["memes", "Memecoins"],
            ["collections", "Collections"],
            ["execute", "Execute"],
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

      {tab === "execute" ? (
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>Execution</h2>
              <p>
                Free-mint alerts fire when ≥{settings?.smart_wallet_min ?? 2} smart wallets hit a
                new SeaDrop (price 0). Auto-copy sweeps up to{" "}
                {settings?.mint_max_wallets ?? 3} wallets × qty (cap {settings?.mint_max_total ?? 20}{" "}
                total). Signers: {settings?.signer_count ?? 0}. LIVE needs keys in `.env`.
              </p>
            </div>
          </div>

          <div className="form" style={{ gridTemplateColumns: "1fr 1fr auto" }}>
            <input
              className="input"
              value={maxSpendEth}
              onChange={(e) => setMaxSpendEth(e.target.value)}
              placeholder="Max ETH per mint"
              style={{ fontFamily: "var(--font-display)" }}
            />
            <div className="meta" style={{ alignItems: "center" }}>
              <span className="chip">{settings?.has_signer ? `signers ${settings.signer_count ?? 1}` : "no signer"}</span>
              <span className={`chip ${settings?.execute_live ? "sell" : "mint"}`}>
                {settings?.execute_live ? "live" : "dry-run"}
              </span>
              <span className={`chip ${settings?.auto_copy_mint ? "curated" : ""}`}>
                {settings?.auto_copy_mint ? "auto-sweep on" : "auto-sweep off"}
              </span>
            </div>
            <button
              className="btn btn-primary"
              type="button"
              disabled={busy}
              onClick={() => void saveSettings({ max_spend_eth: maxSpendEth })}
            >
              Save max spend
            </button>
          </div>

          <div className="toolbar" style={{ marginBottom: "1rem" }}>
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy}
              onClick={() =>
                void saveSettings({ auto_copy_mint: !settings?.auto_copy_mint })
              }
            >
              {settings?.auto_copy_mint ? "Disable auto-sweep" : "Enable auto-sweep"}
            </button>
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy || !settings?.has_signer}
              onClick={() => void saveSettings({ execute_live: !settings?.execute_live })}
            >
              {settings?.execute_live ? "Switch to dry-run" : "Switch to LIVE"}
            </button>
            <input
              className="input"
              style={{ maxWidth: "6rem" }}
              value={String(settings?.mint_quantity ?? 1)}
              onChange={(e) => {
                const n = Number(e.target.value) || 1;
                setSettings((s) => (s ? { ...s, mint_quantity: n } : s));
                setMintQty(String(n));
              }}
              onBlur={() =>
                void saveSettings({ mint_quantity: settings?.mint_quantity ?? 1 })
              }
              title="Qty per wallet"
            />
            <input
              className="input"
              style={{ maxWidth: "6rem" }}
              value={String(settings?.mint_max_wallets ?? 3)}
              onChange={(e) => {
                const n = Number(e.target.value) || 1;
                setSettings((s) => (s ? { ...s, mint_max_wallets: n } : s));
                setSweepWallets(String(n));
              }}
              onBlur={() =>
                void saveSettings({ mint_max_wallets: settings?.mint_max_wallets ?? 3 })
              }
              title="Max wallets per sweep"
            />
            <input
              className="input"
              style={{ maxWidth: "6rem" }}
              value={String(settings?.mint_max_total ?? 20)}
              onChange={(e) => {
                const n = Number(e.target.value) || 1;
                setSettings((s) => (s ? { ...s, mint_max_total: n } : s));
              }}
              onBlur={() =>
                void saveSettings({ mint_max_total: settings?.mint_max_total ?? 20 })
              }
              title="Max total NFTs per collection"
            />
          </div>

          <h2 style={{ fontSize: "1.05rem", margin: "0 0 0.75rem" }}>Manual mint / free sweep</h2>
          <form className="form" onSubmit={onManualMint} style={{ marginBottom: "0.75rem" }}>
            <input
              className="input"
              required
              placeholder="0x collection address"
              value={mintCollection}
              onChange={(e) => setMintCollection(e.target.value)}
            />
            <input
              className="input"
              value={mintQty}
              onChange={(e) => setMintQty(e.target.value)}
              placeholder="Qty/wallet"
            />
            <button className="btn btn-primary" type="submit" disabled={busy}>
              Queue mint
            </button>
          </form>
          <form className="form" onSubmit={onSweepMint}>
            <input
              className="input"
              required
              placeholder="0x free SeaDrop collection"
              value={mintCollection}
              onChange={(e) => setMintCollection(e.target.value)}
            />
            <input
              className="input"
              value={mintQty}
              onChange={(e) => setMintQty(e.target.value)}
              placeholder="Qty/wallet"
            />
            <input
              className="input"
              value={sweepWallets}
              onChange={(e) => setSweepWallets(e.target.value)}
              placeholder="# wallets"
            />
            <button className="btn btn-primary" type="submit" disabled={busy}>
              Sweep free mint
            </button>
          </form>

          <h2 style={{ fontSize: "1.05rem", margin: "1.2rem 0 0.75rem" }}>Orders</h2>
          <div className="list">
            {orders.length === 0 ? (
              <div className="empty">No mint orders yet.</div>
            ) : (
              orders.map((o) => (
                <article className="row" key={o.id}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className={`chip ${o.dry_run ? "mint" : "sell"}`}>
                        {o.dry_run ? "dry" : "live"}
                      </span>
                      <span className="chip">{o.source}</span>
                      <span className="label">{o.status}</span>
                      <span className="chip">x{o.quantity}</span>
                    </div>
                    <a className="addr" href={explorerAddr(o.collection)} target="_blank" rel="noreferrer">
                      {o.collection}
                    </a>
                    <div className="meta">
                      {o.signer ? <span className="chip">{shortAddr(o.signer)}</span> : null}
                      <span className="chip">{o.value_wei} wei</span>
                      {o.tx_hash ? (
                        <a className="chip" href={explorerTx(o.tx_hash)} target="_blank" rel="noreferrer">
                          tx {shortAddr(o.tx_hash)}
                        </a>
                      ) : null}
                      {o.error ? <span className="chip blocked">{o.error.slice(0, 80)}</span> : null}
                      <span className="chip">{new Date(o.created_at).toLocaleString()}</span>
                    </div>
                  </div>
                </article>
              ))
            )}
          </div>
        </section>
      ) : null}

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
              <div className="empty">No wallets yet.</div>
            ) : (
              filteredWallets.map((w) => (
                <article className="row" key={w.address}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className="label">{w.label || "unnamed"}</span>
                      <span className={`chip ${w.source}`}>{w.source}</span>
                      {!w.active ? <span className="chip blocked">paused</span> : null}
                    </div>
                    <a className="addr" href={explorerAddr(w.address)} target="_blank" rel="noreferrer">
                      {w.address}
                    </a>
                  </div>
                  <div className="actions">
                    <button
                      className="btn btn-ghost"
                      type="button"
                      onClick={async () => {
                        await api.patchWallet(w.address, { active: !w.active });
                        await refresh();
                      }}
                    >
                      {w.active ? "Pause" : "Resume"}
                    </button>
                    <button
                      className="btn btn-danger"
                      type="button"
                      onClick={async () => {
                        await api.deleteWallet(w.address);
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
              <p>Watched-wallet events. Telegram also fires on collection heat / premium Seaport prints.</p>
            </div>
          </div>
          <div className="list">
            {filteredTrades.length === 0 ? (
              <div className="empty">No events yet.</div>
            ) : (
              filteredTrades.map((t) => (
                <article className="row" key={`${t.tx_hash}-${t.id}`}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className={`chip ${t.side}`}>{t.side}</span>
                      <span className="label">#{t.token_id}</span>
                    </div>
                    <div className="addr">
                      <a href={explorerAddr(t.wallet)} target="_blank" rel="noreferrer">
                        {shortAddr(t.wallet)}
                      </a>
                      {" · "}
                      <a href={explorerToken(t.collection, t.token_id)} target="_blank" rel="noreferrer">
                        {shortAddr(t.collection)}
                      </a>
                    </div>
                  </div>
                </article>
              ))
            )}
          </div>
        </section>
      ) : null}

      {tab === "memes" ? (
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>Memecoins</h2>
              <p>
                Uniswap V2/V3/V4 launches ≤30d from first liquidity. Telegram alerts + auto-buy
                require locked LP + score ≥70 ({memeStats?.alerted ?? 0} alerted). Buys use a
                separate spend cap; default is dry-run.
              </p>
            </div>
          </div>

          <div className="form" style={{ gridTemplateColumns: "1fr 1fr auto" }}>
            <input
              className="input"
              value={memeMaxSpendEth}
              onChange={(e) => setMemeMaxSpendEth(e.target.value)}
              placeholder="Max ETH per meme buy"
              style={{ fontFamily: "var(--font-display)" }}
            />
            <div className="meta" style={{ alignItems: "center" }}>
              <span className={`chip ${settings?.meme_execute_live ? "sell" : "mint"}`}>
                {settings?.meme_execute_live ? "meme live" : "meme dry-run"}
              </span>
              <span className={`chip ${settings?.meme_auto_buy ? "curated" : ""}`}>
                {settings?.meme_auto_buy ? "auto-buy on" : "auto-buy off"}
              </span>
              <span className="chip">slip {(settings?.meme_slippage_bps ?? 1000) / 100}%</span>
            </div>
            <button
              className="btn btn-primary"
              type="button"
              disabled={busy}
              onClick={() => void saveSettings({ meme_max_spend_eth: memeMaxSpendEth })}
            >
              Save max spend
            </button>
          </div>

          <div className="toolbar" style={{ marginBottom: "1rem" }}>
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy}
              onClick={() => void saveSettings({ meme_auto_buy: !settings?.meme_auto_buy })}
            >
              {settings?.meme_auto_buy ? "Disable auto-buy" : "Enable auto-buy"}
            </button>
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy || !settings?.has_signer}
              onClick={() =>
                void saveSettings({ meme_execute_live: !settings?.meme_execute_live })
              }
            >
              {settings?.meme_execute_live ? "Switch to dry-run" : "Switch to LIVE"}
            </button>
            <input
              className="input"
              style={{ maxWidth: "7rem" }}
              value={String(settings?.meme_slippage_bps ?? 1000)}
              onChange={(e) => {
                const n = Number(e.target.value) || 1000;
                setSettings((s) => (s ? { ...s, meme_slippage_bps: n } : s));
              }}
              onBlur={() =>
                void saveSettings({ meme_slippage_bps: settings?.meme_slippage_bps ?? 1000 })
              }
              title="Slippage in basis points (1000 = 10%)"
            />
          </div>

          <h2 style={{ fontSize: "1.05rem", margin: "0 0 0.75rem" }}>Manual buy</h2>
          <form className="form" onSubmit={onManualMemeBuy}>
            <input
              className="input"
              required
              placeholder="0x token address"
              value={manualMemeToken}
              onChange={(e) => setManualMemeToken(e.target.value)}
            />
            <button className="btn btn-primary" type="submit" disabled={busy}>
              Queue buy
            </button>
          </form>

          <h2 style={{ fontSize: "1.05rem", margin: "1.2rem 0 0.75rem" }}>Tracked tokens</h2>
          <div className="list">
            {filteredMemes.length === 0 ? (
              <div className="empty">No memecoins tracked yet — start meme-watcher.</div>
            ) : (
              filteredMemes.map((m) => (
                <article className="row" key={m.address}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className="label">{m.symbol || shortAddr(m.address)}</span>
                      <span className={`chip ${m.lp_locked ? "curated" : "sell"}`}>
                        {m.lp_locked ? "LP locked" : "LP open"}
                      </span>
                      <span className="chip">{m.dex}</span>
                      <span className="chip">score {m.score.toFixed(0)}</span>
                    </div>
                    <div className="addr">
                      <a href={explorerAddr(m.address)} target="_blank" rel="noreferrer">
                        {shortAddr(m.address)}
                      </a>
                      {" · "}
                      {m.name}
                      {" · "}
                      swaps {m.swap_count}
                      {m.alerted_at ? " · alerted" : ""}
                    </div>
                    <div className="addr">{(m.flags ?? []).join(", ")}</div>
                  </div>
                  <div className="actions">
                    {m.lp_locked ? (
                      <button
                        className="btn btn-ghost"
                        type="button"
                        disabled={busy}
                        onClick={() => void queueMemeBuy(m.address)}
                      >
                        Buy
                      </button>
                    ) : null}
                    {m.launch_tx ? (
                      <a
                        className="btn btn-ghost"
                        href={explorerTx(m.launch_tx)}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Launch tx
                      </a>
                    ) : null}
                  </div>
                </article>
              ))
            )}
          </div>

          <h2 style={{ fontSize: "1.05rem", margin: "1.2rem 0 0.75rem" }}>Buy orders</h2>
          <div className="list">
            {memeOrders.length === 0 ? (
              <div className="empty">No meme buy orders yet.</div>
            ) : (
              memeOrders.map((o) => (
                <article className="row" key={o.id}>
                  <div className="row-main">
                    <div className="row-title">
                      <span className={`chip ${o.dry_run ? "mint" : "sell"}`}>
                        {o.dry_run ? "dry" : "live"}
                      </span>
                      <span className="chip">{o.source}</span>
                      <span className="label">{o.status}</span>
                      {o.dex ? <span className="chip">{o.dex}</span> : null}
                    </div>
                    <a className="addr" href={explorerAddr(o.token)} target="_blank" rel="noreferrer">
                      {o.token}
                    </a>
                    <div className="meta">
                      <span className="chip">{o.spend_wei} wei</span>
                      {o.tx_hash ? (
                        <a className="chip" href={explorerTx(o.tx_hash)} target="_blank" rel="noreferrer">
                          tx {shortAddr(o.tx_hash)}
                        </a>
                      ) : null}
                      {o.error ? <span className="chip blocked">{o.error.slice(0, 80)}</span> : null}
                      <span className="chip">{new Date(o.created_at).toLocaleString()}</span>
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
              <p>Known Robinhood NFT contracts.</p>
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
                    notify(`Loaded ${r.loaded}`);
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
            {filteredCollections.map((c) => (
              <article className="row" key={c.address}>
                <div className="row-main">
                  <div className="row-title">
                    <span className="label">{c.name || "unnamed"}</span>
                  </div>
                  <a className="addr" href={explorerAddr(c.address)} target="_blank" rel="noreferrer">
                    {c.address}
                  </a>
                </div>
                <div className="actions">
                  <button
                    className="btn btn-ghost"
                    type="button"
                    onClick={() => {
                      setTab("execute");
                      setMintCollection(c.address);
                    }}
                  >
                    Mint
                  </button>
                  <button
                    className="btn btn-danger"
                    type="button"
                    onClick={async () => {
                      await api.deleteCollection(c.address);
                      await refresh();
                    }}
                  >
                    Remove
                  </button>
                </div>
              </article>
            ))}
          </div>
        </section>
      ) : null}

      {toast ? <div className="toast">{toast}</div> : null}
    </div>
  );
}
