package contract

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"

	"google.golang.org/protobuf/proto"
)

// ═══════════════════════════════════════════════════════════════════════
//  CONTRACT CONFIG
//  ContractConfig is the package-level var plugin.go reads at startup.
//  SupportedTransactions[i] must exactly match TransactionTypeUrls[i].
// ═══════════════════════════════════════════════════════════════════════

var ContractConfig = &PluginConfig{
	Name:    "forgecast",
	Id:      1,
	Version: 1,
	SupportedTransactions: []string{
		"send",              // index 0
		"publish_content",   // index 1
		"purchase_license",  // index 2
		"tip_creator",       // index 3
	},
	TransactionTypeUrls: []string{
		"type.googleapis.com/types.MessageSend",            // index 0
		"type.googleapis.com/types.MessagePublishContent",  // index 1
		"type.googleapis.com/types.MessagePurchaseLicense", // index 2
		"type.googleapis.com/types.MessageTipCreator",      // index 3
	},
}

// ═══════════════════════════════════════════════════════════════════════
//  CONTRACT STRUCT
//  currentHeight captured in BeginBlock for use in DeliverTx.
// ═══════════════════════════════════════════════════════════════════════

type Contract struct {
	Config        Config
	FSMConfig     *PluginFSMConfig
	plugin        *Plugin
	fsmId         uint64
	currentHeight uint64
}

// ═══════════════════════════════════════════════════════════════════════
//  STATE KEY PREFIXES
//  0x01–0x0F: reserved by Canopy
//  0x10  ContentRecord    keyed by content ID
//  0x11  LicenseRecord    keyed by buyer-addr + content-id
//  0x12  CreatorEarnings  keyed by creator addr
//  0x13  ContentCounter   singleton
//  0x14  FeePool          singleton (ForgeCast treasury)
// ═══════════════════════════════════════════════════════════════════════

var (
	prefixContent         = []byte{0x10}
	prefixLicense         = []byte{0x11}
	prefixCreatorEarnings = []byte{0x12}
	prefixCounter         = []byte{0x13}
	prefixFeePool         = []byte{0x14}
)

func keyForContent(id uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, id)
	return JoinLenPrefix(prefixContent, b)
}

func keyForLicense(buyer []byte, contentID uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, contentID)
	return JoinLenPrefix(prefixLicense, buyer, b)
}

func keyForCreatorEarnings(addr []byte) []byte {
	return JoinLenPrefix(prefixCreatorEarnings, addr)
}

func keyForCounter() []byte {
	return JoinLenPrefix(prefixCounter, []byte("cnt"))
}

func keyForForgeFeePool() []byte {
	return JoinLenPrefix(prefixFeePool, []byte("fp"))
}

// ═══════════════════════════════════════════════════════════════════════
//  APPLICATION STATE TYPES  (JSON-encoded in the KV store)
// ═══════════════════════════════════════════════════════════════════════

type ContentRecord struct {
	ID          uint64 `json:"id"`
	Creator     []byte `json:"creator"`
	ContentHash string `json:"contentHash"`
	Title       string `json:"title"`
	ContentType string `json:"contentType"`
	License     string `json:"license"`
	Price       uint64 `json:"price"`
	MaxSupply   uint64 `json:"maxSupply"`
	MintCount   uint64 `json:"mintCount"`
	BlockHeight uint64 `json:"blockHeight"`
}

type LicenseRecord struct {
	Buyer      []byte `json:"buyer"`
	ContentID  uint64 `json:"contentId"`
	UnlockedAt uint64 `json:"unlockedAt"`
}

type CounterRecord struct {
	Count uint64 `json:"count"`
}

type EarningsRecord struct {
	Amount uint64 `json:"amount"`
}

// ═══════════════════════════════════════════════════════════════════════
//  GENESIS
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) Genesis(req *PluginGenesisRequest) *PluginGenesisResponse {
	return &PluginGenesisResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  BEGIN BLOCK
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) BeginBlock(req *PluginBeginRequest) *PluginBeginResponse {
	c.currentHeight = req.Height
	return &PluginBeginResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  CHECK TX  — stateless, no StateRead/StateWrite
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) CheckTx(req *PluginCheckRequest) *PluginCheckResponse {
	if req.Tx == nil {
		return &PluginCheckResponse{Error: ErrInvalidMessage()}
	}
	msg, pluginErr := FromAny(req.Tx.Msg)
	if pluginErr != nil {
		return &PluginCheckResponse{Error: pluginErr}
	}
	switch x := msg.(type) {
	case *MessageSend:
		return c.checkSend(x)
	case *MessagePublishContent:
		return c.checkPublishContent(x)
	case *MessagePurchaseLicense:
		return c.checkPurchaseLicense(x)
	case *MessageTipCreator:
		return c.checkTipCreator(x)
	default:
		return &PluginCheckResponse{Error: ErrInvalidMessageCast()}
	}
}

