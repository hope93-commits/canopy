# Lattice — Onchain Reputation & Social Graph

> **One-line pitch:** Lattice is an onchain reputation and social graph primitive that any Nested Chain can plug into — turning protocol activity into portable identity.

## What is Lattice?

Lattice is a Social-Fi infrastructure layer built as a Canopy Nested Chain plugin. It is not a standalone app — it is a protocol primitive that any other Nested Chain can integrate to emit reputation events for their users.

Those events accumulate into portable identity scores with time-weighted decay. Users can follow and endorse each other with staked LAT bonds. The result is a composable reputation layer that travels with an address across the entire Canopy ecosystem.

## How It Works

### Reputation Events
Any registered protocol can emit signed reputation events to Lattice. Each event has a type (positive or negative), a weight in base points, and a source protocol address. Events accumulate into a score with time-weighted decay.

Decay formula: score = sum(points * HALF_LIFE / (HALF_LIFE + age_in_blocks))
HALF_LIFE = 10,000 blocks

### Social Graph
- follow_address: onchain follow link between two addresses
- unfollow_address: remove follow link
- endorse_address: stake LAT bond to vouch for another address
- revoke_endorsement: unstake bond and remove endorsement

### Portable Identity Score
Each address has a queryable LatticeProfile containing their raw score, followers, following, endorsements received, and full reputation event history. Any other Nested Chain can read this data via RPC.

## Transaction Types

- register_protocol: Register a protocol to emit reputation events
- emit_reputation_event: Protocol emits positive or negative rep event for a user
- follow_address: Add an address to your onchain social graph
- unfollow_address: Remove an address from your social graph
- endorse_address: Stake LAT bond to vouch for another address
- revoke_endorsement: Unstake bond and remove endorsement
- faucet: Testnet token faucet
- reward: Admin reward distribution

## Chain Info

- Chain ID: 2
- Token: LAT
- Template: Go

## Running Locally

Start the node:
  canopy start
  Set plugin to go in ~/.canopy/config.json
  Restart node

Build and run the plugin:
  cd plugin/go
  go build -o go-plugin .

Run the test suite:
  cd plugin/go/tutorial
  go test -v -run TestLatticeTransactions -timeout 300s

Launch the frontend:
  cd Frontend
  python3 -m http.server 8080
  Open http://localhost:8080

## Test Suite (9/9 passing)

1. Create protocol admin, User A, User B accounts
2. Fund all accounts via faucet
3. Register protocol
4. Emit positive reputation event for User A (+15 pts)
5. User B follows User A
6. User B endorses User A (50,000 LAT stake)
7. Verify User B balance reduced by stake amount
8. Revoke endorsement (stake returned)
9. User B unfollows User A

## Frontend

Single-page app served via Python HTTP server. Connects directly to the local Canopy node RPC with zero mock data.

- Explorer: search any address, view reputation score, followers, endorsements, rep history
- Actions: register protocol, emit rep events, follow, endorse, unfollow, revoke
- Live Feed: real-time transaction feed from the chain
