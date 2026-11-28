package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestText(t *testing.T) {
	for in, want := range map[string]string{
		"arn:aws:eks:us-west-2:000000000000:cluster/spark-on-eks":   "arn:aws:eks:us-west-2:redacted:cluster/spark-on-eks",
		"arn:aws:iam::000000000000:role/nodes":                      "arn:aws:iam::redacted:role/nodes",
		"arn:aws-us-gov:eks:us-gov-west-1:000000000000:cluster/x":   "arn:aws-us-gov:eks:us-gov-west-1:redacted:cluster/x",
		"arn:aws:s3:::bucket-000000000000":                          "arn:aws:s3:::bucket-000000000000",
		"000000000000.dkr.ecr.us-west-2.amazonaws.com/spark:3.5":    "redacted.dkr.ecr.us-west-2.amazonaws.com/spark:3.5",
		"https://ABCDEF0123456789.gr7.us-west-2.eks.amazonaws.com":  "https://redacted.gr7.us-west-2.eks.amazonaws.com",
		"ABCDEF0123456789.yl4.us-east-1.eks.amazonaws.com:443":      "redacted.yl4.us-east-1.eks.amazonaws.com:443",
		"myaks-dns-1a2b3c4d.hcp.eastus.azmk8s.io":                   "redacted.hcp.eastus.azmk8s.io",
		`Get "https://ABC.gr7.us-west-2.eks.amazonaws.com/version"`: `Get "https://redacted.gr7.us-west-2.eks.amazonaws.com/version"`,
		"ABCDEF0123456789.sk1.cn-north-1.eks.amazonaws.com.cn":      "redacted.sk1.cn-north-1.eks.amazonaws.com.cn",
		// not an endpoint, not an account
		"eks.amazonaws.com/role-arn":                            "eks.amazonaws.com/role-arn",
		"metrics.eks.amazonaws.com/v1beta1: the server is busy": "metrics.eks.amazonaws.com/v1beta1: the server is busy",
		"node.metrics.eks.amazonaws.com":                        "node.metrics.eks.amazonaws.com",
		"127.0.0.1:6443":                                        "127.0.0.1:6443",
		"registry.example/api:2.14":                             "registry.example/api:2.14",
		"mem 137438953472":                                      "mem 137438953472",
	} {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

// OpenShift hosts: api.<cluster>.<base>, api-int.<cluster>.<base> and anything under
// apps.<cluster>.<base>, with at least three labels after api, api-int or apps.
func TestTextOpenShift(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.ocp1.example.com:6443":                         "https://api.redacted:6443",
		"api.ocp1.example.com:6443":                                 "api.redacted:6443",
		"api-int.ocp1.example.com":                                  "api-int.redacted",
		"api.ocp1.dev.example.co.uk":                                "api.redacted",
		`Get "https://api.ocp1.example.com:6443/api/v1/namespaces"`: `Get "https://api.redacted:6443/api/v1/namespaces"`,
		"shop-web.apps.ocp1.example.com":                            "shop-web.apps.redacted",
		"console-openshift-console.apps.ocp1.example.com":           "console-openshift-console.apps.redacted",
		"apps.ocp1.example.com":                                     "apps.redacted",
		"*.apps.ocp1.example.com":                                   "*.apps.redacted",
		`{"host":"web-ns-a.apps.ocp1.example.com","path":"/"}`:      `{"host":"web-ns-a.apps.redacted","path":"/"}`,
		`["a.apps.ocp1.example.com","api.ocp1.example.com"]`:        `["a.apps.redacted","api.redacted"]`,
		// API groups, CRD names and resource ids keep their names
		"apps.openshift.io/DeploymentConfig/legacy/web":                                      "apps.openshift.io/DeploymentConfig/legacy/web",
		`deploymentconfigs.apps.openshift.io is forbidden: in API group "apps.openshift.io"`: `deploymentconfigs.apps.openshift.io is forbidden: in API group "apps.openshift.io"`,
		"clonesets.apps.kruise.io":                                                           "clonesets.apps.kruise.io",
		"api.cerbos.dev/Policy/ns-a/p":                                                       "api.cerbos.dev/Policy/ns-a/p",
		"apiserver.openshift.io/v1":                                                          "apiserver.openshift.io/v1",
		"apiextensions.k8s.io":                                                               "apiextensions.k8s.io",
		// api must be the first label; apps must be a whole label
		"registry.api.example.com/app:1": "registry.api.example.com/app:1",
		"my-api.ocp1.example.com":        "my-api.ocp1.example.com",
		"webapps.ocp1.example.com":       "webapps.ocp1.example.com",
		"my-apps.ocp1.example.com":       "my-apps.ocp1.example.com",
		"app.kubernetes.io/name":         "app.kubernetes.io/name",
		// one-label base domains and custom domains are not recognized
		"api.ocp.lab:6443":          "api.ocp.lab:6443",
		"shop.apps.ocp.lab":         "shop.apps.ocp.lab",
		"shop.example.com":          "shop.example.com",
		"api-ocp1-example-com:6443": "api-ocp1-example-com:6443",
	} {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestName(t *testing.T) {
	for in, want := range map[string]string{
		"arn:aws:eks:us-west-2:000000000000:cluster/spark-on-eks": "arn:aws:eks:us-west-2:redacted:cluster/spark-on-eks",
		"000000000000_AdministratorAccess":                        "redacted_AdministratorAccess",
		"admin@spark.us-west-2.eksctl.io":                         "admin@spark.us-west-2.eksctl.io",
		"kuberito-source":                                         "kuberito-source",
		"build-1234567890123":                                     "build-1234567890123",
		// oc login: cluster, user and context names carry the API host with dashes
		"api-ocp1-example-com:6443":                                 "api-redacted:6443",
		"user-a/api-ocp1-example-com:6443":                          "user-a/api-redacted:6443",
		"default/api-ocp1-example-com:6443/user-a":                  "default/api-redacted:6443/user-a",
		"ns-a/api-ocp1-dev-example-co-uk:443/kube:admin":            "ns-a/api-redacted:443/kube:admin",
		`context "ns-a/api-ocp1-example-com:6443/user-a" not found`: `context "ns-a/api-redacted:6443/user-a" not found`,
		"api.ocp1.example.com:6443":                                 "api.redacted:6443",
		// not an oc login host
		"api-gateway":              "api-gateway",
		"api-foo:6443":             "api-foo:6443",
		"my-api-ocp1-example:6443": "my-api-ocp1-example:6443",
	} {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPseudonyms(t *testing.T) {
	got := Pseudonyms([]string{"user-b", "", "user-a", "user-b"})
	if len(got) != 2 || got["user-a"] != "redacted-1" || got["user-b"] != "redacted-2" {
		t.Fatalf("Pseudonyms = %v", got)
	}
	again := Pseudonyms([]string{"user-a", "user-b"})
	for k, v := range got {
		if again[k] != v {
			t.Errorf("%s: %s, then %s", k, v, again[k])
		}
	}
}

func TestCopy(t *testing.T) {
	type doc struct {
		Context string         `json:"context"`
		Facts   map[string]any `json:"facts"`
	}
	in := &doc{Context: "arn:aws:eks:us-west-2:000000000000:cluster/x",
		Facts: map[string]any{"images": []string{"000000000000.dkr.ecr.us-west-2.amazonaws.com/a:1"}, "mem_limits_b": int64(137438953472)}}
	out, err := Copy(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "000000000000") {
		t.Fatalf("copy still holds the account: %s", raw)
	}
	if out.Facts["mem_limits_b"] != float64(137438953472) {
		t.Errorf("numbers changed: %v", out.Facts["mem_limits_b"])
	}
	if !strings.Contains(in.Context, "000000000000") {
		t.Error("the original was modified")
	}
}
