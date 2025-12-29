import base64
import json
import os
import time
import urllib.error
import urllib.request
from collections import OrderedDict


BRALE_PROFILES_API = os.getenv("BRALE_PROFILES_API", "http://brale:9991/api/profiles")
FREQTRADE_CONFIG = os.getenv("FREQTRADE_CONFIG", "/freqtrade/user_data/freqtrade-config.json")
SYNC_INTERVAL = int(os.getenv("SYNC_INTERVAL", "3600"))
FREQTRADE_API_URL = os.getenv("FREQTRADE_API_URL", "http://freqtrade:8755/api/v1")
FREQTRADE_RELOAD = os.getenv("FREQTRADE_RELOAD", "true").lower() in ("1", "true", "yes")


def log(msg):
    print(msg, flush=True)


def fetch_json(url, timeout=10):
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def normalize_symbols(symbols, quote):
    out = []
    for sym in symbols or []:
        if not sym:
            continue
        s = str(sym).strip().upper()
        if not s:
            continue
        if ":" in s:
            s = s.split(":", 1)[0]
        if "/" not in s:
            s = f"{s}/{quote}"
        out.append(s)
    return sorted(set(out))


def parse_targets_from_api(api_url, quote):
    data = fetch_json(api_url)
    if isinstance(data, dict) and data.get("success") and isinstance(data.get("items"), list):
        symbols = [item.get("symbol", "") for item in data["items"] if isinstance(item, dict)]
        return normalize_symbols(symbols, quote)
    if isinstance(data, dict) and isinstance(data.get("symbols"), list):
        return normalize_symbols(data["symbols"], quote)
    if isinstance(data, list):
        return normalize_symbols(data, quote)
    raise ValueError("unsupported targets API response format")


def to_freqtrade_pair(sym):
    s = sym.strip().upper()
    if ":" in s:
        return s
    if "/" in s:
        base, quote = s.split("/", 1)
        return f"{base}/{quote}:{quote}"
    return f"{s}/USDT:USDT"


def load_config(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f, object_pairs_hook=OrderedDict)


def write_config(path, cfg):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(cfg, f, indent=4, ensure_ascii=True)
        f.write("\n")


def build_pairs_from_profiles(profiles):
    pairs = set()
    for profile in profiles:
        quote = (profile.get("targets_api_quote") or "USDT").strip().upper()
        targets = []
        api_override = bool(profile.get("targets_api_override"))
        api_url = profile.get("targets_api_url") or ""
        if api_override and api_url:
            try:
                targets = parse_targets_from_api(api_url, quote)
            except Exception as exc:
                log(f"targets api failed ({api_url}): {exc}")
        if not targets:
            targets = normalize_symbols(profile.get("targets", []), quote)
        for t in targets:
            pairs.add(to_freqtrade_pair(t))
    return sorted(pairs)


def build_auth_header(cfg):
    user = os.getenv("FREQTRADE_USER") or cfg.get("api_server", {}).get("username")
    password = os.getenv("FREQTRADE_PASS") or cfg.get("api_server", {}).get("password")
    if not user or not password:
        return None
    raw = f"{user}:{password}".encode("utf-8")
    return "Basic " + base64.b64encode(raw).decode("ascii")


def reload_freqtrade(cfg):
    if not FREQTRADE_RELOAD:
        return
    auth = build_auth_header(cfg)
    if not auth:
        log("freqtrade reload skipped: missing credentials")
        return
    url = FREQTRADE_API_URL.rstrip("/") + "/reload_config"
    req = urllib.request.Request(url, method="POST", headers={"Authorization": auth})
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            log(f"freqtrade reload: {resp.status}")
    except urllib.error.HTTPError as err:
        log(f"freqtrade reload failed: {err.code}")
    except urllib.error.URLError as err:
        log(f"freqtrade reload failed: {err}")


def sync_once():
    profiles_payload = fetch_json(BRALE_PROFILES_API)
    profiles = profiles_payload.get("profiles", [])
    if not profiles:
        raise ValueError("no profiles found")

    pairs = build_pairs_from_profiles(profiles)
    if not pairs:
        raise ValueError("no pairs resolved from profiles")

    cfg = load_config(FREQTRADE_CONFIG)
    exchange = cfg.setdefault("exchange", OrderedDict())
    old_pairs = exchange.get("pair_whitelist", [])
    if old_pairs == pairs:
        log(f"pair_whitelist unchanged ({len(pairs)} pairs)")
        return

    exchange["pair_whitelist"] = pairs
    write_config(FREQTRADE_CONFIG, cfg)
    log(f"pair_whitelist updated ({len(pairs)} pairs)")
    reload_freqtrade(cfg)


def main():
    interval = max(SYNC_INTERVAL, 60)
    while True:
        try:
            sync_once()
        except Exception as exc:
            log(f"sync failed: {exc}")
        time.sleep(interval)


if __name__ == "__main__":
    main()
