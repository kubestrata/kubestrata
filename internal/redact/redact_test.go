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

func TestName(t *testing.T) {
	for in, want := range map[string]string{
		"arn:aws:eks:us-west-2:000000000000:cluster/spark-on-eks": "arn:aws:eks:us-west-2:redacted:cluster/spark-on-eks",
		"000000000000_AdministratorAccess":                        "redacted_AdministratorAccess",
		"admin@spark.us-west-2.eksctl.io":                         "admin@spark.us-west-2.eksctl.io",
		"kuberito-source":                                         "kuberito-source",
		"build-1234567890123":                                     "build-1234567890123",
	} {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
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
