package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PolicyType describes the direction of traffic a policy applies to.
// +kubebuilder:validation:Enum=Ingress;Egress
type PolicyType string

// PolicyType constants for network policy directions.
const (
	// PolicyTypeIngress indicates the policy applies to incoming traffic.
	PolicyTypeIngress PolicyType = "Ingress"
	// PolicyTypeEgress indicates the policy applies to outgoing traffic.
	PolicyTypeEgress PolicyType = "Egress"
)

// NovaNetworkPolicySpec defines the desired state of NovaNetworkPolicy.
type NovaNetworkPolicySpec struct {
	// PodSelector selects the pods to which this policy applies.
	// +kubebuilder:validation:Required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// PolicyTypes specifies which rule types (Ingress, Egress) this policy includes.
	// +optional
	PolicyTypes []PolicyType `json:"policyTypes,omitempty"`

	// Ingress specifies ingress rules. Each rule allows traffic from sources
	// matching From on the specified Ports.
	// +optional
	Ingress []NovaNetworkPolicyIngressRule `json:"ingress,omitempty"`

	// Egress specifies egress rules. Each rule allows traffic to destinations
	// matching To on the specified Ports.
	// +optional
	Egress []NovaNetworkPolicyEgressRule `json:"egress,omitempty"`
}

// NovaNetworkPolicyIngressRule describes an ingress rule.
type NovaNetworkPolicyIngressRule struct {
	// Ports specifies the ports and protocols allowed.
	// +optional
	Ports []NovaNetworkPolicyPort `json:"ports,omitempty"`

	// From specifies sources allowed to reach the selected pods.
	// +optional
	From []NovaNetworkPolicyPeer `json:"from,omitempty"`
}

// NovaNetworkPolicyEgressRule describes an egress rule.
type NovaNetworkPolicyEgressRule struct {
	// Ports specifies the ports and protocols allowed.
	// +optional
	Ports []NovaNetworkPolicyPort `json:"ports,omitempty"`

	// To specifies destinations the selected pods are allowed to reach.
	// +optional
	To []NovaNetworkPolicyPeer `json:"to,omitempty"`
}

// NovaNetworkPolicyPort describes a port with optional range support.
type NovaNetworkPolicyPort struct {
	// Protocol is the protocol (TCP, UDP, SCTP). Defaults to TCP.
	// +optional
	Protocol *string `json:"protocol,omitempty"`

	// Port is the port number.
	// +optional
	Port *int32 `json:"port,omitempty"`

	// EndPort defines the end of a port range (inclusive). If set, Port must
	// also be set and EndPort must be >= Port.
	// +optional
	EndPort *int32 `json:"endPort,omitempty"`
}

// NovaNetworkPolicyPeer describes a peer for policy rules.
type NovaNetworkPolicyPeer struct {
	// PodSelector selects pods in the policy's namespace (or in namespaces
	// selected by NamespaceSelector).
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`

	// NamespaceSelector selects namespaces. Combined with PodSelector, this
	// selects pods in the matching namespaces.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// IPBlock specifies a CIDR range and optional exceptions.
	// +optional
	IPBlock *NovaIPBlock `json:"ipBlock,omitempty"`

	// FQDN specifies a fully qualified domain name. The controller resolves
	// it to IP addresses and generates CIDR rules for each resolved address.
	// +optional
	FQDN *string `json:"fqdn,omitempty"`

	// ServiceAccount selects pods by their service account identity.
	// +optional
	ServiceAccount *ServiceAccountPeer `json:"serviceAccount,omitempty"`
}

// NovaIPBlock describes a CIDR with optional exceptions.
type NovaIPBlock struct {
	// CIDR is the IP block (e.g. "10.0.0.0/8").
	// +kubebuilder:validation:Required
	CIDR string `json:"cidr"`

	// Except are CIDRs that should not be included within the CIDR.
	// +optional
	Except []string `json:"except,omitempty"`
}

// ServiceAccountPeer selects pods running under a specific service account.
type ServiceAccountPeer struct {
	// Name is the service account name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the service account. If empty, the policy's
	// namespace is used.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// NovaNetworkPolicyStatus defines the observed state of NovaNetworkPolicy.
type NovaNetworkPolicyStatus struct {
	// Conditions represent the latest available observations of the policy's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RuleCount is the number of compiled rules generated from this policy.
	// +optional
	RuleCount int32 `json:"ruleCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=nnp

// NovaNetworkPolicy is the Schema for the novanetworkpolicies API.
// It extends Kubernetes NetworkPolicy with port ranges, FQDN-based peers,
// and service account selectors.
type NovaNetworkPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NovaNetworkPolicySpec   `json:"spec,omitempty"`
	Status NovaNetworkPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NovaNetworkPolicyList contains a list of NovaNetworkPolicy.
type NovaNetworkPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NovaNetworkPolicy `json:"items"`
}

// Register types with the SchemeBuilder.
var _ = func() bool {
	SchemeBuilder.Register(&NovaNetworkPolicy{}, &NovaNetworkPolicyList{})
	return true
}()
