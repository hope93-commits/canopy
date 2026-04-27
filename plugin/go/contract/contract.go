package contract

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

// ═══════════════════════════════════════════════════════════════════════
//  ContractConfig — *PluginConfig variable consumed by plugin.go line 54
// ═══════════════════════════════════════════════════════════════════════

var ContractConfig = &PluginConfig{
	Name:    "ForgeCast",
	Id:      2,
	Version: 1,
	SupportedTransactions: []string{
		"send",
		"publish_content",
		"purchase_license",
		"tip_creator",
	},
}

// ═══════════════════════════════════════════════════════════════════════
//  STATE KEY PREFIXES — 0x01-0x0F reserved by Canopy, ForgeCast uses 0x10+
// ═══════════════════════════════════════════════════════════════════════

var (
	prefixContent      = []byte{0x10}
	prefixLicense      = []byte{0x11}
	prefixCreatorStats = []byte{0x12}
	keyContentCounter  = []byte{0x13, 0x00}
)

// ═══════════════════════════════════════════════════════════════════════
//  DATA MODELS
// ═══════════════════════════════════════════════════════════════════════

type ContentRecord struct {
	ID             uint64 `json:"id"`
	CreatorAddress []byte `json:"creator_address"`
	Title          string `json:"title"`
	ContentHash    string `json:"content_hash"`
	LicenseTerms   string `json:"license_terms"`
	PriceUfrg      uint64 `json:"price_ufrg"`
	ContentType    string `json:"content_type"`
	Description    string `json:"description"`
	PublishedAt    uint64 `json:"published_at"`
	LicenseCount   uint64 `json:"license_count"`
	TipTotal       uint64 `json:"tip_total"`
}

type LicenseRecord struct {
	ContentID    uint64 `json:"content_id"`
	BuyerAddress []byte `json:"buyer_address"`
	AmountPaid   uint64 `json:"amount_paid"`
	AcquiredAt   uint64 `json:"acquired_at"`
}

type CreatorStats struct {
	TotalEarnings uint64 `json:"total_earnings"`
	ContentCount  uint64 `json:"content_count"`
	TipTotal      uint64 `json:"tip_total"`
}

// ═══════════════════════════════════════════════════════════════════════
//  CONTRACT STRUCT
// ═══════════════════════════════════════════════════════════════════════

type Contract struct {
	Config        Config
	FSMConfig     *PluginFSMConfig
	plugin        *Plugin
	fsmId         uint64
	currentHeight uint64
}

// ═══════════════════════════════════════════════════════════════════════
//  GENESIS
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) Genesis(_ *PluginGenesisRequest) *PluginGenesisResponse {
	_, _ = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: keyContentCounter, Value: uint64ToBytes(1)},
		},
	})
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
//  CHECK TX — stateless only, no state reads
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) CheckTx(req *PluginCheckRequest) *PluginCheckResponse {
	tx := req.Tx
	if tx == nil {
		return checkErr(14, "nil transaction")
	}
	switch tx.MessageType {

	case "publish_content":
		var msg MessagePublishContent
		if err := unmarshalAny(tx.Msg, &msg); err != nil {
			return checkErr(14, "cannot decode publish_content")
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
			return checkErr(ErrInvalidContentType, "content_type must be: article, image, audio, video, dataset, other")
		}
		if len(msg.Description) > 500 {
			return checkErr(ErrDescriptionTooLong, "description exceeds 500 characters")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.CreatorAddress}}

	case "purchase_license":
		var msg MessagePurchaseLicense
		if err := unmarshalAny(tx.Msg, &msg); err != nil {
			return checkErr(14, "cannot decode purchase_license")
		}
		if msg.ContentId == 0 {
			return checkErr(ErrContentNotFound, "content_id is required")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.BuyerAddress}}

	case "tip_creator":
		var msg MessageTipCreator
		if err := unmarshalAny(tx.Msg, &msg); err != nil {
			return checkErr(14, "cannot decode tip_creator")
		}
		if msg.AmountUfrg == 0 {
			return checkErr(ErrInvalidAmount, "tip amount must be greater than zero")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.SenderAddress}}

	case "send":
		var msg MessageSend
		if err := unmarshalAny(tx.Msg, &msg); err != nil {
			return checkErr(14, "cannot decode send")
		}
		return &PluginCheckResponse{AuthorizedSigners: [][]byte{msg.FromAddress}}
	}

	return checkErr(14, "unknown transaction type")
}

