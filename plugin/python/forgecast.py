#!/usr/bin/env python3
"""
ForgeCast - On-Chain Media Protocol
Built on Canopy Network - Python Plugin
Port: 50002 (Canopy RPC port)
"""

import json
import hashlib
import time
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs

# ── STATE ─────────────────────────────────────────────────────────

block_height = 1847291
total_volume = 0
total_licenses = 0

works = [
    {
        "id": "demo-1",
        "title": "The Architecture of Attention",
        "type": "article",
        "description": "An exploration of how digital media rewires cognition and what creators can do to build with intention rather than extraction.",
        "license": "commercial",
        "price": 180,
        "royalty": 10,
        "creator": "0x4f3a...8c2e",
        "block": 1847188,
        "timestamp": int(time.time() * 1000) - 7200000,
        "licenses_sold": 4
    },
    {
        "id": "demo-2",
        "title": "Signal / Noise — Episode 12",
        "type": "audio",
        "description": "Independent podcast on the economics of attention. Why the creator middle class collapsed and what rebuilds it.",
        "license": "personal",
        "price": 60,
        "royalty": 5,
        "creator": "0x9d2b...1a4f",
        "block": 1847014,
        "timestamp": int(time.time() * 1000) - 18000000,
        "licenses_sold": 12
    },
    {
        "id": "demo-3",
        "title": "On-Chain IP: A Legal Framework",
        "type": "research",
        "description": "First comprehensive analysis of how blockchain timestamps hold up in IP disputes across 14 jurisdictions. Open access.",
        "license": "open",
        "price": 0,
        "royalty": 0,
        "creator": "0x3c7e...f2b8",
        "block": 1846302,
        "timestamp": int(time.time() * 1000) - 86400000,
        "licenses_sold": 88
    },
    {
        "id": "demo-4",
        "title": "Industrial Light Series — Vol. 3",
        "type": "image",
        "description": "12-image photographic series documenting decommissioned manufacturing infrastructure across the American Midwest.",
        "license": "exclusive",
        "price": 420,
        "royalty": 15,
        "creator": "0x7a1d...c9e3",
        "block": 1845891,
        "timestamp": int(time.time() * 1000) - 172800000,
        "licenses_sold": 1
    },
    {
        "id": "demo-5",
        "title": "ForgeCast Protocol Overview",
        "type": "research",
        "description": "Technical overview of the ForgeCast on-chain media protocol and its integration with the Canopy Network stack.",
        "license": "open",
        "price": 0,
        "royalty": 0,
        "creator": "0x4f3a...8c2e",
        "block": 1847100,
        "timestamp": int(time.time() * 1000) - 3600000,
        "licenses_sold": 23
    },
    {
        "id": "demo-6",
        "title": "Maker Economy 2026",
        "type": "article",
        "description": "A data-driven analysis of how on-chain protocols are shifting value back to independent creators for the first time in a decade.",
        "license": "commercial",
        "price": 90,
        "royalty": 8,
        "creator": "0x2b8c...7d1a",
        "block": 1845200,
        "timestamp": int(time.time() * 1000) - 259200000,
        "licenses_sold": 7
    }
]

# ── BLOCK TICKER ──────────────────────────────────────────────────

def block_ticker():
    global block_height
    while True:
        time.sleep(4)
        block_height += 1

threading.Thread(target=block_ticker, daemon=True).start()

# ── HELPERS ───────────────────────────────────────────────────────

def make_tx_hash(data):
    return "0x" + hashlib.sha256(
        (str(data) + str(time.time())).encode()
    ).hexdigest()

def make_id(data):
    return hashlib.sha256(
        (str(data) + str(time.time())).encode()
    ).hexdigest()[:16]

# ── HTTP HANDLER ──────────────────────────────────────────────────

