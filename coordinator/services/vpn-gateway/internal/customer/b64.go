package customer

import "encoding/base64"

// stdB64 is the alphabet WireGuard's `wg pubkey` uses (std base64).
var stdB64 = base64.StdEncoding
