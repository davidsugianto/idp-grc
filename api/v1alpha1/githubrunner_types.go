/*
Copyright 2026 David Sugianto.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RunnerPhase describes the lifecycle phase of a GitHubRunner.
type RunnerPhase string

const (
	RunnerPhasePending      RunnerPhase = "Pending"
	RunnerPhaseRegistering  RunnerPhase = "Registering"
	RunnerPhaseRunning      RunnerPhase = "Running"
	RunnerPhaseFailed       RunnerPhase = "Failed"
	RunnerPhaseTerminating  RunnerPhase = "Terminating"
)

// SecretRef references a Kubernetes Secret by name.
type SecretRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// GitHubRunnerSpec defines the desired state of GitHubRunner.
type GitHubRunnerSpec struct {
	// GitHubURL is the URL of the GitHub repository or organization this runner registers with.
	// Repository: https://github.com/<owner>/<repo>
	// Organization: https://github.com/<org>
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^https://github\.com/.+`
	GitHubURL string `json:"githubURL"`

	// CredentialsSecretRef references the Kubernetes Secret containing the GitHub PAT
	// under the key "token".
	CredentialsSecretRef SecretRef `json:"credentialsSecretRef"`

	// RunnerName is the name shown in GitHub's runner list.
	// Defaults to the CR name if unset.
	// +optional
	RunnerName string `json:"runnerName,omitempty"`

	// RunnerLabels is a list of labels to attach to the runner in GitHub.
	// +optional
	RunnerLabels []string `json:"runnerLabels,omitempty"`

	// RunnerGroup is the name of the GitHub runner group to assign the runner to.
	// +optional
	RunnerGroup string `json:"runnerGroup,omitempty"`

	// Image is the runner container image.
	// Defaults to ghcr.io/actions/runner:latest.
	// +optional
	// +kubebuilder:default="ghcr.io/actions/runner:latest"
	Image string `json:"image,omitempty"`

	// Resources sets CPU/memory requests and limits for the runner Pod.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// GitHubRunnerStatus defines the observed state of GitHubRunner.
type GitHubRunnerStatus struct {
	// Phase is the current lifecycle phase of the runner.
	// +optional
	Phase RunnerPhase `json:"phase,omitempty"`

	// RunnerID is the numeric ID assigned to this runner by GitHub.
	// +optional
	RunnerID int64 `json:"runnerID,omitempty"`

	// RunnerName is the name registered with GitHub.
	// +optional
	RunnerName string `json:"runnerName,omitempty"`

	// PodName is the name of the runner Pod managed by this CR.
	// +optional
	PodName string `json:"podName,omitempty"`

	// RegisteredAt is the timestamp when the runner was last successfully registered.
	// +optional
	RegisteredAt *metav1.Time `json:"registeredAt,omitempty"`

	// Conditions holds standard Kubernetes conditions for the runner.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ghr,scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Runner-ID",type=integer,JSONPath=`.status.runnerID`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GitHubRunner is the Schema for the githubrunners API.
type GitHubRunner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitHubRunnerSpec   `json:"spec,omitempty"`
	Status GitHubRunnerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GitHubRunnerList contains a list of GitHubRunner.
type GitHubRunnerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitHubRunner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitHubRunner{}, &GitHubRunnerList{})
}
