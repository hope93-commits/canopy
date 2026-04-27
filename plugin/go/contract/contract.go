package contract

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
)

// ═══════════════════════════════════════════════════════════════════════
//  STATE KEY PREFIXES
//  0x01-0x0F reserved by Canopy. ForgeCast uses 0x10+.
// ═══════════════════════════════════════════════════════════════════════

var (
	// prefixContent maps content ID (uint64) → ContentRecord JSON
	prefixContent = []byte{0x10}
	// prefixLicense maps (buyerAddress + contentID bytes) → LicenseRecord JSON
	prefixLicense = []byte{0x11}
	// prefixCreatorStats maps creatorAddress → CreatorStats JSON
	prefixCreatorStats = []byte{0x12}
	// keyContentCounter holds the global monotonic content ID counter
	keyContentCounter = []byte{0x13, 0x00}
)

// ═══════════════════════════════════════════════════════════════════════
//  DATA MODELS
// ═══════════════════════════════════════════════════════════════════════

// ContentRecord is stored on-chain for every published piece of content.
type ContentRecord struct {
	ID            uint64 `json:"id"`
	CreatorAddress []byte `json:"creator_address"`
	Title         string `json:"title"`
	ContentHash   string `json:"content_hash"`
	LicenseTerms  string `json:"license_terms"`
	PriceUfrg     uint64 `json:"price_ufrg"`
	ContentType   string `json:"content_type"`
	Description   string `json:"description"`
	PublishedAt   uint64 `json:"published_at"` // block height
	LicenseCount  uint64 `json:"license_count"`
	TipTotal      uint64 `json:"tip_total"`
}

// LicenseRecord proves a buyer holds a valid license for a piece of content.
type LicenseRecord struct {
	ContentID    uint64 `json:"content_id"`
	BuyerAddress []byte `json:"buyer_address"`
	AmountPaid   uint64 `json:"amount_paid"`
	AcquiredAt   uint64 `json:"acquired_at"` // block height
}

// CreatorStats aggregates totals for a creator's on-chain activity.
type CreatorStats struct {
	TotalEarnings uint64 `json:"total_earnings"`
	ContentCount  uint64 `json:"content_count"`
	TipTotal      uint64 `json:"tip_total"`
}

// ═══════════════════════════════════════════════════════════════════════
//  CONTRACT STRUCT
// ═══════════════════════════════════════════════════════════════════════

// Contract holds runtime state and implements the Canopy plugin interface.
type Contract struct {
	Config        Config
	FSMConfig     *PluginFSMConfig
	plugin        *Plugin
	fsmId         uint64
	currentHeight uint64 // captured in BeginBlock — DeliverTx has no Height field
}

// ═══════════════════════════════════════════════════════════════════════
//  CONTRACT CONFIG — maps transaction type strings to proto URLs
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) ContractConfig() ContractConfig {
	return ContractConfig{
		SupportedTransactions: []string{
			"send",              // 0
			"publish_content",  // 1
			"purchase_license", // 2
			"tip_creator",      // 3
		},
		TransactionTypeUrls: []string{
			"type.googleapis.com/types.MessageSend",            // 0
			"type.googleapis.com/types.MessagePublishContent",  // 1
			"type.googleapis.com/types.MessagePurchaseLicense", // 2
			"type.googleapis.com/types.MessageTipCreator",      // 3
		},
	}
}

