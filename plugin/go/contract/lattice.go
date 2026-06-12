package contract

import (
"math/rand"

"google.golang.org/protobuf/proto"
)

// ── CheckTx handlers ──────────────────────────────────────────────────────

func (c *Contract) CheckMessageRegisterProtocol(msg *MessageRegisterProtocol) *PluginCheckResponse {
if len(msg.AdminAddress) != 20 {
return &PluginCheckResponse{Error: ErrInvalidAddress()}
}
if msg.ProtocolName == "" {
return &PluginCheckResponse{Error: ErrInvalidAmount()}
}
return &PluginCheckResponse{
AuthorizedSigners: [][]byte{msg.AdminAddress},
}
}

func (c *Contract) CheckMessageEmitReputationEvent(msg *MessageEmitReputationEvent) *PluginCheckResponse {
if len(msg.ProtocolAddress) != 20 {
return &PluginCheckResponse{Error: ErrInvalidAddress()}
}
if len(msg.SubjectAddress) != 20 {
return &PluginCheckResponse{Error: ErrInvalidAddress()}
}
if msg.EventType == "" {
return &PluginCheckResponse{Error: ErrInvalidAmount()}
}
return &PluginCheckResponse{
AuthorizedSigners: [][]byte{msg.ProtocolAddress},
}
}

func (c *Contract) CheckMessageFollowAddress(msg *MessageFollowAddress) *PluginCheckResponse {
if len(msg.FollowerAddress) != 20 || len(msg.TargetAddress) != 20 {
return &PluginCheckResponse{Error: ErrInvalidAddress()}
}
return &PluginCheckResponse{
AuthorizedSigners: [][]byte{msg.FollowerAddress},
}
}

func (c *Contract) CheckMessageUnfollowAddress(msg *MessageUnfollowAddress) *PluginCheckResponse {
if len(msg.FollowerAddress) != 20 || len(msg.TargetAddress) != 20 {
return &PluginCheckResponse{Error: ErrInvalidAddress()}
}
return &PluginCheckResponse{
AuthorizedSigners: [][]byte{msg.FollowerAddress},
}
}

func (c *Contract) CheckMessageEndorseAddress(msg *MessageEndorseAddress) *PluginCheckResponse {
if len(msg.EndorserAddress) != 20 || len(msg.SubjectAddress) != 20 {
return &PluginCheckResponse{Error: ErrInvalidAddress()}
}
if msg.StakeAmount == 0 {
return &PluginCheckResponse{Error: ErrInvalidAmount()}
}
return &PluginCheckResponse{
AuthorizedSigners: [][]byte{msg.EndorserAddress},
}
}

func (c *Contract) CheckMessageRevokeEndorsement(msg *MessageRevokeEndorsement) *PluginCheckResponse {
if len(msg.EndorserAddress) != 20 || len(msg.SubjectAddress) != 20 {
return &PluginCheckResponse{Error: ErrInvalidAddress()}
}
return &PluginCheckResponse{
AuthorizedSigners: [][]byte{msg.EndorserAddress},
}
}

// ── DeliverTx handlers ────────────────────────────────────────────────────

