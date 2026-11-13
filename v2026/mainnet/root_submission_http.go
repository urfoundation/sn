// Native submission retains its original approval and method boundary while
// sharing only fixed-route HTTP mechanics with the separate EVM action owner.
package main

import "context"

// The native route projection changes no serialized approval or signature.
func rootSubmissionTransport(approval rootSubmissionApproval) ownedSubmissionRoute {
	return ownedSubmissionRoute{RpcUrl: approval.RpcUrl, TlsSpkiHash: approval.TlsSpkiHash, ReadRetrySeconds: approval.ReadRetrySeconds, SendTimeoutSeconds: approval.SendTimeoutSeconds}
}

// Native route validation retains the original identity and TLS constraints.
func rootSubmissionRoute(approval rootSubmissionApproval) error {
	return rootSubmissionTransport(approval).validate()
}

// Construction remains network-free and cannot substitute another route.
func newRootSubmissionClient(approval rootSubmissionApproval) (*rpcClient, error) {
	return newOwnedSubmissionClient(rootSubmissionTransport(approval))
}

// This wrapper cannot invoke the EVM method. Every uncertain result continues
// to require the native owner's original canonical receipt reconciliation.
func (self *rootOwnedSubmission) sendOnce(ctx context.Context, raw, expectedHash string) (string, error) {
	return ownedSubmissionPost(ctx, self.chain.client, rootSubmissionTransport(self.config.Approval), "author_submitExtrinsic", raw, expectedHash)
}