// ═══════════════════════════════════════════════════════════════════════
//  GENESIS
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) Genesis(req *PluginGenesisRequest) *PluginGenesisResponse {
	// Initialise the content counter at 1
	counterBytes := uint64ToBytes(1)
	_, _ = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Entries: []*PluginKeyValue{{Key: keyContentCounter, Value: counterBytes}},
	})
	return &PluginGenesisResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  BEGIN BLOCK — capture current height for use in DeliverTx
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) BeginBlock(req *PluginBeginRequest) *PluginBeginResponse {
	c.currentHeight = req.Height
	return &PluginBeginResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  CHECK TX — stateless validation + AuthorizedSigners
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) CheckTx(req *PluginCheckRequest) *PluginCheckResponse {
	switch req.Transaction.MessageType {

	case "publish_content":
		var msg MessagePublishContent
		if err := unmarshalProto(req.Transaction.Message, &msg); err != nil {
			return checkErr(14, "cannot decode publish_content message")
		}
		if strings.TrimSpace(msg.Title) == "" {
			return checkErr(ErrInvalidTitle, "title is required")
		}
		if len(msg.Title) > 200 {
			return checkErr(ErrInvalidTitle, "title exceeds 200 characters")
		}
		if strings.TrimSpace(msg.ContentHash) == "" {
			return checkErr(ErrInvalidHash, "content_hash is required")
		}
		if strings.TrimSpace(msg.LicenseTerms) == "" {
			return checkErr(ErrInvalidLicense, "license_terms is required")
		}
		if !validContentType(msg.ContentType) {
			return checkErr(ErrInvalidContentType, "content_type must be one of: article, image, audio, video, dataset, other")
		}
		if len(msg.Description) > 500 {
			return checkErr(ErrDescriptionTooLong, "description exceeds 500 characters")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.CreatorAddress}}

	case "purchase_license":
		var msg MessagePurchaseLicense
		if err := unmarshalProto(req.Transaction.Message, &msg); err != nil {
			return checkErr(14, "cannot decode purchase_license message")
		}
		if msg.ContentId == 0 {
			return checkErr(ErrContentNotFound, "content_id is required")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.BuyerAddress}}

	case "tip_creator":
		var msg MessageTipCreator
		if err := unmarshalProto(req.Transaction.Message, &msg); err != nil {
			return checkErr(14, "cannot decode tip_creator message")
		}
		if msg.AmountUfrg == 0 {
			return checkErr(ErrInvalidAmount, "tip amount must be greater than zero")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.SenderAddress}}

	case "send":
		var msg MessageSend
		if err := unmarshalProto(req.Transaction.Message, &msg); err != nil {
			return checkErr(14, "cannot decode send message")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.FromAddress}}
	}

	return checkErr(14, "unknown transaction type")
}

// ═══════════════════════════════════════════════════════════════════════
//  DELIVER TX — state-changing logic
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) DeliverTx(req *PluginDeliverRequest) *PluginDeliverResponse {
	switch req.Transaction.MessageType {

	case "publish_content":
		return c.handlePublishContent(req)

	case "purchase_license":
		return c.handlePurchaseLicense(req)

	case "tip_creator":
		return c.handleTipCreator(req)

	case "send":
		return c.handleSend(req)
	}

	return deliverErr(14, "unknown transaction type")
}

// ─── publish_content ───────────────────────────────────────────────────

func (c *Contract) handlePublishContent(req *PluginDeliverRequest) *PluginDeliverResponse {
	var msg MessagePublishContent
	if err := unmarshalProto(req.Transaction.Message, &msg); err != nil {
		return deliverErr(14, "decode error")
	}

	// Read the global counter
	readResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: [][]byte{keyContentCounter},
	})
	if err != nil {
		return deliverErr(14, "state read error: "+err.Error())
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	var nextID uint64 = 1
	if len(readResp.Entries) > 0 && len(readResp.Entries[0].Value) == 8 {
		nextID = bytesToUint64(readResp.Entries[0].Value)
	}

	// Build and store the ContentRecord
	record := ContentRecord{
		ID:             nextID,
		CreatorAddress: msg.CreatorAddress,
		Title:          msg.Title,
		ContentHash:    msg.ContentHash,
		LicenseTerms:   msg.LicenseTerms,
		PriceUfrg:      msg.PriceUfrg,
		ContentType:    msg.ContentType,
		Description:    msg.Description,
		PublishedAt:    c.currentHeight,
	}
	recordBytes, _ := json.Marshal(record)

	// Read existing creator stats
	statsKey := makeKey(prefixCreatorStats, msg.CreatorAddress)
	statsResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{Keys: [][]byte{statsKey}})
	if err != nil {
		return deliverErr(14, "state read error: "+err.Error())
	}
	var stats CreatorStats
	if len(statsResp.Entries) > 0 && len(statsResp.Entries[0].Value) > 0 {
		_ = json.Unmarshal(statsResp.Entries[0].Value, &stats)
	}
	stats.ContentCount++
	statsBytes, _ := json.Marshal(stats)

	// Write content record + updated counter + creator stats
	contentKey := makeKey(prefixContent, uint64ToBytes(nextID))
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Entries: []*PluginKeyValue{
			{Key: contentKey, Value: recordBytes},
			{Key: keyContentCounter, Value: uint64ToBytes(nextID + 1)},
			{Key: statsKey, Value: statsBytes},
		},
	})
	if err != nil {
		return deliverErr(14, "state write error: "+err.Error())
	}

	return &PluginDeliverResponse{
		Events: []*PluginEvent{{
			Type: "content_published",
			Attributes: []*PluginEventAttribute{
				{Key: "content_id", Value: uint64ToStr(nextID)},
				{Key: "creator", Value: bytesToHex(msg.CreatorAddress)},
				{Key: "title", Value: msg.Title},
			},
		}},
	}
}