class ForgeCastHandler(BaseHTTPRequestHandler):

    def log_message(self, format, *args):
        # Custom log format
        print(f"[ForgeCast] {self.command} {self.path} - {args[1]}")

    def send_cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")

    def send_json(self, data, status=200):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_cors()
        self.end_headers()
        self.wfile.write(body)

    def read_body(self):
        length = int(self.headers.get("Content-Length", 0))
        if length == 0:
            return {}
        try:
            return json.loads(self.rfile.read(length))
        except:
            return {}

    # ── OPTIONS (preflight) ───────────────────────────────────────

    def do_OPTIONS(self):
        self.send_response(200)
        self.send_cors()
        self.end_headers()

    # ── GET ───────────────────────────────────────────────────────

    def do_GET(self):
        path = urlparse(self.path).path

        if path == "/health":
            self.send_json({
                "status": "ok",
                "chain": "FRG-001",
                "blockHeight": block_height,
                "node": "ForgeCast Python Plugin",
                "protocol": "Canopy Network"
            })

        elif path == "/stats":
            total_vol = total_volume
            total_lic = total_licenses
            self.send_json({
                "volume": str(total_vol // 1000 + 284) + "K",
                "works": len(works),
                "licenses": total_lic,
                "total_staked": "62.4M",
                "block_height": block_height
            })

        elif path == "/works":
            self.send_json(works)

        else:
            self.send_json({"error": "not found"}, 404)

    # ── POST ──────────────────────────────────────────────────────

    def do_POST(self):
        global total_volume, total_licenses
        path = urlparse(self.path).path
        data = self.read_body()

        # ── /publish ──────────────────────────────────────────────
        if path == "/publish":
            title   = data.get("title", "").strip()
            type_   = data.get("type", "").strip()
            license = data.get("license", "").strip()

            if not title or not type_ or not license:
                self.send_json(
                    {"error": "title, type and license are required"}, 400
                )
                return

            work_id  = make_id(title + data.get("creator", ""))
            tx_hash  = make_tx_hash(work_id)

            new_work = {
                "id":            work_id,
                "title":         title,
                "type":          type_,
                "description":   data.get("description", ""),
                "license":       license,
                "price":         int(data.get("price", 0)),
                "royalty":       int(data.get("royalty", 0)),
                "creator":       data.get("creator", "unknown"),
                "block":         block_height,
                "timestamp":     int(time.time() * 1000),
                "licenses_sold": 0
            }

            works.insert(0, new_work)
            print(f"[ForgeCast] PublishContent: '{title}' at block #{block_height}")

            self.send_json({
                "id":      work_id,
                "tx_hash": tx_hash,
                "block":   block_height,
                "status":  "confirmed"
            })

        # ── /license ──────────────────────────────────────────────
        elif path == "/license":
            work_id = data.get("work_id", "")
            buyer   = data.get("buyer", "")
            price   = int(data.get("price", 0))

            for work in works:
                if work["id"] == work_id:
                    work["licenses_sold"] += 1
                    total_volume   += price
                    total_licenses += 1
                    break

            tx_hash = make_tx_hash(work_id + buyer)
            print(f"[ForgeCast] PurchaseLicense: {work_id} by {buyer} ({price} FRG)")

            self.send_json({
                "tx_hash": tx_hash,
                "block":   block_height,
                "status":  "confirmed"
            })

        # ── /stake ────────────────────────────────────────────────
        elif path == "/stake":
            amount  = int(data.get("amount", 0))
            address = data.get("address", "")
            tx_hash = make_tx_hash(address + str(amount))
            print(f"[ForgeCast] StakeFRG: {amount} FRG from {address}")

            self.send_json({
                "tx_hash": tx_hash,
                "block":   block_height,
                "status":  "confirmed"
            })

        # ── /claim ────────────────────────────────────────────────
        elif path == "/claim":
            address = data.get("address", "")
            tx_hash = make_tx_hash(address + "claim")
            print(f"[ForgeCast] ClaimRewards: {address}")

            self.send_json({
                "tx_hash": tx_hash,
                "block":   block_height,
                "status":  "confirmed"
            })

        else:
            self.send_json({"error": "not found"}, 404)

# ── MAIN ──────────────────────────────────────────────────────────

if __name__ == "__main__":
    PORT = 50002
    server = HTTPServer(("0.0.0.0", PORT), ForgeCastHandler)

    print("=" * 50)
    print("  ForgeCast — On-Chain Media Protocol")
    print("  Built on Canopy Network · Chain FRG-001")
    print(f"  Server running on http://localhost:{PORT}")
    print("=" * 50)

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[ForgeCast] Shutting down...")
        server.shutdown()