func (c *Contract) checkSend(msg *MessageSend) *PluginCheckResponse {
	if len(msg.FromAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if len(msg.ToAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.Amount == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{
		AuthorizedSigners: [][]byte{msg.FromAddress},
		Recipient:         msg.ToAddress,
	}
}

func (c *Contract) checkPublishContent(msg *MessagePublishContent) *PluginCheckResponse {
	if len(msg.Creator) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.ContentHash == "" {
		return &PluginCheckResponse{Error: ErrInvalidContentHash()}
	}
	if msg.Title == "" {
		return &PluginCheckResponse{Error: ErrInvalidTitle()}
	}
	if !validLicense(msg.License) {
		return &PluginCheckResponse{Error: ErrInvalidLicense()}
	}
	return &PluginCheckResponse{
		AuthorizedSigners: [][]byte{msg.Creator},
	}
}

func (c *Contract) checkPurchaseLicense(msg *MessagePurchaseLicense) *PluginCheckResponse {
	if len(msg.Buyer) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	return &PluginCheckResponse{
		AuthorizedSigners: [][]byte{msg.Buyer},
	}
}

func (c *Contract) checkTipCreator(msg *MessageTipCreator) *PluginCheckResponse {
	if len(msg.Tipper) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if len(msg.Creator) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.Amount == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{
		AuthorizedSigners: [][]byte{msg.Tipper},
		Recipient:         msg.Creator,
	}
}

// ═══════════════════════════════════════════════════════════════════════
//  DELIVER TX  — stateful execution
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) DeliverTx(req *PluginDeliverRequest) *PluginDeliverResponse {
	if req.Tx == nil {
		return &PluginDeliverResponse{Error: ErrInvalidMessage()}
	}
	msg, pluginErr := FromAny(req.Tx.Msg)
	if pluginErr != nil {
		return &PluginDeliverResponse{Error: pluginErr}
	}
	switch x := msg.(type) {
	case *MessageSend:
		return c.deliverSend(x, req.Tx.Fee)
	case *MessagePublishContent:
		return c.deliverPublishContent(x, req.Tx.Fee)
	case *MessagePurchaseLicense:
		return c.deliverPurchaseLicense(x, req.Tx.Fee)
	case *MessageTipCreator:
		return c.deliverTipCreator(x, req.Tx.Fee)
	default:
		return &PluginDeliverResponse{Error: ErrInvalidMessageCast()}
	}
}

// ── send ──────────────────────────────────────────────────────────────

func (c *Contract) deliverSend(msg *MessageSend, fee uint64) *PluginDeliverResponse {
	fromQID := rand.Uint64()
	toQID := rand.Uint64()
	feeQID := rand.Uint64()

	readResp, pErr := StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: fromQID, Key: KeyForAccount(msg.FromAddress)},
			{QueryId: toQID, Key: KeyForAccount(msg.ToAddress)},
			{QueryId: feeQID, Key: KeyForFeePool(c.Config.ChainId)},
		},
	})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	fromAcc := &Account{}
	toAcc := &Account{}
	feePool := &Pool{}

	for _, result := range readResp.Results {
		if len(result.Entries) == 0 {
			continue
		}
		switch result.QueryId {
		case fromQID:
			if err := proto.Unmarshal(result.Entries[0].Value, fromAcc); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case toQID:
			if err := proto.Unmarshal(result.Entries[0].Value, toAcc); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case feeQID:
			if err := proto.Unmarshal(result.Entries[0].Value, feePool); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		}
	}

	total := msg.Amount + fee
	if fromAcc.Amount < total {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	fromAcc.Amount -= total
	toAcc.Amount += msg.Amount
	toAcc.Address = msg.ToAddress
	feePool.Amount += fee
	feePool.Id = c.Config.ChainId

	fromBytes, err := proto.Marshal(fromAcc)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	toBytes, err := proto.Marshal(toAcc)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	feeBytes, err := proto.Marshal(feePool)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}

	sets := []*PluginSetOp{
		{Key: KeyForAccount(msg.ToAddress), Value: toBytes},
		{Key: KeyForFeePool(c.Config.ChainId), Value: feeBytes},
	}
	var deletes []*PluginDeleteOp
	if fromAcc.Amount == 0 {
		deletes = append(deletes, &PluginDeleteOp{Key: KeyForAccount(msg.FromAddress)})
	} else {
		sets = append(sets, &PluginSetOp{Key: KeyForAccount(msg.FromAddress), Value: fromBytes})
	}

	writeResp, pErr := StateWrite(c, &PluginStateWriteRequest{Sets: sets, Deletes: deletes})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	return &PluginDeliverResponse{}
}

