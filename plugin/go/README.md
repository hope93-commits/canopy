# ForgeCast — The Creator Economy, Rebuilt On-Chain

A sovereign content chain built on Canopy Network. Creators publish work, prove ownership, and get paid directly — no platform takes a cut.

## What It Does

ForgeCast is a Canopy Nested Chain with 4 custom transaction types:

- **publish_content** — publish a content record on-chain with title, hash, license terms, and price
- **purchase_license** — buy a license for another creator's content
- **tip_creator** — send a direct tip to any creator address
- **send** — transfer $FRG tokens between accounts

## Stack

- Canopy Network (Go plugin, BLS12-381 signing, NestBFT consensus)
- Single-file HTML frontend (vanilla JS, @noble/curves BLS)
- Native token: $FRG

## Run Locally

```bash
# 1. Start the chain
canopy start

# 2. Serve the frontend
cd frontend && python3 -m http.server 8080

# 3. Open http://localhost:8080

Key Files
plugin/go/contract/contract.go — all 4 transaction types
plugin/go/proto/tx.proto — message definitions
frontend/index.html — complete frontend
plugin/go/chain.json — chain metadata
