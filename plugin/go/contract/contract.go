// contract.go — ForgeCast ($FRG) plugin application logic
//
// On-chain media protocol. Creators publish, timestamp, license,
// and earn from their work. Readers pay access fees on-chain.
//
// Implements the Canopy plugin interface exactly as found in:
//   github.com/hope93-commits/canopy/plugin/go/contract/plugin.go
//
// IMPORTANT: StateRead/StateWrite are called as c.plugin.StateRead(c, ...)
// NOT as standalone functions. This matches the actual repo.
//
// Transaction types:
//   send               — built-in token transfer
//   publish_content    — anchor content on-chain with IPFS URI
//   access_content     — pay creator to unlock content
//   license_content    — pay for commercial/exclusive license
//   claim_earnings     — creator sweeps accumulated royalties
//
// State key prefixes (no conflict with built-ins 0x01, 0x02, 0x07):
//   0x10 — Content
//   0x11 — ContentCounter  (singleton)
//   0x12 — CreatorEarnings
//   0x13 — AccessRecord    (accessor_addr + content_id compound key)

package contract

import (
	"bytes"
	"encoding/binary"
	"log"
	"math/rand"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
)

// ─────────────────────────────────────────────────────────────
// Package-level block height
//
// plugin.go creates a NEW Contract instance for every FSM message,
// so state on the Contract struct does NOT persist between BeginBlock
// and DeliverTx. We use a package-level variable protected by a
// RWMutex to safely share the current height across calls.
// ─────────────────────────────────────────────────────────────

var (
	heightMu     sync.RWMutex
	globalHeight uint64
)

func setGlobalHeight(h uint64) {
	heightMu.Lock()
	defer heightMu.Unlock()
	globalHeight = h
}

func getGlobalHeight() uint64 {
	heightMu.RLock()
	defer heightMu.RUnlock()
	return globalHeight
}

// ─────────────────────────────────────────────────────────────
// Plugin registration
// ─────────────────────────────────────────────────────────────

var ContractConfig = &PluginConfig{
	Name:    "forgecast_contract",
	Id:      1,
	Version: 1,
	SupportedTransactions: []string{
		"send",
		"publish_content",
		"access_content",
		"license_content",
		"claim_earnings",
	},
	TransactionTypeUrls: []string{
		"type.googleapis.com/types.MessageSend",
		"type.googleapis.com/types.MessagePublishContent",
		"type.googleapis.com/types.MessageAccessContent",
		"type.googleapis.com/types.MessageLicenseContent",
		"type.googleapis.com/types.MessageClaimEarnings",
	},
	EventTypeUrls: nil,
}

// init registers all proto file descriptors — required so Canopy can
// deserialise ForgeCast message types from the protobuf Any wrapper.
func init() {
	file_account_proto_init()
	file_event_proto_init()
	file_plugin_proto_init()
	file_tx_proto_init()

	var fds [][]byte
	for _, file := range []protoreflect.FileDescriptor{
		anypb.File_google_protobuf_any_proto,
		File_account_proto, File_event_proto, File_plugin_proto, File_tx_proto,
	} {
		fd, _ := proto.Marshal(protodesc.ToFileDescriptorProto(file))
		fds = append(fds, fd)
	}
	ContractConfig.FileDescriptorProtos = fds
}

// ─────────────────────────────────────────────────────────────
// Contract struct
// ─────────────────────────────────────────────────────────────

// NOTE: Do NOT add block-scoped state here — plugin.go creates a new
// Contract instance per FSM message. Use package-level vars instead.
type Contract struct {
	Config    Config
	FSMConfig *PluginFSMConfig
	plugin    *Plugin
	fsmId     uint64
}

// ─────────────────────────────────────────────────────────────
// State key constructors
// ─────────────────────────────────────────────────────────────

var (
	accountPrefix        = []byte{1}    // built-in
	poolPrefix           = []byte{2}    // built-in
	paramsPrefix         = []byte{7}    // built-in
	contentPrefix        = []byte{0x10} // ForgeCast: Content records
	contentCounterPrefix = []byte{0x11} // ForgeCast: singleton counter
	earningsPrefix       = []byte{0x12} // ForgeCast: CreatorEarnings per address
	accessPrefix         = []byte{0x13} // ForgeCast: AccessRecord per accessor+content
)

func KeyForAccount(addr []byte) []byte {
	return JoinLenPrefix(accountPrefix, addr)
}

