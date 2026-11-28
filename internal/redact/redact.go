// Package redact masks cloud account ids, managed API endpoint hosts and OpenShift cluster
// domains in what kubestrata prints or renders under --redact. The cache always keeps the real
// values.
//
//	arn:aws:eks:us-west-2:000000000000:cluster/x        → arn:aws:eks:us-west-2:redacted:cluster/x
//	000000000000.dkr.ecr.us-west-2.amazonaws.com/app    → redacted.dkr.ecr.us-west-2.amazonaws.com/app
//	ABCDEF0123.gr7.us-west-2.eks.amazonaws.com          → redacted.gr7.us-west-2.eks.amazonaws.com
//	myaks-dns-1a2b3c4d.hcp.eastus.azmk8s.io             → redacted.hcp.eastus.azmk8s.io
//	api.ocp1.example.com:6443                           → api.redacted:6443
//	shop-web.apps.ocp1.example.com                      → shop-web.apps.redacted
//
// Names (contexts, kubeconfig clusters and users) also lose any standalone run of 12 digits, the
// shape of an AWS account id outside an ARN, and the dashed API host that `oc login` writes into
// them (default/api-ocp1-example-com:6443/user-a → default/api-redacted:6443/user-a).
package redact

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
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
	// OpenShift serves the API at api.<cluster>.<base> (api-int.<cluster>.<base> inside the cluster)
	// and Routes under apps.<cluster>.<base>; <cluster>.<base> names the cluster and the company and
	// is masked whole. api and api-int must be the first label of the host; apps may follow other
	// labels (shop-web.apps.<cluster>.<base>). At least three labels must follow, so API groups and
	// CRD names (apps.openshift.io, clonesets.apps.kruise.io, api.cerbos.dev) are left alone; a
	// cluster under a one-label base domain (api.ocp.lab) is not masked.
	openshiftAPI  = regexp.MustCompile(`(^|[^A-Za-z0-9.-])(api(?:-int)?\.)(?:[A-Za-z0-9-]+\.){2,}[A-Za-z0-9-]+`)
	openshiftApps = regexp.MustCompile(`((?:^|[^A-Za-z0-9-])apps\.)(?:[A-Za-z0-9-]+\.){2,}[A-Za-z0-9-]+`)
	// oc login names the cluster <api host with dashes>:<port>, the user <user>/<cluster> and the
	// context <namespace>/<cluster>/<user>. The dashed host has at least two parts after api.
	ocLoginHost = regexp.MustCompile(`(^|[^A-Za-z0-9.-])api-[A-Za-z0-9]+-[A-Za-z0-9-]+(:[0-9]+)`)
	digits      = regexp.MustCompile(`[0-9]+`)
)

// Text masks account ids in ARNs and ECR registries, the cluster label of managed endpoints and
// the cluster domain of OpenShift API and Route hosts. The patterns never match JSON syntax or
// numbers, so Text is safe on a JSON document.
func Text(s string) string {
	s = arnAccount.ReplaceAllString(s, "${1}"+Mask+":")
	s = ecrAccount.ReplaceAllString(s, Mask+"${1}")
	s = endpoint.ReplaceAllString(s, Mask+"${1}")
	s = openshiftAPI.ReplaceAllString(s, "${1}${2}"+Mask)
	return openshiftApps.ReplaceAllString(s, "${1}"+Mask)
}

// Name is Text plus the dashed API host of oc login names and any run of exactly 12 digits, for
// context, cluster and user names and for messages that quote them.
func Name(s string) string {
	s = ocLoginHost.ReplaceAllString(Text(s), "${1}api-"+Mask+"${2}")
	return digits.ReplaceAllStringFunc(s, func(d string) string {
		if len(d) == 12 {
			return Mask
		}
		return d
	})
}

// Pseudonyms maps each distinct non-empty value to redacted-1, redacted-2, ... in sorted order.
// The same set of values gives the same masks, so a value keeps its mask across every document of
// one run while different values stay apart (team names taken from user names).
func Pseudonyms(values []string) map[string]string {
	seen := map[string]bool{}
	var uniq []string
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			uniq = append(uniq, v)
		}
	}
	sort.Strings(uniq)
	out := make(map[string]string, len(uniq))
	for i, v := range uniq {
		out[v] = Mask + "-" + strconv.Itoa(i+1)
	}
	return out
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
