#!/usr/bin/env python3
import json
import urllib.request
from collections import Counter, defaultdict

RPC = "https://rpc.mainnet.chain.robinhood.com"
TRANSFER_TOPIC = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
ZERO = "0x0000000000000000000000000000000000000000"


def rpc(method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(
        RPC,
        data=body,
        headers={"Content-Type": "application/json", "User-Agent": "Mozilla/5.0"},
    )
    with urllib.request.urlopen(req, timeout=90) as r:
        d = json.load(r)
    if "error" in d:
        raise RuntimeError(d["error"])
    return d["result"]


def owner_of(contract, token_id):
    data = "0x6352211e" + format(token_id, "064x")
    try:
        res = rpc("eth_call", [{"to": contract, "data": data}, "latest"])
        if not res or res in ("0x", "0x0"):
            return None
        return ("0x" + res[-40:]).lower()
    except Exception:
        return None


collections = {
    "StonkBrokers": "0x539cdd042c2f3d93ebc5be7dfff0c79f3b4fabf0",
    "RH MACHINES": "0xa3bde969745509cdd41a88e46a38aee57228bf39",
    "Button Presser": "0xe5143de9d3ccbc31ffb4e7fc66d8320e0e2693d2",
    "Rekt Tradooor": "0x7b3ecfa33657de415ff269dc97dfa82954cee706",
    "OnChainHoodies": "0x9ec6c5b9f572a9b02138e553bc5f5882da735f45",
    "QUOTRONS": "0xbde7bec47cbfc689e5e952b6cdd113a500abcd83",
    "Robin Rebels": "0x74f46bdc53ed7652f3038622e419bd4cd6a9ca57",
    "Hopium Machines": "0x7da15c761409cb921a81f0e003704cff418b700b",
    "PitBoys": "0x57069d845701b50f41327362c1c23789043f8dec",
    "BrosRh": "0x444444447657f90a85c99c00c0780e4e1c40c897",
    "Spritehood Wisps": "0xd6577124f96394faee65afd2408f2ffa88445f63",
    "Pepe Must Live": "0x5219561069a12d6261ee4149ed87901254e22603",
    "Hood Land": "0x693c7f27dbdd986625079d441c33fb90a105d153",
    "PONS FAM": "0x2582f12ef9d4aec9e42e5c09cd3016efe3870bb7",
}

opensea_owners = {
    "0x17e9e0d951e9a52a697180119b90ce682e24c66f": "StonkBrokers-owner",
    "0x72749a1e5a92c4cd35052804fcfab49fe257cdf5": "RH-MACHINES-owner",
    "0xcfbd7e12a0f154a45576a73c1e409200068507b9": "Button-Presser-owner",
    "0xdcae87821fa6caea05dbc2811126f4bc7ff73bd1": "Rekt-Tradooor-owner",
    "0xd08a299e6f8e4cb152506802b37c35b940dab0d8": "OnChainHoodies-owner",
    "0x7171e64e979265aed6588577d1c6b60a701d7866": "QUOTRONS-owner",
    "0xbe94988b540a22abd25a6b6db381f85fd4b8e111": "Robin-Rebels-owner",
    "0x69cff5f7faa679f93a82ffccd771bf3bfb0537c7": "Hopium-Machines-owner",
    "0x4be25231574464e58c593bc3001b4bdee37954a6": "PitBoys-owner",
    "0x2dbac9443e95bb0735f665c5129617f74a160fb7": "BrosRh-owner",
    "0x4bda26da074c22ec79e66e3311a5595253cf1be6": "Spritehood-Wisps-owner",
    "0xa13070bd3e2b6dd8c4e844d96e759c1eba41ce1a": "Pepe-Must-Live-owner",
    "0xbabf2cc223f21eec8ffffcdf7996654ccd906d82": "Hood-Land-owner",
    "0xddbddf3a889f1382c42b603747a7b8f209e420c6": "PONS-FAM-owner",
    "0xda3f2de081ba260e53f5a11f9f858a2161bb953a": "pyopyopyopyo-owner",
    "0x48d39d45795f3704d3c4ef751bd8aeecb007a417": "Cash-Cats-owner",
}


def fetch_mint_logs(addr, lo, hi):
    from_topic = "0x" + ("0" * 24) + ZERO[2:]
    try:
        return rpc(
            "eth_getLogs",
            [
                {
                    "address": addr,
                    "fromBlock": hex(lo),
                    "toBlock": hex(hi),
                    "topics": [TRANSFER_TOPIC, from_topic],
                }
            ],
        )
    except Exception:
        if hi - lo < 2000:
            return []
        mid = (lo + hi) // 2
        return fetch_mint_logs(addr, lo, mid) + fetch_mint_logs(addr, mid + 1, hi)


def main():
    latest = int(rpc("eth_blockNumber", []), 16)
    print("latest", latest, "chain", int(rpc("eth_chainId", []), 16))

    wallet_hits = defaultdict(lambda: {"collections": set(), "mints": 0, "labels": set()})

    for addr, label in opensea_owners.items():
        a = addr.lower()
        wallet_hits[a]["labels"].add(label)
        wallet_hits[a]["collections"].add(label.split("-")[0])

    for name, addr in collections.items():
        early = set()
        for tid in list(range(0, 25)) + list(range(50, 60)) + [100, 250, 500]:
            o = owner_of(addr, tid)
            if o and o != ZERO:
                early.add(o)

        mint_tos = Counter()
        windows = [(0, 100000), (100000, 500000), (500000, 2000000)]
        step = 250000
        for lo in range(max(0, latest - step * 8), latest + 1, step):
            windows.append((lo, min(latest, lo + step - 1)))

        seen = set()
        for lo, hi in windows:
            key = (lo, hi)
            if key in seen:
                continue
            seen.add(key)
            logs = fetch_mint_logs(addr, lo, hi)
            for lg in logs:
                topics = lg.get("topics") or []
                if len(topics) < 3:
                    continue
                to = ("0x" + topics[2][-40:]).lower()
                mint_tos[to] += 1

        top = [a for a, _ in mint_tos.most_common(20)]
        print(
            f"{name}: early_owners={len(early)} mint_events={sum(mint_tos.values())} unique_minters={len(mint_tos)}"
        )
        for w in list(early) + top:
            if w == ZERO:
                continue
            wallet_hits[w]["collections"].add(name)
            wallet_hits[w]["mints"] += mint_tos.get(w, 0)
            if w in early:
                wallet_hits[w]["labels"].add(f"early-holder:{name}")

    ranked = sorted(
        wallet_hits.items(),
        key=lambda kv: (len(kv[1]["collections"]), kv[1]["mints"]),
        reverse=True,
    )
    seed = []
    for addr, meta in ranked:
        cols = sorted(meta["collections"])
        labels = sorted(meta["labels"])
        if len(cols) >= 2 or labels or meta["mints"] >= 3:
            seed.append(
                {
                    "address": addr,
                    "collections": cols,
                    "labels": labels,
                    "mint_hits": meta["mints"],
                }
            )

    print("SEED", len(seed))
    for s in seed[:50]:
        cols = ",".join(s["collections"][:4])
        print(f"{s['address']}|cols={len(s['collections'])}|mints={s['mint_hits']}|{cols}")

    out = {"wallets": seed[:80], "contracts": collections}
    path = "configs/research_raw.json"
    with open(path, "w") as f:
        json.dump(out, f, indent=2)
    print("wrote", path, "count", len(out["wallets"]))


if __name__ == "__main__":
    main()