func (c *Contract) DeliverMessageRegisterProtocol(msg *MessageRegisterProtocol, fee uint64) *PluginDeliverResponse {
// Deduct fee from admin account
if err := c.deductFee(msg.AdminAddress, fee); err != nil {
return &PluginDeliverResponse{Error: err}
}
protocol := &Protocol{
AdminAddress: msg.AdminAddress,
ProtocolName: msg.ProtocolName,
ProtocolUrl:  msg.ProtocolUrl,
BlockHeight:  c.currentHeight,
}
bz, pluginErr := marshalProto(protocol)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
writeResp, pluginErr := c.plugin.StateWrite(c, &PluginStateWriteRequest{
Sets: []*PluginSetOp{
{Key: KeyForProtocol(msg.AdminAddress), Value: bz},
},
})
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
if writeResp.Error != nil {
return &PluginDeliverResponse{Error: writeResp.Error}
}
return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageEmitReputationEvent(msg *MessageEmitReputationEvent, fee uint64) *PluginDeliverResponse {
// Deduct fee
if err := c.deductFee(msg.ProtocolAddress, fee); err != nil {
return &PluginDeliverResponse{Error: err}
}
// Load subject profile
profile, pluginErr := c.loadProfile(msg.SubjectAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
// Use provided base_points or derive from event type
points := msg.BasePoints
if points == 0 {
points = BasePointsForEventType(msg.EventType)
}
// Append the event
profile.Events = append(profile.Events, &ReputationEvent{
ProtocolAddress: msg.ProtocolAddress,
EventType:       msg.EventType,
BasePoints:      points,
BlockHeight:     c.currentHeight,
})
// Recompute score
profile.RawScore = ComputeScore(profile, c.currentHeight)
return c.saveProfile(profile)
}

func (c *Contract) DeliverMessageFollowAddress(msg *MessageFollowAddress, fee uint64) *PluginDeliverResponse {
if err := c.deductFee(msg.FollowerAddress, fee); err != nil {
return &PluginDeliverResponse{Error: err}
}
follower, pluginErr := c.loadProfile(msg.FollowerAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
target, pluginErr := c.loadProfile(msg.TargetAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
// Idempotent: only add if not already following
if !containsAddr(follower.Following, msg.TargetAddress) {
follower.Following = append(follower.Following, msg.TargetAddress)
target.Followers = append(target.Followers, msg.FollowerAddress)
}
if err := c.saveProfile(follower); err.Error != nil {
return err
}
return c.saveProfile(target)
}

func (c *Contract) DeliverMessageUnfollowAddress(msg *MessageUnfollowAddress, fee uint64) *PluginDeliverResponse {
if err := c.deductFee(msg.FollowerAddress, fee); err != nil {
return &PluginDeliverResponse{Error: err}
}
follower, pluginErr := c.loadProfile(msg.FollowerAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
target, pluginErr := c.loadProfile(msg.TargetAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
follower.Following = removeAddr(follower.Following, msg.TargetAddress)
target.Followers = removeAddr(target.Followers, msg.FollowerAddress)
if err := c.saveProfile(follower); err.Error != nil {
return err
}
return c.saveProfile(target)
}

func (c *Contract) DeliverMessageEndorseAddress(msg *MessageEndorseAddress, fee uint64) *PluginDeliverResponse {
// Load endorser account and check balance
endorserAccQId := rand.Uint64()
readResp, pluginErr := c.plugin.StateRead(c, &PluginStateReadRequest{
Keys: []*PluginKeyRead{
{QueryId: endorserAccQId, Key: KeyForAccount(msg.EndorserAddress)},
},
})
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
endorserAcc := &Account{}
for _, r := range readResp.Results {
if r.QueryId == endorserAccQId && len(r.Entries) > 0 {
if err := Unmarshal(r.Entries[0].Value, endorserAcc); err != nil {
return &PluginDeliverResponse{Error: err}
}
}
}
total := fee + msg.StakeAmount
if endorserAcc.Amount < total {
return &PluginDeliverResponse{Error: ErrInsufficientFunds()}
}
endorserAcc.Amount -= total

// Load profiles
endorserProfile, pluginErr := c.loadProfile(msg.EndorserAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
subjectProfile, pluginErr := c.loadProfile(msg.SubjectAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}

// Record endorsement
endorserProfile.EndorsementStake += msg.StakeAmount
if !containsAddr(endorserProfile.Endorsing, msg.SubjectAddress) {
endorserProfile.Endorsing = append(endorserProfile.Endorsing, msg.SubjectAddress)
}
subjectProfile.EndorsementsReceived = append(subjectProfile.EndorsementsReceived, &Endorsement{
EndorserAddress:       msg.EndorserAddress,
StakeAmount:           msg.StakeAmount,
EndorserScoreSnapshot: endorserProfile.RawScore,
BlockHeight:           c.currentHeight,
})
subjectProfile.RawScore = ComputeScore(subjectProfile, c.currentHeight)

// Write all state
accBz, err := marshalProto(endorserAcc)
if err != nil {
return &PluginDeliverResponse{Error: err}
}
endorserBz, err := marshalProto(endorserProfile)
if err != nil {
return &PluginDeliverResponse{Error: err}
}
subjectBz, err := marshalProto(subjectProfile)
if err != nil {
return &PluginDeliverResponse{Error: err}
}
writeResp, pluginErr := c.plugin.StateWrite(c, &PluginStateWriteRequest{
Sets: []*PluginSetOp{
{Key: KeyForAccount(msg.EndorserAddress), Value: accBz},
{Key: KeyForProfile(msg.EndorserAddress), Value: endorserBz},
{Key: KeyForProfile(msg.SubjectAddress), Value: subjectBz},
},
})
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
if writeResp.Error != nil {
return &PluginDeliverResponse{Error: writeResp.Error}
}
return &PluginDeliverResponse{}
}

func (c *Contract) DeliverMessageRevokeEndorsement(msg *MessageRevokeEndorsement, fee uint64) *PluginDeliverResponse {
if err := c.deductFee(msg.EndorserAddress, fee); err != nil {
return &PluginDeliverResponse{Error: err}
}
endorserProfile, pluginErr := c.loadProfile(msg.EndorserAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
subjectProfile, pluginErr := c.loadProfile(msg.SubjectAddress)
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}

// Find and remove the endorsement, refund stake
var refund uint64
var remaining []*Endorsement
for _, e := range subjectProfile.EndorsementsReceived {
if string(e.EndorserAddress) == string(msg.EndorserAddress) {
refund = e.StakeAmount
} else {
remaining = append(remaining, e)
}
}
subjectProfile.EndorsementsReceived = remaining
subjectProfile.RawScore = ComputeScore(subjectProfile, c.currentHeight)
endorserProfile.Endorsing = removeAddr(endorserProfile.Endorsing, msg.SubjectAddress)
endorserProfile.EndorsementStake -= refund

// Refund stake to endorser account
accQId := rand.Uint64()
readResp, pluginErr := c.plugin.StateRead(c, &PluginStateReadRequest{
Keys: []*PluginKeyRead{{QueryId: accQId, Key: KeyForAccount(msg.EndorserAddress)}},
})
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
acc := &Account{}
for _, r := range readResp.Results {
if r.QueryId == accQId && len(r.Entries) > 0 {
Unmarshal(r.Entries[0].Value, acc)
}
}
acc.Amount += refund

accBz, err := marshalProto(acc)
if err != nil {
return &PluginDeliverResponse{Error: err}
}
endorserBz, err := marshalProto(endorserProfile)
if err != nil {
return &PluginDeliverResponse{Error: err}
}
subjectBz, err := marshalProto(subjectProfile)
if err != nil {
return &PluginDeliverResponse{Error: err}
}
writeResp, pluginErr := c.plugin.StateWrite(c, &PluginStateWriteRequest{
Sets: []*PluginSetOp{
{Key: KeyForAccount(msg.EndorserAddress), Value: accBz},
{Key: KeyForProfile(msg.EndorserAddress), Value: endorserBz},
{Key: KeyForProfile(msg.SubjectAddress), Value: subjectBz},
},
})
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
if writeResp.Error != nil {
return &PluginDeliverResponse{Error: writeResp.Error}
}
return &PluginDeliverResponse{}
}

// ── Helpers ───────────────────────────────────────────────────────────────

func (c *Contract) loadProfile(address []byte) (*LatticeProfile, *PluginError) {
qId := rand.Uint64()
readResp, pluginErr := c.plugin.StateRead(c, &PluginStateReadRequest{
Keys: []*PluginKeyRead{{QueryId: qId, Key: KeyForProfile(address)}},
})
if pluginErr != nil {
return nil, pluginErr
}
profile := &LatticeProfile{Address: address}
for _, r := range readResp.Results {
if r.QueryId == qId && len(r.Entries) > 0 {
if err := Unmarshal(r.Entries[0].Value, profile); err != nil {
return nil, err
}
}
}
return profile, nil
}

func (c *Contract) saveProfile(profile *LatticeProfile) *PluginDeliverResponse {
bz, err := marshalProto(profile)
if err != nil {
return &PluginDeliverResponse{Error: err}
}
writeResp, pluginErr := c.plugin.StateWrite(c, &PluginStateWriteRequest{
Sets: []*PluginSetOp{{Key: KeyForProfile(profile.Address), Value: bz}},
})
if pluginErr != nil {
return &PluginDeliverResponse{Error: pluginErr}
}
if writeResp.Error != nil {
return &PluginDeliverResponse{Error: writeResp.Error}
}
return &PluginDeliverResponse{}
}

func (c *Contract) deductFee(address []byte, fee uint64) *PluginError {
if fee == 0 {
return nil
}
qId := rand.Uint64()
readResp, pluginErr := c.plugin.StateRead(c, &PluginStateReadRequest{
Keys: []*PluginKeyRead{{QueryId: qId, Key: KeyForAccount(address)}},
})
if pluginErr != nil {
return pluginErr
}
acc := &Account{}
for _, r := range readResp.Results {
if r.QueryId == qId && len(r.Entries) > 0 {
Unmarshal(r.Entries[0].Value, acc)
}
}
if acc.Amount < fee {
return ErrInsufficientFunds()
}
acc.Amount -= fee
bz, err := marshalProto(acc)
if err != nil {
return err
}
writeResp, pluginErr := c.plugin.StateWrite(c, &PluginStateWriteRequest{
Sets: []*PluginSetOp{{Key: KeyForAccount(address), Value: bz}},
})
if pluginErr != nil {
return pluginErr
}
return writeResp.Error
}

func marshalProto(m proto.Message) ([]byte, *PluginError) {
bz, err := proto.Marshal(m)
if err != nil {
return nil, ErrMarshal(err)
}
return bz, nil
}


func containsAddr(list [][]byte, addr []byte) bool {
for _, a := range list {
if string(a) == string(addr) {
return true
}
}
return false
}

func removeAddr(list [][]byte, addr []byte) [][]byte {
result := make([][]byte, 0, len(list))
for _, a := range list {
if string(a) != string(addr) {
result = append(result, a)
}
}
return result
}
