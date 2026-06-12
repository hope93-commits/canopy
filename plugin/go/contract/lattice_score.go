package contract

import "math"

// Lattice reputation scoring with time-weighted decay
//
// Score = sum of all events where each event contributes:
//   base_points * decay_factor
//   decay_factor = 0.5 ^ (age_in_blocks / half_life)

const (
// HalfLifeBlocks: reputation event half-life in blocks
// At ~24s per block, 3600 blocks ≈ 24 hours
HalfLifeBlocks = uint64(3600)

// Base point values by event type
PointsVoteCast          = uint64(10)
PointsDisputeResolved   = uint64(25)
PointsTradeCompleted    = uint64(15)
PointsLoanRepaid        = uint64(20)
PointsEndorsementBonus  = uint64(30)
PointsProtocolRegistered = uint64(5)
PointsDefault           = uint64(10)
)

// BasePointsForEventType returns the base point value for a given event type string
func BasePointsForEventType(eventType string) uint64 {
switch eventType {
case "vote_cast":
return PointsVoteCast
case "dispute_resolved":
return PointsDisputeResolved
case "trade_completed":
return PointsTradeCompleted
case "loan_repaid":
return PointsLoanRepaid
case "endorsement_received":
return PointsEndorsementBonus
case "protocol_registered":
return PointsProtocolRegistered
default:
return PointsDefault
}
}

// DecayFactor returns the decay multiplier for an event given its age in blocks
// Uses exponential decay: 0.5 ^ (age / half_life)
func DecayFactor(currentHeight, eventHeight uint64) float64 {
if currentHeight <= eventHeight {
return 1.0
}
age := float64(currentHeight - eventHeight)
halfLife := float64(HalfLifeBlocks)
return math.Pow(0.5, age/halfLife)
}

// ComputeScore calculates the decayed reputation score for a profile
func ComputeScore(profile *LatticeProfile, currentHeight uint64) uint64 {
var total float64
for _, event := range profile.Events {
decay := DecayFactor(currentHeight, event.BlockHeight)
total += float64(event.BasePoints) * decay
}
// Add endorsement weight: each endorsement contributes
// stake_amount * (endorser_score_snapshot / 1000) as a bonus
for _, endorsement := range profile.EndorsementsReceived {
weight := float64(endorsement.StakeAmount) * (float64(endorsement.EndorserScoreSnapshot) / 1000.0)
total += weight * DecayFactor(currentHeight, endorsement.BlockHeight)
}
return uint64(total)
}
