package ralph

import (
	"fmt"
	"strings"
)

// failureSourceVerification names the verification command in a failure-output
// block's delimiters. Every other source is a subscription, which names itself,
// so this is the only constant needed: verification is the one failing thing
// that does not run through the engine.
const failureSourceVerification = "verification"

// printFailureBlock renders one failure-output block: the failure text framed by
// delimiter lines naming where it came from, so what the next prompt is about to
// carry stands out from the loop's status lines.
//
// Every path that shows a human failure output renders through here - the
// verification command at its call site, after-workitem and after-phase
// validators through the engine's resolution ledger - so the framing is the same
// wherever the failure came from and no path can quietly grow its own. Exactly
// one block is printed per failure: the ledger is the only printer for anything
// that resolves through the engine, and the call sites do not print again.
//
// The content is printed verbatim rather than reformatted, so the block and the
// feedback injected into the next prompt cannot disagree. Empty content prints
// nothing rather than an empty frame.
func printFailureBlock(source, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	fmt.Printf("\n--- Failure Output: %s ---\n%s\n--- End Failure Output: %s ---\n\n", source, content, source)
}
