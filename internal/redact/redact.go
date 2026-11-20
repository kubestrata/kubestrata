// Package redact masks cloud account ids and managed API endpoint hosts in what kubestrata
// prints or renders under --redact. The cache always keeps the real values.
//
//	arn:aws:eks:us-west-2:000000000000:cluster/x        → arn:aws:eks:us-west-2:redacted:cluster/x
//	000000000000.dkr.ecr.us-west-2.amazonaws.com/app    → redacted.dkr.ecr.us-west-2.amazonaws.com/app
//	ABCDEF0123.gr7.us-west-2.eks.amazonaws.com          → redacted.gr7.us-west-2.eks.amazonaws.com
//	myaks-dns-1a2b3c4d.hcp.eastus.azmk8s.io             → redacted.hcp.eastus.azmk8s.io
//
// Names (contexts, kubeconfig clusters and users) also lose any standalone run of 12 digits, the
// shape of an AWS account id outside an ARN.
package redact

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Mask replaces every masked part.
const Mask = "redacted"

var (
	// arn:<partition>:<service>:<region>:<account>:<resource>
	arnAccount = regexp.MustCompile(`(arn:aws[a-z-]*:[a-z0-9-]*:[a-z0-9-]*:)[0-9]{12}:`)
	// <account>.dkr.ecr.<region>.amazonaws.com
	ecrAccount = regexp.MustCompile(`\b[0-9]{12}(\.dkr\.ecr(?:-fips)?\.[a-z0-9-]+\.amazonaws\.com)`)
	// the first label of an EKS or AKS API endpoint is unique to the cluster. Endpoints carry at
	// least two more labels (<id>.gr7.<region>.eks.amazonaws.com, <id>.hcp.<region>.azmk8s.io), so
	// API groups such as metrics.eks.amazonaws.com are left alone.
	endpoint = regexp.MustCompile(`\b[A-Za-z0-9-]+((?:\.[A-Za-z0-9-]+){2,}\.(?:eks\.amazonaws\.com(?:\.cn)?|azmk8s\.io))\b`)
	digits   = regexp.MustCompile(`[0-9]+`)
)

// Text masks account ids in ARNs and ECR registries and the cluster label of managed endpoints.
// The patterns never match JSON syntax or numbers, so Text is safe on a JSON document.
func Text(s string) string {
	s = arnAccount.ReplaceAllString(s, "${1}"+Mask+":")
	s = ecrAccount.ReplaceAllString(s, Mask+"${1}")
	return endpoint.ReplaceAllString(s, Mask+"${1}")
}

// Name is Text plus any run of exactly 12 digits, for context, cluster and user names.
func Name(s string) string {
	return digits.ReplaceAllStringFunc(Text(s), func(d string) string {
		if len(d) == 12 {
			return Mask
		}
		return d
	})
}

// Copy returns a deep copy of v with Text applied to every string: v is written as JSON, masked
// and read back into a new value. Numbers read back as float64 inside map[string]any.
func Copy[T any](v *T) (*T, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("redact: %w", err)
	}
	var out T
	if err := json.Unmarshal([]byte(Text(string(raw))), &out); err != nil {
		return nil, fmt.Errorf("redact: %w", err)
	}
	return &out, nil
}