// ═══════════════════════════════════════════════════════════════════════
//  DELIVER TX
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) DeliverTx(req *PluginDeliverRequest) *PluginDeliverResponse {
	tx := req.Tx
	if tx == nil {
		return deliverErr(14, "nil transaction")
	}
	switch tx.MessageType {
	case "publish_content":
		return c.handlePublishContent(tx)
	case "purchase_license":
		return c.handlePurchaseLicense(tx)
	case "tip_creator":
		return c.handleTipCreator(tx)
	case "send":
		return &PluginDeliverResponse{}
	}
	return deliverErr(14, "unknown transaction type")
}

// ─── publish_content ───────────────────────────────────────────────────

func (c *Contract) handlePublishContent(tx *Transaction) *PluginDeliverResponse {
	var msg MessagePublishContent
	if err := unmarshalAny(tx.Msg, &msg); err != nil {
		return deliverErr(14, "decode error")
	}

	readResp, pluginErr := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{{QueryId: 1, Key: keyContentCounter}},
	})
	if pluginErr != nil {
		return deliverErr(14, pluginErr.Msg)
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	var nextID uint64 = 1
	for _, r := range readResp.Results {
		if r.QueryId == 1 && len(r.Entries) > 0 {
			if v := r.Entries[0].Value; len(v) == 8 {
				nextID = bytesToUint64(v)
			}
		}
	}

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
	contentKey := makeKey(prefixContent, uint64ToBytes(nextID))

	statsKey := makeKey(prefixCreatorStats, msg.CreatorAddress)
	var stats CreatorStats
	if sr, _ := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{{QueryId: 1, Key: statsKey}},
	}); sr != nil && len(sr.Results) > 0 && len(sr.Results[0].Entries) > 0 {
		_ = json.Unmarshal(sr.Results[0].Entries[0].Value, &stats)
	}
	stats.ContentCount++
	statsBytes, _ := json.Marshal(stats)

	if _, pluginErr = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: contentKey, Value: recordBytes},
			{Key: keyContentCounter, Value: uint64ToBytes(nextID + 1)},
			{Key: statsKey, Value: statsBytes},
		},
	}); pluginErr != nil {
		return deliverErr(14, pluginErr.Msg)
	}

	return &PluginDeliverResponse{
		Events: []*Event{makeEvent("content_published", map[string]string{
			"content_id": fmt.Sprintf("%d", nextID),
			"creator":    bytesToHex(msg.CreatorAddress),
			"title":      msg.Title,
		})},
	}
}

// ─── purchase_license ──────────────────────────────────────────────────

func (c *Contract) handlePurchaseLicense(tx *Transaction) *PluginDeliverResponse {
	var msg MessagePurchaseLicense
	if err := unmarshalAny(tx.Msg, &msg); err != nil {
		return deliverErr(14, "decode error")
	}

	contentKey := makeKey(prefixContent, uint64ToBytes(msg.ContentId))
	licenseKey := makeLicenseKey(msg.BuyerAddress, msg.ContentId)

	readResp, pluginErr := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{
			{QueryId: 1, Key: contentKey},
			{QueryId: 2, Key: licenseKey},
		},
	})
	if pluginErr != nil {
		return deliverErr(14, pluginErr.Msg)
	}
	if readResp.Error != nil {
		return &PluginDeliverResponse{Error: readResp.Error}
	}

	var contentVal, licenseVal []byte
	for _, r := range readResp.Results {
		if r.QueryId == 1 && len(r.Entries) > 0 {
			contentVal = r.Entries[0].Value
		}
		if r.QueryId == 2 && len(r.Entries) > 0 {
			licenseVal = r.Entries[0].Value
		}
	}

	if len(contentVal) == 0 {
		return deliverErr(ErrContentNotFound, "content not found")
	}
	var record ContentRecord
	if err := json.Unmarshal(contentVal, &record); err != nil {
		return deliverErr(14, "decode content record error")
	}
	if bytes.Equal(msg.BuyerAddress, record.CreatorAddress) {
		return deliverErr(ErrSelfPurchase, "cannot purchase your own content")
	}
	if len(licenseVal) > 0 {
		return deliverErr(ErrAlreadyLicensed, "already holds a license for this content")
	}
	if record.PriceUfrg > 0 && msg.AmountUfrg < record.PriceUfrg {
		return deliverErr(ErrInsufficientFunds, "amount is less than content price")
	}

	license := LicenseRecord{
		ContentID:    msg.ContentId,
		BuyerAddress: msg.BuyerAddress,
		AmountPaid:   msg.AmountUfrg,
		AcquiredAt:   c.currentHeight,
	}
	licenseBytes, _ := json.Marshal(license)
	record.LicenseCount++
	recordBytes, _ := json.Marshal(record)

	statsKey := makeKey(prefixCreatorStats, record.CreatorAddress)
	var stats CreatorStats
	if sr, _ := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{{QueryId: 1, Key: statsKey}},
	}); sr != nil && len(sr.Results) > 0 && len(sr.Results[0].Entries) > 0 {
		_ = json.Unmarshal(sr.Results[0].Entries[0].Value, &stats)
	}
	stats.TotalEarnings += msg.AmountUfrg
	statsBytes, _ := json.Marshal(stats)

	if _, pluginErr = c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{
			{Key: licenseKey, Value: licenseBytes},
			{Key: contentKey, Value: recordBytes},
			{Key: statsKey, Value: statsBytes},
		},
	}); pluginErr != nil {
		return deliverErr(14, pluginErr.Msg)
	}

	return &PluginDeliverResponse{
		Events: []*Event{makeEvent("license_purchased", map[string]string{
			"content_id":  fmt.Sprintf("%d", msg.ContentId),
			"buyer":       bytesToHex(msg.BuyerAddress),
			"amount_ufrg": fmt.Sprintf("%d", msg.AmountUfrg),
		})},
	}
}

