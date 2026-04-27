# AGENTS.md — ForgeCast ($FRG)

## What this chain does
ForgeCast is a sovereign content chain on Canopy Network. Creators publish work on-chain with automatic authorship proof, set custom license terms, and receive direct $FRG payments. No platform takes a cut.

## Module path
```
github.com/hope93-commits/canopy/plugin/go
```

## Transaction types

| Type string        | Proto message                  | Who signs         |
|--------------------|--------------------------------|-------------------|
| `publish_content`  | `MessagePublishContent`        | `creator_address` |
| `purchase_license` | `MessagePurchaseLicense`       | `buyer_address`   |
| `tip_creator`      | `MessageTipCreator`            | `sender_address`  |
| `send`             | `MessageSend` (Canopy built-in)| `from_address`    |

## State key prefixes

| Prefix | Type           | Key structure                        |
|--------|----------------|--------------------------------------|
| `0x10` | ContentRecord  | prefix + uint64 content ID (8 bytes) |
| `0x11` | LicenseRecord  | prefix + buyer address + uint64 ID   |
| `0x12` | CreatorStats   | prefix + creator address             |
| `0x13` | Counter        | `{0x13, 0x00}` — global content ID   |

## Custom error codes (15+)

| Code | Constant             | Meaning                              |
|------|----------------------|--------------------------------------|
| 15   | ErrInvalidTitle      | Title empty or > 200 chars           |
| 16   | ErrInvalidHash       | Content hash empty                   |
| 17   | ErrInvalidLicense    | License terms empty                  |
| 18   | ErrContentNotFound   | Content ID not in state              |
| 19   | ErrAlreadyLicensed   | Buyer already holds this license     |
| 20   | ErrInsufficientFunds | Amount < content price               |
| 21   | ErrInvalidAmount     | Tip or payment amount is zero        |
| 22   | ErrSelfPurchase      | Creator buying their own content     |
| 23   | ErrSelfTip           | Creator tipping themselves           |
| 24   | ErrInvalidContentType| Unknown content_type value           |
| 25   | ErrDescriptionTooLong| Description > 500 characters         |

## Critical rules for AI assistants

1. **Never read/write state inside CheckTx** — CheckTx is stateless.
2. **currentHeight is captured in BeginBlock** — DeliverTx has no Height field.
3. **Use `make()` + `copy()` for composite keys** — never `append()` on address slices.
4. **SupportedTransactions and TransactionTypeUrls must stay index-aligned.**
5. **send uses `msg` field format; all plugin types use `msgTypeUrl` + `msgBytes`.**
6. **Signatures and public keys are hex strings in RPC calls, not base64.**
7. **Time is microseconds: `BigInt(Date.now() * 1000)`.**
8. **BLS signing uses G2 (96-byte) signatures via `@noble/curves bls12_381`.**
9. **Empty memo field must be omitted from sign bytes (proto3 default).**

## RPC ports
- `50002` — public RPC (submit txs, query state)
- `50003` — admin RPC (localhost only, never expose)

## Frontend query for content list
```
GET /v1/query/state?prefix=0110&page=1&perPage=50
```
Values are base64-encoded JSON ContentRecord objects.