func KeyForFeePool(chainId uint64) []byte {
	return JoinLenPrefix(poolPrefix, formatUint64(chainId))
}

func KeyForFeeParams() []byte {
	return JoinLenPrefix(paramsPrefix, []byte("/f/"))
}

func KeyForContent(id uint64) []byte {
	return JoinLenPrefix(contentPrefix, formatUint64(id))
}

func KeyForContentCounter() []byte {
	return JoinLenPrefix(contentCounterPrefix, []byte("/cc/"))
}

func KeyForCreatorEarnings(addr []byte) []byte {
	return JoinLenPrefix(earningsPrefix, addr)
}

// KeyForAccessRecord — compound key: content_id bytes + accessor_address
func KeyForAccessRecord(contentId uint64, accessorAddr []byte) []byte {
	idBytes := formatUint64(contentId)
	compound := append(idBytes, accessorAddr...)
	return JoinLenPrefix(accessPrefix, compound)
}

func formatUint64(u uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, u)
	return b
}

// ─────────────────────────────────────────────────────────────
// Lifecycle: Genesis
// ─────────────────────────────────────────────────────────────

func (c *Contract) Genesis(_ *PluginGenesisRequest) *PluginGenesisResponse {
	// Write default fee params and zero content counter
	defaultParams := &FeeParams{
		SendFee:    10000,
		PublishFee: 10000,
		AccessFee:  10000,
		LicenseFee: 10000,
		ClaimFee:   10000,
	}
	paramsBytes, err := Marshal(defaultParams)
	if err != nil {
		return &PluginGenesisResponse{Error: err}
	}
	counterBytes, err := Marshal(&ContentCounter{Count: 0})
	if err != nil {
		return &PluginGenesisResponse{Error: err}
	}
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForFeeParams(), Value: paramsBytes},
			{Key: KeyForContentCounter(), Value: counterBytes},
		},
	})
	if err != nil {
		return &PluginGenesisResponse{Error: err}
	}
	return &PluginGenesisResponse{}
}

// ─────────────────────────────────────────────────────────────
// Lifecycle: BeginBlock
// ─────────────────────────────────────────────────────────────

func (c *Contract) BeginBlock(req *PluginBeginRequest) *PluginBeginResponse {
	setGlobalHeight(req.Height)
	return &PluginBeginResponse{}
}

// ─────────────────────────────────────────────────────────────
// Lifecycle: CheckTx  (stateless validation only — no StateWrite)
// ─────────────────────────────────────────────────────────────

func (c *Contract) CheckTx(request *PluginCheckRequest) *PluginCheckResponse {
	// Read fee params (minimal state read, as per docs)
	resp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: rand.Uint64(), Key: KeyForFeeParams()},
		},
	})
	if err == nil {
		err = resp.Error
	}
	if err != nil {
		return &PluginCheckResponse{Error: err}
	}
	minFees := new(FeeParams)
	var feeBytes []byte
	for _, r := range resp.Results {
		if len(r.Entries) > 0 {
			feeBytes = r.Entries[0].Value
		}
	}
	if e := Unmarshal(feeBytes, minFees); e != nil {
		return &PluginCheckResponse{Error: e}
	}
	if request.Tx.Fee < minFees.SendFee {
		return &PluginCheckResponse{Error: ErrTxFeeBelowStateLimit()}
	}

	msg, err := FromAny(request.Tx.Msg)
	if err != nil {
		return &PluginCheckResponse{Error: err}
	}

	switch x := msg.(type) {
	case *MessageSend:
		return c.CheckMessageSend(x)
	case *MessagePublishContent:
		return c.CheckPublishContent(x)
	case *MessageAccessContent:
		return c.CheckAccessContent(x)
	case *MessageLicenseContent:
		return c.CheckLicenseContent(x)
	case *MessageClaimEarnings:
		return c.CheckClaimEarnings(x)
	default:
		return &PluginCheckResponse{Error: ErrInvalidMessageCast()}
	}
}