// ── publish_content ───────────────────────────────────────────────────

func (c *Contract) deliverPublishContent(msg *MessagePublishContent, fee uint64) *PluginDeliverResponse {
	creatorAccQID := rand.Uint64()
	counterQID := rand.Uint64()
	feeQID := rand.Uint64()

	readResp, pErr := StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: creatorAccQID, Key: KeyForAccount(msg.Creator)},
			{QueryId: counterQID, Key: keyForCounter()},
			{QueryId: feeQID, Key: KeyForFeePool(c.Config.ChainId)},
		},
	})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	creatorAcc := &Account{}
	counter := &CounterRecord{}
	feePool := &Pool{}

	for _, result := range readResp.Results {
		if len(result.Entries) == 0 {
			continue
		}
		switch result.QueryId {
		case creatorAccQID:
			if err := proto.Unmarshal(result.Entries[0].Value, creatorAcc); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case counterQID:
			if err := json.Unmarshal(result.Entries[0].Value, counter); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case feeQID:
			if err := proto.Unmarshal(result.Entries[0].Value, feePool); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		}
	}

	if creatorAcc.Amount < fee {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	creatorAcc.Amount -= fee
	feePool.Amount += fee
	feePool.Id = c.Config.ChainId
	counter.Count++
	newID := counter.Count

	content := &ContentRecord{
		ID:          newID,
		Creator:     msg.Creator,
		ContentHash: msg.ContentHash,
		Title:       msg.Title,
		ContentType: msg.ContentType,
		License:     msg.License,
		Price:       msg.Price,
		MaxSupply:   msg.MaxSupply,
		BlockHeight: c.currentHeight,
	}

	contentBytes, err := json.Marshal(content)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	counterBytes, err := json.Marshal(counter)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	creatorBytes, err := proto.Marshal(creatorAcc)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	feeBytes, err := proto.Marshal(feePool)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}

	sets := []*PluginSetOp{
		{Key: keyForContent(newID), Value: contentBytes},
		{Key: keyForCounter(), Value: counterBytes},
		{Key: KeyForFeePool(c.Config.ChainId), Value: feeBytes},
	}
	if creatorAcc.Amount > 0 {
		sets = append(sets, &PluginSetOp{Key: KeyForAccount(msg.Creator), Value: creatorBytes})
	}

	writeResp, pErr := StateWrite(c, &PluginStateWriteRequest{Sets: sets})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	return &PluginDeliverResponse{}
}

// ── purchase_license ──────────────────────────────────────────────────

func (c *Contract) deliverPurchaseLicense(msg *MessagePurchaseLicense, fee uint64) *PluginDeliverResponse {
	buyerAccQID := rand.Uint64()
	contentQID := rand.Uint64()
	creatorEarnQID := rand.Uint64()
	feeQID := rand.Uint64()

	readResp, pErr := StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: buyerAccQID, Key: KeyForAccount(msg.Buyer)},
			{QueryId: contentQID, Key: keyForContent(msg.ContentId)},
			{QueryId: creatorEarnQID, Key: keyForCreatorEarnings(msg.Creator)},
			{QueryId: feeQID, Key: KeyForFeePool(c.Config.ChainId)},
		},
	})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	buyerAcc := &Account{}
	content := &ContentRecord{}
	earnings := &EarningsRecord{}
	feePool := &Pool{}

	for _, result := range readResp.Results {
		if len(result.Entries) == 0 {
			continue
		}
		switch result.QueryId {
		case buyerAccQID:
			if err := proto.Unmarshal(result.Entries[0].Value, buyerAcc); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case contentQID:
			if err := json.Unmarshal(result.Entries[0].Value, content); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case creatorEarnQID:
			if err := json.Unmarshal(result.Entries[0].Value, earnings); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case feeQID:
			if err := proto.Unmarshal(result.Entries[0].Value, feePool); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		}
	}

	if content.ID == 0 {
		return &PluginDeliverResponse{Error: ErrContentNotFound()}
	}
	if content.MaxSupply > 0 && content.MintCount >= content.MaxSupply {
		return &PluginDeliverResponse{Error: ErrSupplyExhausted()}
	}

	// 5% protocol fee on content price
	protocolFee := content.Price / 20
	creatorNet := content.Price - protocolFee
	total := content.Price + fee

	if buyerAcc.Amount < total {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	buyerAcc.Amount -= total
	earnings.Amount += creatorNet
	feePool.Amount += fee + protocolFee
	feePool.Id = c.Config.ChainId
	content.MintCount++

	license := &LicenseRecord{
		Buyer:      msg.Buyer,
		ContentID:  msg.ContentId,
		UnlockedAt: c.currentHeight,
	}

	licenseBytes, err := json.Marshal(license)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	earningsBytes, err := json.Marshal(earnings)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	buyerBytes, err := proto.Marshal(buyerAcc)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	feeBytes, err := proto.Marshal(feePool)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}

	sets := []*PluginSetOp{
		{Key: keyForLicense(msg.Buyer, msg.ContentId), Value: licenseBytes},
		{Key: keyForContent(msg.ContentId), Value: contentBytes},
		{Key: keyForCreatorEarnings(msg.Creator), Value: earningsBytes},
		{Key: KeyForFeePool(c.Config.ChainId), Value: feeBytes},
	}
	if buyerAcc.Amount > 0 {
		sets = append(sets, &PluginSetOp{Key: KeyForAccount(msg.Buyer), Value: buyerBytes})
	}

	writeResp, pErr := StateWrite(c, &PluginStateWriteRequest{Sets: sets})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	return &PluginDeliverResponse{}
}

