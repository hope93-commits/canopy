package contract

import "fmt"

// ═══════════════════════════════════════════════════════════════════════
//  ERROR CODES
//  Canopy built-in codes: 1–14. ForgeCast custom codes start at 15.
// ═══════════════════════════════════════════════════════════════════════

const (
	// Built-in Canopy codes (do not reuse)
	codeTimeout         = 1
	codeMarshal         = 2
	codeUnmarshal       = 3
	codePluginRead      = 4
	codePluginWrite     = 5
	codeInvalidRespID   = 6
	codeUnexpectedType  = 7
	codeInvalidMsg      = 8
	codeInsufficientFunds = 9
	codeFromAny         = 10
	codeInvalidMsgCast  = 11
	codeInvalidAddress  = 12
	codeInvalidAmount   = 13
	codeFeeBelowLimit   = 14

	// ForgeCast custom codes
	codeInvalidContentHash = 15
	codeInvalidTitle       = 16
	codeInvalidLicense     = 17
	codeContentNotFound    = 18
	codeSupplyExhausted    = 19
	codeAlreadyLicensed    = 20
)

const module = "forgecast"

func newErr(code uint64, msg string) *PluginError {
	return &PluginError{Code: code, Module: module, Msg: msg}
}

// ── built-in wrappers ─────────────────────────────────────────────────

func ErrPluginTimeout() *PluginError {
	return newErr(codeTimeout, "plugin timeout")
}
func ErrMarshal(err error) *PluginError {
	return newErr(codeMarshal, fmt.Sprintf("marshal: %s", err))
}
func ErrUnmarshal(err error) *PluginError {
	return newErr(codeUnmarshal, fmt.Sprintf("unmarshal: %s", err))
}
func ErrPluginRead(err error) *PluginError {
	return newErr(codePluginRead, fmt.Sprintf("state read: %s", err))
}
func ErrPluginWrite(err error) *PluginError {
	return newErr(codePluginWrite, fmt.Sprintf("state write: %s", err))
}
func ErrInvalidPluginRespId() *PluginError {
	return newErr(codeInvalidRespID, "invalid response id")
}
func ErrUnexpectedFSMToPlugin(t interface{}) *PluginError {
	return newErr(codeUnexpectedType, fmt.Sprintf("unexpected FSM type: %T", t))
}
func ErrInvalidFSMToPluginMMessage(t interface{}) *PluginError {
	return newErr(codeInvalidMsg, fmt.Sprintf("invalid FSM message: %T", t))
}
func ErrInvalidMessage() *PluginError {
	return newErr(codeInvalidMsg, "nil or invalid transaction message")
}
func ErrInsufficientFunds() *PluginError {
	return newErr(codeInsufficientFunds, "insufficient funds")
}
func ErrFromAny(err error) *PluginError {
	return newErr(codeFromAny, fmt.Sprintf("from any: %s", err))
}
func ErrInvalidMessageCast() *PluginError {
	return newErr(codeInvalidMsgCast, "invalid message cast")
}
func ErrInvalidAddress() *PluginError {
	return newErr(codeInvalidAddress, "address must be exactly 20 bytes")
}
func ErrInvalidAmount() *PluginError {
	return newErr(codeInvalidAmount, "amount must be greater than zero")
}
func ErrTxFeeBelowStateLimit() *PluginError {
	return newErr(codeFeeBelowLimit, "fee below minimum")
}
func ErrFailedPluginWrite(err error) *PluginError {
	return newErr(codePluginWrite, fmt.Sprintf("write failed: %s", err))
}
func ErrFailedPluginRead(err error) *PluginError {
	return newErr(codePluginRead, fmt.Sprintf("read failed: %s", err))
}

// ── ForgeCast custom errors ───────────────────────────────────────────

func ErrInvalidContentHash() *PluginError {
	return newErr(codeInvalidContentHash, "content hash must not be empty")
}
func ErrInvalidTitle() *PluginError {
	return newErr(codeInvalidTitle, "title must not be empty")
}
func ErrInvalidLicense() *PluginError {
	return newErr(codeInvalidLicense, "license must be: free, payPerView, commercial, or collectible")
}
func ErrContentNotFound() *PluginError {
	return newErr(codeContentNotFound, "content not found")
}
func ErrSupplyExhausted() *PluginError {
	return newErr(codeSupplyExhausted, "max supply reached for this content")
}
func ErrAlreadyLicensed() *PluginError {
	return newErr(codeAlreadyLicensed, "buyer already holds a license for this content")
}