func (c *Contract) CheckMessageSend(msg *MessageSend) *PluginCheckResponse {
	if len(msg.FromAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if len(msg.ToAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.Amount == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{Recipient: msg.ToAddress, AuthorizedSigners: [][]byte{msg.FromAddress}}
}

func (c *Contract) CheckPublishContent(msg *MessagePublishContent) *PluginCheckResponse {
	if len(msg.AuthorAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if len(msg.Title) == 0 || len(msg.Title) > 200 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	if len(msg.IpfsUri) == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	if msg.ContentType > 4 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	if msg.LicenseType > 3 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	if msg.RoyaltyBps > 5000 { // max 50%
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.AuthorAddress}}
}

func (c *Contract) CheckAccessContent(msg *MessageAccessContent) *PluginCheckResponse {
	if len(msg.AccessorAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.ContentId == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	// Payment amount validated in DeliverTx where we can read content record
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.AccessorAddress}}
}

func (c *Contract) CheckLicenseContent(msg *MessageLicenseContent) *PluginCheckResponse {
	if len(msg.LicenseeAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	if msg.ContentId == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	if msg.Payment == 0 {
		return &PluginCheckResponse{Error: ErrInvalidAmount()}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.LicenseeAddress}}
}

func (c *Contract) CheckClaimEarnings(msg *MessageClaimEarnings) *PluginCheckResponse {
	if len(msg.CreatorAddress) != 20 {
		return &PluginCheckResponse{Error: ErrInvalidAddress()}
	}
	return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.CreatorAddress}}
}

// ─────────────────────────────────────────────────────────────
// Lifecycle: DeliverTx  (stateful execution)
// ─────────────────────────────────────────────────────────────

func (c *Contract) DeliverTx(request *PluginDeliverRequest) *PluginDeliverResponse {
	msg, err := FromAny(request.Tx.Msg)
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	switch x := msg.(type) {
	case *MessageSend:
		return c.DeliverMessageSend(x, request.Tx.Fee)
	case *MessagePublishContent:
		return c.DeliverPublishContent(x, request.Tx.Fee)
	case *MessageAccessContent:
		return c.DeliverAccessContent(x, request.Tx.Fee)
	case *MessageLicenseContent:
		return c.DeliverLicenseContent(x, request.Tx.Fee)
	case *MessageClaimEarnings:
		return c.DeliverClaimEarnings(x, request.Tx.Fee)
	default:
		return &PluginDeliverResponse{Error: ErrInvalidMessageCast()}
	}
}

// ─── DeliverMessageSend ───────────────────────────────────────

func (c *Contract) DeliverMessageSend(msg *MessageSend, fee uint64) *PluginDeliverResponse {
	log.Printf("DeliverMessageSend: from=%x to=%x amount=%d fee=%d", msg.FromAddress, msg.ToAddress, msg.Amount, fee)
	var (
		fromKey, toKey, feePoolKey         = KeyForAccount(msg.FromAddress), KeyForAccount(msg.ToAddress), KeyForFeePool(c.Config.ChainId)
		fromQueryId, toQueryId, feeQueryId = rand.Uint64(), rand.Uint64(), rand.Uint64()
		from, to, feePool                  = new(Account), new(Account), new(Pool)
		fromBytes, toBytes, feePoolBytes   []byte
	)
	response, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: feeQueryId, Key: feePoolKey},
			{QueryId: fromQueryId, Key: fromKey},
			{QueryId: toQueryId, Key: toKey},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if response.Error != nil {
		return &PluginDeliverResponse{Error: response.Error}
	}
	for _, resp := range response.Results {
		if len(resp.Entries) == 0 {
			continue
		}
		switch resp.QueryId {
		case fromQueryId:
			fromBytes = resp.Entries[0].Value
		case toQueryId:
			toBytes = resp.Entries[0].Value
		case feeQueryId:
			feePoolBytes = resp.Entries[0].Value
		}
	}
	amountToDeduct := msg.Amount + fee
	if e := Unmarshal(fromBytes, from); e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	if e := Unmarshal(toBytes, to); e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	if e := Unmarshal(feePoolBytes, feePool); e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	if from.Amount < amountToDeduct {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}
	if bytes.Equal(fromKey, toKey) {
		to = from
	}
	from.Amount -= amountToDeduct
	feePool.Amount += fee
	to.Amount += msg.Amount
	fromBytes, err = Marshal(from)
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	toBytes, err = Marshal(to)
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	feePoolBytes, err = Marshal(feePool)
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	var resp *PluginStateWriteResponse
	if from.Amount == 0 {
		resp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
			Sets:    []*PluginSetOp{{Key: feePoolKey, Value: feePoolBytes}, {Key: toKey, Value: toBytes}},
			Deletes: []*PluginDeleteOp{{Key: fromKey}},
		})
	} else {
		resp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
			Sets: []*PluginSetOp{
				{Key: feePoolKey, Value: feePoolBytes},
				{Key: toKey, Value: toBytes},
				{Key: fromKey, Value: fromBytes},
			},
		})
	}
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if resp.Error != nil {
		return &PluginDeliverResponse{Error: resp.Error}
	}
	return &PluginDeliverResponse{}
}

