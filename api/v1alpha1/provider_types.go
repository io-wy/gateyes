package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProviderSpec defines the desired state of a Provider.
type ProviderSpec struct {
	// Type is the provider protocol type: openai, anthropic, grpc, azure.
	// +kubebuilder:validation:Enum=openai;anthropic;grpc;azure
	Type string `json:"type"`

	// Vendor is the vendor name for display and routing.
	Vendor string `json:"vendor,omitempty"`

	// BaseURL is the REST API base URL.
	BaseURL string `json:"baseURL,omitempty"`

	// GRPCTarget is the gRPC target address (for grpc type).
	GRPCTarget string `json:"grpcTarget,omitempty"`

	// GRPCUseTLS enables TLS for gRPC connections.
	GRPCUseTLS bool `json:"grpcUseTLS,omitempty"`

	// GRPCAuthority is the gRPC authority header override.
	GRPCAuthority string `json:"grpcAuthority,omitempty"`

	// APIKey is the authentication key for the provider endpoint.
	// This field is sensitive and should be stored in a Secret reference in production.
	APIKey string `json:"apiKey,omitempty"`

	// Model is the default model name for this provider.
	Model string `json:"model,omitempty"`

	// Weight controls routing priority when multiple providers are available.
	Weight int `json:"weight,omitempty"`

	// PriceInput is the input token price per token.
	PriceInput float64 `json:"priceInput,omitempty"`

	// PriceOutput is the output token price per token.
	PriceOutput float64 `json:"priceOutput,omitempty"`

	// MaxTokens is the maximum tokens allowed per request.
	MaxTokens int `json:"maxTokens,omitempty"`

	// Timeout is the request timeout in seconds.
	Timeout int `json:"timeout,omitempty"`

	// Enabled controls whether this provider is active.
	Enabled bool `json:"enabled,omitempty"`

	// Headers are additional HTTP headers sent to the provider.
	Headers map[string]string `json:"headers,omitempty"`

	// ExtraBody is additional JSON merged into the request body.
	ExtraBody map[string]any `json:"extraBody,omitempty"`

	// Endpoint is the surface endpoint: "chat" or "responses".
	// +kubebuilder:validation:Enum=chat;responses
	Endpoint string `json:"endpoint,omitempty"`
}

// ProviderStatus defines the observed state of a Provider.
type ProviderStatus struct {
	// Ready indicates whether the provider has been successfully reconciled.
	Ready bool `json:"ready,omitempty"`

	// LastSyncTime is the timestamp of the last successful reconcile.
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions represent the latest available observations of the provider state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=gpv

// Provider is the Schema for the providers API.
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider.
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Provider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
}
