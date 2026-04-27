# ForgeCast ($FRG)

> The Creator Economy, Rebuilt on Canopy.

ForgeCast is a sovereign content chain on the [Canopy Network](https://canopynetwork.org). Creators publish work on-chain with automatic timestamp and authorship proof, set their own license terms, and collect $FRG payments directly from their audience — no platform, no algorithm, no cut.

## What it does

| Feature | Description |
|---|---|
| **On-chain publishing** | Every piece of content is timestamped at the block it was submitted. Authorship is cryptographically proven. |
| **Custom licensing** | Creators define their own license terms. Terms are immutable once published. |
| **Direct payments** | Buyers pay creators in $FRG. No intermediary. No fee skimmed. |
| **Tipping** | Anyone can send a direct $FRG tip to any creator, with an optional on-chain message. |
| **License registry** | Purchased licenses are recorded on-chain and verifiable by anyone. |

## Transaction types

- `publish_content` — publish a new work with title, content hash, license, price, type, description
- `purchase_license` — pay the creator and record a license on-chain
- `tip_creator` — send a direct $FRG tip
- `send` — standard $FRG transfer

## Content types supported

`article` · `image` · `audio` · `video` · `dataset` · `other`

## Running locally

```bash
# 1. Fix module paths (after cloning)
sed -i 's|github.com/canopy-network/go-plugin|github.com/hope93-commits/canopy/plugin/go|g' go.mod
sed -i 's|github.com/canopy-network/go-plugin|github.com/hope93-commits/canopy/plugin/go|g' proto/*.proto
sed -i 's|github.com/canopy-network/go-plugin|github.com/hope93-commits/canopy/plugin/go|g' proto/_generate.sh

# 2. Regenerate proto types
cd proto && ./_generate.sh && cd ..

# 3. Build the plugin
GOTOOLCHAIN=local go build -o go-plugin .

# 4. Start the node (from canopy root)
canopy start --data-dir ~/.canopy-forgecast --rpc-port 50002 --admin-port 50003

# 5. Open the frontend
open frontend/index.html
```

## Repo structure

```
plugin/go/
├── main.go              ← DO NOT TOUCH
├── chain.json           ← chain identity
├── AGENTS.md            ← AI assistant context
├── README.md
├── contract/
│   ├── plugin.go        ← DO NOT TOUCH
│   ├── contract.go      ← application logic
│   ├── error.go         ← custom error codes
│   └── tx.pb.go         ← generated from tx.proto
└── proto/
    ├── tx.proto         ← message definitions
    ├── account.proto    ← DO NOT TOUCH
    └── plugin.proto     ← DO NOT TOUCH

frontend/
└── index.html           ← single-file dApp
```

## Philosophy

> No algorithms. No gatekeepers. Just creators and their audience.

Built on [Canopy Network](https://canopynetwork.org) Betanet · BLS12-381 · $FRG
