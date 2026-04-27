package contract

// ForgeCast custom error codes — all start at 15+ to avoid collision with Canopy reserved codes 1-14

const (
	// ErrInvalidTitle is returned when the content title is empty or too long
	ErrInvalidTitle = 15
	// ErrInvalidHash is returned when the content hash is empty
	ErrInvalidHash = 16
	// ErrInvalidLicense is returned when the license terms are empty
	ErrInvalidLicense = 17
	// ErrContentNotFound is returned when a content record cannot be found
	ErrContentNotFound = 18
	// ErrAlreadyLicensed is returned when the buyer already holds a license
	ErrAlreadyLicensed = 19
	// ErrInsufficientFunds is returned when the buyer cannot cover the price
	ErrInsufficientFunds = 20
	// ErrInvalidAmount is returned when a payment or tip amount is zero or negative
	ErrInvalidAmount = 21
	// ErrSelfPurchase is returned when a creator tries to buy their own content
	ErrSelfPurchase = 22
	// ErrSelfTip is returned when a creator tries to tip themselves
	ErrSelfTip = 23
	// ErrInvalidContentType is returned when an unrecognised content type is submitted
	ErrInvalidContentType = 24
	// ErrDescriptionTooLong is returned when the description exceeds 500 characters
	ErrDescriptionTooLong = 25
)