// ─── DeliverPublishContent ────────────────────────────────────

func (c *Contract) DeliverPublishContent(msg *MessagePublishContent, fee uint64) *PluginDeliverResponse {
	log.Printf("DeliverPublishContent: author=%x title=%q fee=%d", msg.AuthorAddress, msg.Title, fee)

	counterQId := rand.Uint64()
	authorQId  := rand.Uint64()
	feePoolQId := rand.Uint64()

	readResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: counterQId, Key: KeyForContentCounter()},
			{QueryId: authorQId, Key: KeyForAccount(msg.AuthorAddress)},
			{QueryId: feePoolQId, Key: KeyForFeePool(c.Config.ChainId)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	counter := &ContentCounter{}
	author  := &Account{}
	feePool := &Pool{}

	for _, r := range readResp.Results {
		if len(r.Entries) == 0 {
			continue
		}
		switch r.QueryId {
		case counterQId:
			Unmarshal(r.Entries[0].Value, counter)
		case authorQId:
			Unmarshal(r.Entries[0].Value, author)
		case feePoolQId:
			Unmarshal(r.Entries[0].Value, feePool)
		}
	}

	if author.Amount < fee {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	// Assign new content ID
	newId := counter.Count + 1
	content := &Content{
		Id:             newId,
		AuthorAddress:  msg.AuthorAddress,
		Title:          msg.Title,
		IpfsUri:        msg.IpfsUri,
		Synopsis:       msg.Synopsis,
		ContentType:    msg.ContentType,
		LicenseType:    msg.LicenseType,
		AccessFee:      msg.AccessFee,
		RoyaltyBps:     msg.RoyaltyBps,
		BlockHeight:    getGlobalHeight(),
		AccessCount:    0,
		LicenseCount:   0,
		TimestampProof: msg.TimestampProof,
	}

	// Update author earnings record (increment works published)
	earningsQId := rand.Uint64()
	earningsResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: earningsQId, Key: KeyForCreatorEarnings(msg.AuthorAddress)},
		},
	})
	earnings := &CreatorEarnings{Address: msg.AuthorAddress}
	if err == nil && earningsResp.Error == nil {
		for _, r := range earningsResp.Results {
			if r.QueryId == earningsQId && len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, earnings)
			}
		}
	}
	earnings.WorksPublished++

	// Deduct fee
	author.Amount -= fee
	feePool.Amount += fee
	counter.Count = newId

	// Marshal everything
	contentBytes, e := Marshal(content)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	counterBytes, e := Marshal(counter)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	feePoolBytes, e := Marshal(feePool)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	earningsBytes, e := Marshal(earnings)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}

	sets := []*PluginSetOp{
		{Key: KeyForContent(newId), Value: contentBytes},
		{Key: KeyForContentCounter(), Value: counterBytes},
		{Key: KeyForFeePool(c.Config.ChainId), Value: feePoolBytes},
		{Key: KeyForCreatorEarnings(msg.AuthorAddress), Value: earningsBytes},
	}

	var writeResp *PluginStateWriteResponse
	if author.Amount == 0 {
		writeResp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
			Sets:    sets,
			Deletes: []*PluginDeleteOp{{Key: KeyForAccount(msg.AuthorAddress)}},
		})
	} else {
		authorBytes, e := Marshal(author)
		if e != nil {
			return &PluginDeliverResponse{Error: e}
		}
		sets = append(sets, &PluginSetOp{Key: KeyForAccount(msg.AuthorAddress), Value: authorBytes})
		writeResp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{Sets: sets})
	}
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	log.Printf("Published content id=%d at block=%d", newId, getGlobalHeight())
	return &PluginDeliverResponse{}
}

// ─── DeliverAccessContent ─────────────────────────────────────