// ─── tip_creator ───────────────────────────────────────────────────────

func (c *Contract) handleTipCreator(tx *Transaction) *PluginDeliverResponse {
	var msg MessageTipCreator
	if err := unmarshalAny(tx.Msg, &msg); err != nil {
		return deliverErr(14, "decode error")
	}
	if msg.AmountUfrg == 0 {
		return deliverErr(ErrInvalidAmount, "tip amount must be greater than zero")
	}
	if bytes.Equal(msg.SenderAddress, msg.CreatorAddress) {
		return deliverErr(ErrSelfTip, "cannot tip yourself")
	}

	statsKey := makeKey(prefixCreatorStats, msg.CreatorAddress)
	var stats CreatorStats
	if sr, _ := c.plugin.StateRead(c, &PluginStateReadRequest{
		Keys: []*PluginKeyRead{{QueryId: 1, Key: statsKey}},
	}); sr != nil && len(sr.Results) > 0 && len(sr.Results[0].Entries) > 0 {
		_ = json.Unmarshal(sr.Results[0].Entries[0].Value, &stats)
	}
	stats.TipTotal += msg.AmountUfrg
	stats.TotalEarnings += msg.AmountUfrg
	statsBytes, _ := json.Marshal(stats)

	if _, pluginErr := c.plugin.StateWrite(c, &PluginStateWriteRequest{
		Sets: []*PluginSetOp{{Key: statsKey, Value: statsBytes}},
	}); pluginErr != nil {
		return deliverErr(14, pluginErr.Msg)
	}

	return &PluginDeliverResponse{
		Events: []*Event{makeEvent("creator_tipped", map[string]string{
			"creator":     bytesToHex(msg.CreatorAddress),
			"sender":      bytesToHex(msg.SenderAddress),
			"amount_ufrg": fmt.Sprintf("%d", msg.AmountUfrg),
		})},
	}
}

// ═══════════════════════════════════════════════════════════════════════
//  END BLOCK
// ═══════════════════════════════════════════════════════════════════════

func (c *Contract) EndBlock(_ *PluginEndRequest) *PluginEndResponse {
	return &PluginEndResponse{}
}

// ═══════════════════════════════════════════════════════════════════════
//  HELPERS
// ═══════════════════════════════════════════════════════════════════════

func makeKey(prefix, suffix []byte) []byte {
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

func bytesToHex(b []byte) string {
	const hx = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, ch := range b {
		out[i*2] = hx[ch>>4]
		out[i*2+1] = hx[ch&0x0f]
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

func unmarshalAny(a *anypb.Any, out proto.Message) error {
	if a == nil {
		return fmt.Errorf("nil any")
	}
	return proto.Unmarshal(a.Value, out)
}

func makeEvent(eventType string, attrs map[string]string) *Event {
	b, _ := json.Marshal(attrs)
	return &Event{
		EventType: eventType,
		Msg: &Event_Custom{Custom: &EventCustom{
			Msg: &anypb.Any{Value: b},
		}},
	}
}

func checkErr(code uint64, msg string) *PluginCheckResponse {
	return &PluginCheckResponse{Error: &PluginError{Code: code, Msg: msg}}
}

func deliverErr(code uint64, msg string) *PluginDeliverResponse {
	return &PluginDeliverResponse{Error: &PluginError{Code: code, Msg: msg}}
}
