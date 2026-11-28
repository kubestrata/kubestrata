package collect

// mode says what the collector does with a kind.
type mode int

const (
	modeShow       mode = iota // inventory item
	modeSupport                // collected to resolve owners and aggregate; shown only when orphan
	modeNodes                  // summarized into the cluster, never an item
	modeNamespaces             // sectors of the view
	modeSecrets                // inventory item, metadata only through ListSecretsMeta
)

// Categories of the view filters.
const (
	CatWorkload = "workload"
	CatNetwork  = "network"
	CatConfig   = "config"
	CatStorage  = "storage"
	CatPolicy   = "policy"
	CatCRD      = "crd"
)

type kindSpec struct {
	Group, Resource, Kind, Cat string
	Mode                       mode
}

// builtin is the default inventory. OpenShift and Gateway API kinds are listed here so they get
// a real category; they are collected only when the cluster serves them.
var builtin = []kindSpec{
	{"", "namespaces", "Namespace", "", modeNamespaces},
	{"", "nodes", "Node", "", modeNodes},
	{"", "pods", "Pod", CatWorkload, modeSupport},
	{"apps", "replicasets", "ReplicaSet", CatWorkload, modeSupport},
	{"", "replicationcontrollers", "ReplicationController", CatWorkload, modeSupport},
	{"batch", "jobs", "Job", CatWorkload, modeSupport},
	{"apps", "deployments", "Deployment", CatWorkload, modeShow},
	{"apps", "statefulsets", "StatefulSet", CatWorkload, modeShow},
	{"apps", "daemonsets", "DaemonSet", CatWorkload, modeShow},
	{"batch", "cronjobs", "CronJob", CatWorkload, modeShow},
	{"apps.openshift.io", "deploymentconfigs", "DeploymentConfig", CatWorkload, modeShow},
	{"", "services", "Service", CatNetwork, modeShow},
	{"networking.k8s.io", "ingresses", "Ingress", CatNetwork, modeShow},
	{"networking.k8s.io", "networkpolicies", "NetworkPolicy", CatNetwork, modeShow},
	{"route.openshift.io", "routes", "Route", CatNetwork, modeShow},
	{"gateway.networking.k8s.io", "gateways", "Gateway", CatNetwork, modeShow},
	{"gateway.networking.k8s.io", "httproutes", "HTTPRoute", CatNetwork, modeShow},
	{"", "configmaps", "ConfigMap", CatConfig, modeShow},
	{"", "secrets", "Secret", CatConfig, modeSecrets},
	{"", "serviceaccounts", "ServiceAccount", CatConfig, modeShow},
	{"", "persistentvolumeclaims", "PersistentVolumeClaim", CatStorage, modeShow},
	{"", "persistentvolumes", "PersistentVolume", CatStorage, modeShow},
	{"storage.k8s.io", "storageclasses", "StorageClass", CatStorage, modeShow},
	{"snapshot.storage.k8s.io", "volumesnapshots", "VolumeSnapshot", CatStorage, modeShow},
	{"autoscaling", "horizontalpodautoscalers", "HorizontalPodAutoscaler", CatPolicy, modeShow},
	{"policy", "poddisruptionbudgets", "PodDisruptionBudget", CatPolicy, modeShow},
	{"", "resourcequotas", "ResourceQuota", CatPolicy, modeShow},
	{"", "limitranges", "LimitRange", CatPolicy, modeShow},
	{"rbac.authorization.k8s.io", "roles", "Role", CatPolicy, modeShow},
	{"rbac.authorization.k8s.io", "rolebindings", "RoleBinding", CatPolicy, modeShow},
}

func specFor(group, resource string) (kindSpec, bool) {
	for _, k := range builtin {
		if k.Group == group && k.Resource == resource {
			return k, true
		}
	}
	return kindSpec{}, false
}

// platformGroups serve the configuration and state of OpenShift's own cluster operators, which the
// cluster version operator installs and the target platform replaces: machine configuration and
// the Machine API, cluster-wide config.openshift.io settings, the operator configs of
// operator.openshift.io and of the per-operator groups, API request counts, console
// customizations. Their custom resources get Platform.
var platformGroups = map[string]bool{
	"apiserver.openshift.io":              true, // APIRequestCount
	"machineconfiguration.openshift.io":   true, // MachineConfig, MachineConfigPool, KubeletConfig, ...
	"config.openshift.io":                 true, // ClusterVersion, ClusterOperator, Infrastructure, ...
	"operator.openshift.io":               true, // IngressController, Network, Etcd, KubeAPIServer, ...
	"controlplane.operator.openshift.io":  true, // PodNetworkConnectivityCheck
	"imageregistry.operator.openshift.io": true,
	"samples.operator.openshift.io":       true,
	"ingress.operator.openshift.io":       true, // DNSRecord
	"network.operator.openshift.io":       true,
	"machine.openshift.io":                true, // Machine, MachineSet, MachineHealthCheck
	"autoscaling.openshift.io":            true, // ClusterAutoscaler, MachineAutoscaler
	"tuned.openshift.io":                  true,
	"cloudcredential.openshift.io":        true, // CredentialsRequest
	"console.openshift.io":                true, // ConsoleLink, ConsolePlugin, ...
}

// noiseConfigMaps are created by the control plane in every namespace.
var noiseConfigMaps = map[string]bool{"kube-root-ca.crt": true, "openshift-service-ca.crt": true}

// DefaultSystemNS matches namespaces hidden by default in the view: the Kubernetes ones by exact
// name (kube-prometheus-stack is an application), OpenShift and the cloud providers by prefix.
const DefaultSystemNS = `^(kube-system|kube-public|kube-node-lease|local-path-storage|openshift(-.*)?|amazon-.*|aws-.*|gke-.*|azure-.*)$`

// DefaultTeamKeys are looked up, in order, in resource labels and then namespace labels and
// annotations. openshift.io/requester (set on OpenShift projects) is the last fallback.
var DefaultTeamKeys = []string{"team", "owner", "app.kubernetes.io/part-of"}

// TeamFromRequester is the namespace annotation OpenShift sets to the user who requested the
// project. A team taken from it is a user name: --redact masks it.
const TeamFromRequester = "openshift.io/requester"

// TeamFromNamespace is the team_from of a team taken from the namespace name (--team-from-ns).
const TeamFromNamespace = "namespace-name"

// DefaultRequesterIgnore are requesters that name no team: the OpenShift bootstrap admin, the
// system users and service accounts, and a plain admin. A trailing * matches a prefix.
var DefaultRequesterIgnore = []string{"kube:admin", "system:*", "admin"}
