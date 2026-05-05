package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SatellitePhase represents the lifecycle phase of a Satellite.
// +kubebuilder:validation:Enum=Pending;Registered;Failed;Terminating
type SatellitePhase string

const (
	// SatellitePhasePending means the satellite has not yet been registered.
	SatellitePhasePending SatellitePhase = "Pending"
	// SatellitePhaseRegistered means the satellite is registered with Ground Control.
	SatellitePhaseRegistered SatellitePhase = "Registered"
	// SatellitePhaseFailed means registration or reconciliation failed.
	SatellitePhaseFailed SatellitePhase = "Failed"
	// SatellitePhaseTerminating means the satellite is being deleted.
	SatellitePhaseTerminating SatellitePhase = "Terminating"
)

// SatelliteSpec defines the desired state of a Satellite.
type SatelliteSpec struct {
	// Group is the name of the group to assign this satellite to.
	// +optional
	Group string `json:"group,omitempty"`

	// ConfigName is the name of the Ground Control config to assign.
	// +optional
	ConfigName string `json:"configName,omitempty"`

	// GroundControlURL is the URL of the Ground Control server.
	// Defaults to the operator's configured default.
	// +optional
	GroundControlURL string `json:"groundControlURL,omitempty"`

	// Token is an optional pre-existing ZTR token. If empty, the operator
	// will call the register endpoint to obtain one.
	// +optional
	Token string `json:"token,omitempty"`
}

// SatelliteStatus defines the observed state of a Satellite.
type SatelliteStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase SatellitePhase `json:"phase,omitempty"`

	// Registered indicates whether the satellite is registered with Ground Control.
	// +optional
	Registered bool `json:"registered"`

	// Token is the ZTR token obtained from Ground Control, stored as a secret reference.
	// +optional
	TokenRef string `json:"tokenRef,omitempty"`

	// Message is a human-readable message indicating details about the current phase.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions represent the latest available observations of the satellite's state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// Satellite is the Schema for the satellites API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Registered",type="boolean",JSONPath=".status.registered"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Namespaced,shortName=sat
type Satellite struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SatelliteSpec   `json:"spec,omitempty"`
	Status SatelliteStatus `json:"status,omitempty"`
}

// SatelliteList contains a list of Satellite.
// +kubebuilder:object:root=true
type SatelliteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Satellite `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Satellite{}, &SatelliteList{})
}