// ─── purchase_license ──────────────────────────────────────────────────

func (c *Contract) handlePurchaseLicense(req *PluginDeliverRequest) *PluginDeliverResponse {
	var msg MessagePurchaseLicense
	if err := unmarshalProto(req.Transaction.Message, &msg); err != nil {
		return deliverErr(14, "decode error")
	}

	contentKey := makeKey(prefixContent, uint64ToBytes(msg.ContentId))
	licenseKey := makeLicenseKey(msg.BuyerAddress, msg.ContentId)

	// Read content + existing license in one call
	readResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: [][]byte{contentKey, licenseKey},
	})
	if err != nil {
		return deliverErr(14, "state read error: "+err.Error())
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	if len(readResp.Entries) < 1 || len(readResp.Entries[0].Value) == 0 {
		return deliverErr(ErrContentNotFound, "content not found")
	}

	var record ContentRecord
	if err := json.Unmarshal(readResp.Entries[0].Value, &record); err != nil {
		return deliverErr(14, "decode content record error")
	}

	// Prevent self-purchase
	if bytes.Equal(msg.BuyerAddress, record.CreatorAddress) {
		return deliverErr(ErrSelfPurchase, "cannot purchase your own content")
	}

	// Prevent duplicate license
	if len(readResp.Entries) > 1 && len(readResp.Entries[1].Value) > 0 {
		return deliverErr(ErrAlreadyLicensed, "already holds a license for this content")
	}

	// Validate amount matches price (skip if free)
	if record.PriceUfrg > 0 && msg.AmountUfrg < record.PriceUfrg {
		return deliverErr(ErrInsufficientFunds, "amount is less than content price")
	}

	// Build license record
	license := LicenseRecord{
		ContentID:    msg.ContentId,
		BuyerAddress: msg.BuyerAddress,
		AmountPaid:   msg.AmountUfrg,
		AcquiredAt:   c.currentHeight,
	}
	licenseBytes, _ := json.Marshal(license)

	// Update content: increment license count
	record.LicenseCount++
	recordBytes, _ := json.Marshal(record)

	// Read creator stats
	statsKey := makeKey(prefixCreatorStats, record.CreatorAddress)
	statsResp, _ := c.plugin.StateRead(c, &PluginStateReadRequest{Keys: [][]byte{statsKey}})
	var stats CreatorStats
	if len(statsResp.Entries) > 0 && len(statsResp.Entries[0].Value) > 0 {
		_ = json.Unmarshal(statsResp.Entries[0].Value, &stats)
	}
	stats.TotalEarnings += msg.AmountUfrg
	statsBytes, _ := json.Marshal(stats)

	// Write all state
	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Entries: []*PluginKeyValue{
			{Key: licenseKey, Value: licenseBytes},
			{Key: contentKey, Value: recordBytes},
			{Key: statsKey, Value: statsBytes},
		},
	})
	if err != nil {
		return deliverErr(14, "state write error: "+err.Error())
	}

	return &PluginDeliverResponse{
		Events: []*PluginEvent{{
			Type: "license_purchased",
			Attributes: []*PluginEventAttribute{
				{Key: "content_id", Value: uint64ToStr(msg.ContentId)},
				{Key: "buyer", Value: bytesToHex(msg.BuyerAddress)},
				{Key: "amount_ufrg", Value: uint64ToStr(msg.AmountUfrg)},
			},
		}},
	}
}

