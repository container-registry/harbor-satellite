package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// SatelliteSpec defines the desired state of a Satellite.
type SatelliteSpec struct {
	// GroundControlURL is the base URL of the Ground Control API.
	GroundControlURL string `json:"groundControlURL"`
	// CredentialsSecret names the Kubernetes Secret holding Ground Control credentials.
	CredentialsSecret string `json:"credentialsSecret"`
	// ConfigName is the Ground Control config to assign to this satellite.
	ConfigName string `json:"configName"`
	// Groups lists the image groups the satellite should replicate.
	Groups []string `json:"groups,omitempty"`
}

// SatelliteStatus defines the observed state of a Satellite.
type SatelliteStatus struct {
	// RegistrationState reflects the satellite's registration with Ground Control.
	RegistrationState string `json:"registrationState,omitempty"`
	// LastHeartbeat is the time of the most recent successful sync.
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`
	// Conditions reports high-level lifecycle conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Satellite is the Schema for the satellites API.
type Satellite struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SatelliteSpec   `json:"spec,omitempty"`
	Status SatelliteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SatelliteList contains a list of Satellite.
type SatelliteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Satellite `json:"items"`
}
