#!/usr/bin/env python3
"""Fast curated seed research: OpenSea collection owners + early token holders via ownerOf."""
import json
import urllib.request
from collections import defaultdict

RPC = "https://rpc.mainnet.chain.robinhood.com"
ZERO = "0x0000000000000000000000000000000000000000"

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
    "0x17e9e0d951e9a52a697180119b90ce682e24c66f": ("StonkBrokers-owner", ["StonkBrokers"]),
    "0x72749a1e5a92c4cd35052804fcfab49fe257cdf5": ("RH-MACHINES-owner", ["RH MACHINES"]),
    "0xcfbd7e12a0f154a45576a73c1e409200068507b9": ("Button-Presser-owner", ["Button Presser"]),
    "0xdcae87821fa6caea05dbc2811126f4bc7ff73bd1": ("Rekt-Tradooor-owner", ["Rekt Tradooor"]),
    "0xd08a299e6f8e4cb152506802b37c35b940dab0d8": ("OnChainHoodies-owner", ["OnChainHoodies"]),
    "0x7171e64e979265aed6588577d1c6b60a701d7866": ("QUOTRONS-owner", ["QUOTRONS"]),
    "0xbe94988b540a22abd25a6b6db381f85fd4b8e111": ("Robin-Rebels-owner", ["Robin Rebels"]),
    "0x69cff5f7faa679f93a82ffccd771bf3bfb0537c7": ("Hopium-Machines-owner", ["Hopium Machines"]),
    "0x4be25231574464e58c593bc3001b4bdee37954a6": ("PitBoys-owner", ["PitBoys"]),
    "0x2dbac9443e95bb0735f665c5129617f74a160fb7": ("BrosRh-owner", ["BrosRh"]),
    "0x4bda26da074c22ec79e66e3311a5595253cf1be6": ("Spritehood-Wisps-owner", ["Spritehood Wisps"]),
    "0xa13070bd3e2b6dd8c4e844d96e759c1eba41ce1a": ("Pepe-Must-Live-owner", ["Pepe Must Live"]),
    "0xbabf2cc223f21eec8ffffcdf7996654ccd906d82": ("Hood-Land-owner", ["Hood Land"]),
    "0xddbddf3a889f1382c42b603747a7b8f209e420c6": ("PONS-FAM-owner", ["PONS FAM"]),
    "0xda3f2de081ba260e53f5a11f9f858a2161bb953a": ("pyopyopyopyo-owner", ["pyopyopyopyo"]),
    "0x48d39d45795f3704d3c4ef751bd8aeecb007a417": ("Cash-Cats-owner", ["Cash Cats"]),
}


def rpc(method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(
        RPC,
        data=body,
        headers={"Content-Type": "application/json", "User-Agent": "Mozilla/5.0"},
    )
    with urllib.request.urlopen(req, timeout=30) as r:
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


def main():
    print("chain", int(rpc("eth_chainId", []), 16), flush=True)
    print("block", int(rpc("eth_blockNumber", []), 16), flush=True)

    hits = defaultdict(lambda: {"collections": set(), "labels": set(), "evidence": set()})

    for addr, (label, cols) in opensea_owners.items():
        a = addr.lower()
        hits[a]["labels"].add(label)
        hits[a]["collections"].update(cols)
        hits[a]["evidence"].add(f"opensea-collection-owner:{label}")

    for name, contract in collections.items():
        print(f"scanning early holders {name}", flush=True)
        for tid in list(range(0, 30)) + [50, 75, 100, 150, 200, 250, 500, 1000]:
            o = owner_of(contract, tid)
            if not o or o == ZERO:
                continue
            hits[o]["collections"].add(name)
            hits[o]["labels"].add(f"early-token:{name}")
            hits[o]["evidence"].add(
                f"https://robinhoodchain.blockscout.com/token/{contract}/instance/{tid}"
            )

    ranked = sorted(hits.items(), key=lambda kv: len(kv[1]["collections"]), reverse=True)
    wallets = []
    for addr, meta in ranked:
        cols = sorted(meta["collections"])
        labels = sorted(meta["labels"])
        # keep multi-collection holders + all labeled OpenSea owners
        is_owner = any(l.endswith("-owner") for l in labels)
        if len(cols) >= 2 or is_owner:
            primary = next((l for l in labels if l.endswith("-owner")), labels[0] if labels else "rhc-nft")
            wallets.append(
                {
                    "address": addr,
                    "label": primary,
                    "source": "curated",
                    "tags": ["rhc-nft", "curated-seed"],
                    "collections": cols,
                    "evidence": sorted(meta["evidence"])[:8],
                    "score": float(min(99, 50 + 10 * len(cols))),
                    "active": True,
                }
            )

    print("seed_wallets", len(wallets), flush=True)
    with open("configs/research_raw.json", "w") as f:
        json.dump({"wallets": wallets, "contracts": collections}, f, indent=2)

    # YAML seed
    lines = ["# Curated Robinhood Chain NFT smart-wallet seed", "# Generated by scripts/research_seed_fast.py", "wallets:"]
    for w in wallets[:60]:
        lines.append(f'  - address: "{w["address"]}"')
        lines.append(f'    label: "{w["label"]}"')
        lines.append('    source: curated')
        lines.append("    tags:")
        for t in w["tags"]:
            lines.append(f'      - "{t}"')
        lines.append("    collections:")
        for c in w["collections"]:
            lines.append(f'      - "{c}"')
        lines.append("    evidence:")
        for e in w["evidence"][:4]:
            lines.append(f'      - "{e}"')
        lines.append(f'    score: {w["score"]}')
        lines.append("    active: true")
    with open("configs/wallets.seed.yaml", "w") as f:
        f.write("\n".join(lines) + "\n")
    print("wrote configs/wallets.seed.yaml", flush=True)


if __name__ == "__main__":
    main()
