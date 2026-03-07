package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EgressGatewayPolicySpec defines the desired state of EgressGatewayPolicy.
type EgressGatewayPolicySpec struct {
	// PodSelector selects pods whose egress traffic should be routed
	// through the gateway nodes.
	// +kubebuilder:validation:Required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// DestinationCIDRs are the external CIDRs that this policy applies to.
	// If empty, all external traffic from selected pods is routed through the gateway.
	// +optional
	DestinationCIDRs []string `json:"destinationCIDRs,omitempty"`

	// ExcludedCIDRs are CIDRs excluded from gateway routing (e.g., cluster CIDR).
	// +optional
	ExcludedCIDRs []string `json:"excludedCIDRs,omitempty"`

	// GatewaySelector selects nodes that act as egress gateways.
	// +kubebuilder:validation:Required
	GatewaySelector metav1.LabelSelector `json:"gatewaySelector"`

	// EgressIP is the SNAT IP to use for traffic leaving through the gateway.
	// If empty, the gateway node's primary IP is used.
	// +optional
	EgressIP string `json:"egressIP,omitempty"`
}

// EgressGatewayPolicyStatus defines the observed state of EgressGatewayPolicy.
type EgressGatewayPolicyStatus struct {
	// ActiveGatewayNode is the node currently handling egress traffic.
	ActiveGatewayNode string `json:"activeGatewayNode,omitempty"`

	// EgressIP is the IP being used for SNAT.
	EgressIP string `json:"egressIP,omitempty"`

	// GatewayNodes is the list of eligible gateway nodes.
	// +optional
	GatewayNodes []string `json:"gatewayNodes,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=egp
// +kubebuilder:printcolumn:name="Gateway",type=string,JSONPath=`.status.activeGatewayNode`
// +kubebuilder:printcolumn:name="EgressIP",type=string,JSONPath=`.status.egressIP`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// EgressGatewayPolicy defines an egress gateway routing policy.
type EgressGatewayPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EgressGatewayPolicySpec   `json:"spec,omitempty"`
	Status EgressGatewayPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EgressGatewayPolicyList contains a list of EgressGatewayPolicy.
type EgressGatewayPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EgressGatewayPolicy `json:"items"`
}

// Register types with the SchemeBuilder.
var _ = func() bool {
	SchemeBuilder.Register(&EgressGatewayPolicy{}, &EgressGatewayPolicyList{})
	return true
}()