func (c *Contract) DeliverAccessContent(msg *MessageAccessContent, fee uint64) *PluginDeliverResponse {
	log.Printf("DeliverAccessContent: accessor=%x content=%d payment=%d", msg.AccessorAddress, msg.ContentId, msg.Payment)

	contentQId  := rand.Uint64()
	accessorQId := rand.Uint64()
	feePoolQId  := rand.Uint64()
	accessRecQId := rand.Uint64()

	readResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: contentQId, Key: KeyForContent(msg.ContentId)},
			{QueryId: accessorQId, Key: KeyForAccount(msg.AccessorAddress)},
			{QueryId: feePoolQId, Key: KeyForFeePool(c.Config.ChainId)},
			{QueryId: accessRecQId, Key: KeyForAccessRecord(msg.ContentId, msg.AccessorAddress)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	content  := &Content{}
	accessor := &Account{}
	feePool  := &Pool{}
	found := struct{ content, alreadyAccessed bool }{}

	for _, r := range readResp.Results {
		switch r.QueryId {
		case contentQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, content)
				found.content = true
			}
		case accessorQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, accessor)
			}
		case feePoolQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, feePool)
			}
		case accessRecQId:
			if len(r.Entries) > 0 {
				found.alreadyAccessed = true
			}
		}
	}

	if !found.content {
		return &PluginDeliverResponse{Error: ErrInvalidAmount()} // content not found
	}
	if found.alreadyAccessed {
		return &PluginDeliverResponse{Error: ErrInvalidAmount()} // already accessed
	}
	if msg.Payment < content.AccessFee {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	totalCost := msg.Payment + fee
	if accessor.Amount < totalCost {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	// Deduct from accessor
	accessor.Amount -= totalCost
	feePool.Amount += fee

	// Add access_fee to creator's pending earnings
	earningsQId := rand.Uint64()
	earningsResp, _ := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: earningsQId, Key: KeyForCreatorEarnings(content.AuthorAddress)},
		},
	})
	earnings := &CreatorEarnings{Address: content.AuthorAddress}
	if earningsResp != nil && earningsResp.Error == nil {
		for _, r := range earningsResp.Results {
			if r.QueryId == earningsQId && len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, earnings)
			}
		}
	}
	earnings.PendingAmount += msg.Payment

	// Increment content access count
	content.AccessCount++

	// Write access record
	accessRec := &AccessRecord{
		AccessorAddress: msg.AccessorAddress,
		ContentId:       msg.ContentId,
		BlockHeight:     getGlobalHeight(),
		AmountPaid:      msg.Payment,
	}

	contentBytes, e := Marshal(content)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	earningsBytes, e := Marshal(earnings)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	accessRecBytes, e := Marshal(accessRec)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	feePoolBytes, e := Marshal(feePool)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}

	sets := []*PluginSetOp{
		{Key: KeyForContent(msg.ContentId), Value: contentBytes},
		{Key: KeyForCreatorEarnings(content.AuthorAddress), Value: earningsBytes},
		{Key: KeyForAccessRecord(msg.ContentId, msg.AccessorAddress), Value: accessRecBytes},
		{Key: KeyForFeePool(c.Config.ChainId), Value: feePoolBytes},
	}

	var writeResp *PluginStateWriteResponse
	if accessor.Amount == 0 {
		writeResp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
			Sets:    sets,
			Deletes: []*PluginDeleteOp{{Key: KeyForAccount(msg.AccessorAddress)}},
		})
	} else {
		accessorBytes, e := Marshal(accessor)
		if e != nil {
			return &PluginDeliverResponse{Error: e}
		}
		sets = append(sets, &PluginSetOp{Key: KeyForAccount(msg.AccessorAddress), Value: accessorBytes})
		writeResp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{Sets: sets})
	}
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	return &PluginDeliverResponse{}
}

// ─── DeliverLicenseContent ────────────────────────────────────