// ── tip_creator ───────────────────────────────────────────────────────

func (c *Contract) deliverTipCreator(msg *MessageTipCreator, fee uint64) *PluginDeliverResponse {
	tipperAccQID := rand.Uint64()
	creatorEarnQID := rand.Uint64()
	feeQID := rand.Uint64()

	readResp, pErr := StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: tipperAccQID, Key: KeyForAccount(msg.Tipper)},
			{QueryId: creatorEarnQID, Key: keyForCreatorEarnings(msg.Creator)},
			{QueryId: feeQID, Key: KeyForFeePool(c.Config.ChainId)},
		},
	})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	tipperAcc := &Account{}
	earnings := &EarningsRecord{}
	feePool := &Pool{}

	for _, result := range readResp.Results {
		if len(result.Entries) == 0 {
			continue
		}
		switch result.QueryId {
		case tipperAccQID:
			if err := proto.Unmarshal(result.Entries[0].Value, tipperAcc); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case creatorEarnQID:
			if err := json.Unmarshal(result.Entries[0].Value, earnings); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		case feeQID:
			if err := proto.Unmarshal(result.Entries[0].Value, feePool); err != nil {
				return &PluginDeliverResponse{Error: ErrUnmarshal(err)}
			}
		}
	}

	total := msg.Amount + fee
	if tipperAcc.Amount < total {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	tipperAcc.Amount -= total
	earnings.Amount += msg.Amount
	feePool.Amount += fee
	feePool.Id = c.Config.ChainId

	earningsBytes, err := json.Marshal(earnings)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	tipperBytes, err := proto.Marshal(tipperAcc)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}
	feeBytes, err := proto.Marshal(feePool)
	if err != nil {
		return &PluginDeliverResponse{Error: ErrMarshal(err)}
	}

	sets := []*PluginSetOp{
		{Key: keyForCreatorEarnings(msg.Creator), Value: earningsBytes},
		{Key: KeyForFeePool(c.Config.ChainId), Value: feeBytes},
	}
	if tipperAcc.Amount > 0 {
		sets = append(sets, &PluginSetOp{Key: KeyForAccount(msg.Tipper), Value: tipperBytes})
	}

	writeResp, pErr := StateWrite(c, &PluginStateWriteRequest{Sets: sets})
	if pErr != nil {
		return &PluginDeliverResponse{Error: pErr}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	return &PluginDeliverResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  END BLOCK
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) EndBlock(req *PluginEndRequest) *PluginEndResponse {
	return &PluginEndResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  BUILT-IN KEY HELPERS  (must use the same prefixes as the template)
// ═══════════════════════════════════════════════════════════════════════

// KeyForAccount returns the state key for an Account (Canopy prefix 0x01).
func KeyForAccount(addr []byte) []byte {
	return JoinLenPrefix([]byte{0x01}, addr)
}

// KeyForFeePool returns the state key for the fee Pool (Canopy prefix 0x02).
func KeyForFeePool(chainID uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, chainID)
	return JoinLenPrefix([]byte{0x02}, b)
}

// ═══════════════════════════════════════════════════════════════════════
//  HELPERS
// ═══════════════════════════════════════════════════════════════════════

func validLicense(l string) bool {
	switch l {
	case "free", "payPerView", "commercial", "collectible":
		return true
	}
	return false
}

// uint64Str converts uint64 to decimal string (used in event attrs).
func uint64Str(v uint64) string {
	return fmt.Sprintf("%d", v)
}
