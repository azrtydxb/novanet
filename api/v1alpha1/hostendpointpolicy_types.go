package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HostEndpointPolicySpec defines the desired state of HostEndpointPolicy.
type HostEndpointPolicySpec struct {
	// NodeSelector selects nodes to which this host firewall policy applies.
	// +kubebuilder:validation:Required
	NodeSelector metav1.LabelSelector `json:"nodeSelector"`

	// Ingress defines rules for traffic entering the host.
	// +optional
	Ingress []HostRule `json:"ingress,omitempty"`

	// Egress defines rules for traffic leaving the host.
	// +optional
	Egress []HostRule `json:"egress,omitempty"`
}

// HostRule describes a single host firewall rule.
type HostRule struct {
	// Action is the firewall action.
	// +kubebuilder:validation:Enum=Allow;Deny
	// +kubebuilder:validation:Required
	Action HostRuleAction `json:"action"`

	// Protocol is the network protocol.
	// +optional
	Protocol *corev1.Protocol `json:"protocol,omitempty"`

	// CIDRs are the source (for ingress) or destination (for egress) CIDRs.
	// +optional
	CIDRs []string `json:"cidrs,omitempty"`

	// Ports is a list of port numbers or ranges.
	// +optional
	Ports []HostPort `json:"ports,omitempty"`
}

// HostRuleAction is the firewall action type.
// +kubebuilder:validation:Enum=Allow;Deny
type HostRuleAction string

// HostRuleAction constants for firewall rule actions.
const (
	// HostRuleActionAllow permits traffic matching the rule.
	HostRuleActionAllow HostRuleAction = "Allow"
	// HostRuleActionDeny blocks traffic matching the rule.
	HostRuleActionDeny HostRuleAction = "Deny"
)

// HostPort defines a port or port range for host firewall rules.
type HostPort struct {
	// Port is the port number.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// EndPort is the end of a port range (inclusive).
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	EndPort *int32 `json:"endPort,omitempty"`
}

// HostEndpointPolicyStatus defines the observed state of HostEndpointPolicy.
type HostEndpointPolicyStatus struct {
	// MatchingNodes is the count of nodes matching the selector.
	MatchingNodes int32 `json:"matchingNodes,omitempty"`

	// CompiledRuleCount is the number of eBPF rules generated.
	CompiledRuleCount int32 `json:"compiledRuleCount,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=hep
// +kubebuilder:printcolumn:name="Nodes",type=integer,JSONPath=`.status.matchingNodes`
// +kubebuilder:printcolumn:name="Rules",type=integer,JSONPath=`.status.compiledRuleCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// HostEndpointPolicy defines host-level firewall rules applied to node interfaces.
type HostEndpointPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HostEndpointPolicySpec   `json:"spec,omitempty"`
	Status HostEndpointPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HostEndpointPolicyList contains a list of HostEndpointPolicy.
type HostEndpointPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HostEndpointPolicy `json:"items"`
}

// Register types with the SchemeBuilder.
var _ = func() bool {
	SchemeBuilder.Register(&HostEndpointPolicy{}, &HostEndpointPolicyList{})
	return true
}()