func (c *Contract) DeliverLicenseContent(msg *MessageLicenseContent, fee uint64) *PluginDeliverResponse {
	log.Printf("DeliverLicenseContent: licensee=%x content=%d payment=%d", msg.LicenseeAddress, msg.ContentId, msg.Payment)

	contentQId  := rand.Uint64()
	licenseeQId := rand.Uint64()
	feePoolQId  := rand.Uint64()

	readResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: contentQId, Key: KeyForContent(msg.ContentId)},
			{QueryId: licenseeQId, Key: KeyForAccount(msg.LicenseeAddress)},
			{QueryId: feePoolQId, Key: KeyForFeePool(c.Config.ChainId)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	content  := &Content{}
	licensee := &Account{}
	feePool  := &Pool{}
	found := false

	for _, r := range readResp.Results {
		switch r.QueryId {
		case contentQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, content)
				found = true
			}
		case licenseeQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, licensee)
			}
		case feePoolQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, feePool)
			}
		}
	}

	if !found {
		return &PluginDeliverResponse{Error: ErrInvalidAmount()}
	}

	totalCost := msg.Payment + fee
	if licensee.Amount < totalCost {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	// Royalty to creator's pending earnings
	royaltyAmount := (msg.Payment * uint64(content.RoyaltyBps)) / 10000
	earningsQId := rand.Uint64()
	earningsResp, _ := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: earningsQId, Key: KeyForCreatorEarnings(content.AuthorAddress)},
		},
	})
	earnings := &CreatorEarnings{Address: content.AuthorAddress}
	if earningsResp != nil && earningsResp.Error == nil {
		for _, r := range earningsResp.Results {
			if r.QueryId == earningsQId && len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, earnings)
			}
		}
	}
	earnings.PendingAmount += royaltyAmount
	earnings.LicensesIssued++

	content.LicenseCount++
	licensee.Amount -= totalCost
	feePool.Amount += fee

	contentBytes, e := Marshal(content)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	earningsBytes, e := Marshal(earnings)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	feePoolBytes, e := Marshal(feePool)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}

	sets := []*PluginSetOp{
		{Key: KeyForContent(msg.ContentId), Value: contentBytes},
		{Key: KeyForCreatorEarnings(content.AuthorAddress), Value: earningsBytes},
		{Key: KeyForFeePool(c.Config.ChainId), Value: feePoolBytes},
	}

	var writeResp *PluginStateWriteResponse
	if licensee.Amount == 0 {
		writeResp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
			Sets:    sets,
			Deletes: []*PluginDeleteOp{{Key: KeyForAccount(msg.LicenseeAddress)}},
		})
	} else {
		licenseeBytes, e := Marshal(licensee)
		if e != nil {
			return &PluginDeliverResponse{Error: e}
		}
		sets = append(sets, &PluginSetOp{Key: KeyForAccount(msg.LicenseeAddress), Value: licenseeBytes})
		writeResp, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{Sets: sets})
	}
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	return &PluginDeliverResponse{}
}

// ─── DeliverClaimEarnings ─────────────────────────────────────

func (c *Contract) DeliverClaimEarnings(msg *MessageClaimEarnings, fee uint64) *PluginDeliverResponse {
	log.Printf("DeliverClaimEarnings: creator=%x fee=%d", msg.CreatorAddress, fee)

	earningsQId := rand.Uint64()
	creatorQId  := rand.Uint64()
	feePoolQId  := rand.Uint64()

	readResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: earningsQId, Key: KeyForCreatorEarnings(msg.CreatorAddress)},
			{QueryId: creatorQId, Key: KeyForAccount(msg.CreatorAddress)},
			{QueryId: feePoolQId, Key: KeyForFeePool(c.Config.ChainId)},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	earnings := &CreatorEarnings{Address: msg.CreatorAddress}
	creator  := &Account{}
	feePool  := &Pool{}

	for _, r := range readResp.Results {
		switch r.QueryId {
		case earningsQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, earnings)
			}
		case creatorQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, creator)
			}
		case feePoolQId:
			if len(r.Entries) > 0 {
				Unmarshal(r.Entries[0].Value, feePool)
			}
		}
	}

	if creator.Amount < fee {
		return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
	}

	pendingPayout := earnings.PendingAmount

	// Sweep pending earnings to creator's wallet
	creator.Amount += pendingPayout
	creator.Amount -= fee
	feePool.Amount += fee
	earnings.TotalEarned += pendingPayout
	earnings.PendingAmount = 0

	creatorBytes, e := Marshal(creator)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	earningsBytes, e := Marshal(earnings)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}
	feePoolBytes, e := Marshal(feePool)
	if e != nil {
		return &PluginDeliverResponse{Error: e}
	}

	writeResp, err := c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: KeyForAccount(msg.CreatorAddress), Value: creatorBytes},
			{Key: KeyForCreatorEarnings(msg.CreatorAddress), Value: earningsBytes},
			{Key: KeyForFeePool(c.Config.ChainId), Value: feePoolBytes},
		},
	})
	if err != nil {
		return &PluginDeliverResponse{Error: err}
	}
	if writeResp.Error != nil {
		return &PluginDeliverResponse{Error: writeResp.Error}
	}
	log.Printf("Claimed %d nCNPY for creator=%x", pendingPayout, msg.CreatorAddress)
	return &PluginDeliverResponse{}
}

// ─────────────────────────────────────────────────────────────
// Lifecycle: EndBlock
// ─────────────────────────────────────────────────────────────

func (c *Contract) EndBlock(_ *PluginEndRequest) *PluginEndResponse {
	return &PluginEndResponse{}
}