// ─── tip_creator ───────────────────────────────────────────────────────

func (c *Contract) handleTipCreator(req *PluginDeliverRequest) *PluginDeliverResponse {
	var msg MessageTipCreator
	if err := unmarshalProto(req.Transaction.Message, &msg); err != nil {
		return deliverErr(14, "decode error")
	}

	if msg.AmountUfrg == 0 {
		return deliverErr(ErrInvalidAmount, "tip amount must be greater than zero")
	}
	if bytes.Equal(msg.SenderAddress, msg.CreatorAddress) {
		return deliverErr(ErrSelfTip, "cannot tip yourself")
	}

	// Update creator stats
	statsKey := makeKey(prefixCreatorStats, msg.CreatorAddress)
	statsResp, err := c.plugin.StateRead(c, &PluginStateReadRequest{Keys: [][]byte{statsKey}})
	if err != nil {
		return deliverErr(14, "state read error: "+err.Error())
	}

	var stats CreatorStats
	if len(statsResp.Entries) > 0 && len(statsResp.Entries[0].Value) > 0 {
		_ = json.Unmarshal(statsResp.Entries[0].Value, &stats)
	}
	stats.TipTotal += msg.AmountUfrg
	stats.TotalEarnings += msg.AmountUfrg
	statsBytes, _ := json.Marshal(stats)

	_, err = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Entries: []*PluginKeyValue{{Key: statsKey, Value: statsBytes}},
	})
	if err != nil {
		return deliverErr(14, "state write error: "+err.Error())
	}

	return &PluginDeliverResponse{
		Events: []*PluginEvent{{
			Type: "creator_tipped",
			Attributes: []*PluginEventAttribute{
				{Key: "creator", Value: bytesToHex(msg.CreatorAddress)},
				{Key: "sender", Value: bytesToHex(msg.SenderAddress)},
				{Key: "amount_ufrg", Value: uint64ToStr(msg.AmountUfrg)},
			},
		}},
	}
}

// ─── send ──────────────────────────────────────────────────────────────

func (c *Contract) handleSend(req *PluginDeliverRequest) *PluginDeliverResponse {
	// The Canopy FSM handles token transfers natively via the send message type.
	// DeliverTx for send is a pass-through — the FSM already moved the tokens.
	return &PluginDeliverResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  END BLOCK
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) EndBlock(req *PluginEndRequest) *PluginEndResponse {
	return &PluginEndResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  HELPERS
// ═══════════════════════════════════════════════════════════════════════

func makeKey(prefix []byte, suffix []byte) []byte {
	key := make([]byte, len(prefix)+len(suffix))
	copy(key, prefix)
	copy(key[len(prefix):], suffix)
	return key
}

func makeLicenseKey(buyerAddr []byte, contentID uint64) []byte {
	idBytes := uint64ToBytes(contentID)
	key := make([]byte, len(prefixLicense)+len(buyerAddr)+len(idBytes))
	copy(key, prefixLicense)
	copy(key[len(prefixLicense):], buyerAddr)
	copy(key[len(prefixLicense)+len(buyerAddr):], idBytes)
	return key
}

func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func bytesToUint64(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

func uint64ToStr(v uint64) string {
	return fmt.Sprintf("%d", v)
}

func bytesToHex(b []byte) string {
	const hx = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hx[c>>4]
		out[i*2+1] = hx[c&0x0f]
	}
	return string(out)
}

func validContentType(t string) bool {
	switch t {
	case "article", "image", "audio", "video", "dataset", "other":
		return true
	}
	return false
}

func checkErr(code uint32, msg string) *PluginCheckResponse {
	return &PluginCheckResponse{Error: &PluginError{Code: code, Message: msg}}
}

func deliverErr(code uint32, msg string) *PluginDeliverResponse {
	return &PluginDeliverResponse{Error: &PluginError{Code: code, Message: msg}}
}

// unmarshalProto unmarshals protobuf binary data into the given message.
// Concrete message types are provided by the generated tx.pb.go file.
func unmarshalProto(data []byte, out proto.Message) error {
	return proto.Unmarshal(data, out)
}

