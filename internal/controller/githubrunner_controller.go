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

package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	idpv1alpha1 "github.com/davidsugianto/idp-grc/api/v1alpha1"
)

const (
	finalizerName = "idp.grc.io/runner-cleanup"

	conditionRegistered = "Registered"
	conditionPodReady   = "PodReady"
	conditionDegraded   = "Degraded"

	// Backoff durations for GitHub API errors.
	backoffInitial = 10 * time.Second
	backoffMax     = 5 * time.Minute
)

// +kubebuilder:rbac:groups=idp.grc.io,resources=githubrunners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=idp.grc.io,resources=githubrunners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=idp.grc.io,resources=githubrunners/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// GitHubRunnerReconciler reconciles GitHubRunner objects.
type GitHubRunnerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	GitHub   *GitHubClient
}

// SetupWithManager registers the reconciler and sets up a Pod watch so that
// Pod readiness changes trigger a reconcile of the owning GitHubRunner (US2 SC3).
func (r *GitHubRunnerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&idpv1alpha1.GitHubRunner{}).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(),
				mgr.GetRESTMapper(),
				&idpv1alpha1.GitHubRunner{},
				handler.OnlyControllerOwner(),
			),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

// Reconcile is the main reconciliation loop.
func (r *GitHubRunnerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := slog.With("githubrunner", req.NamespacedName)

	runner := &idpv1alpha1.GitHubRunner{}
	if err := r.Get(ctx, req.NamespacedName, runner); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// --- Finalizer: deletion path ---
	if !runner.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, log, runner)
	}

	// --- Finalizer: add ---
	if !controllerutil.ContainsFinalizer(runner, finalizerName) {
		controllerutil.AddFinalizer(runner, finalizerName)
		if err := r.Update(ctx, runner); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// --- Normal reconcile ---
	return r.reconcileRunner(ctx, log, runner)
}

// handleDeletion de-registers the runner from GitHub, deletes the Pod, and removes the finalizer.
func (r *GitHubRunnerReconciler) handleDeletion(ctx context.Context, log *slog.Logger, runner *idpv1alpha1.GitHubRunner) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(runner, finalizerName) {
		return ctrl.Result{}, nil
	}

	log.Info("runner CR is being deleted, running cleanup")

	// Fetch PAT (best-effort — if secret is gone, skip API call).
	pat, _ := r.fetchPAT(ctx, runner)

	if pat != "" && runner.Status.RunnerID != 0 {
		if err := r.GitHub.DeleteRunner(ctx, runner.Spec.GitHubURL, runner.Status.RunnerID, pat); err != nil {
			log.Error("failed to de-register runner from GitHub", "error", err)
			r.Recorder.Eventf(runner, corev1.EventTypeWarning, "DeregistrationFailed", "Failed to de-register runner: %v", err)
			// Don't block deletion on API failure — log and continue.
		} else {
			log.Info("runner de-registered from GitHub", "runnerID", runner.Status.RunnerID)
			r.Recorder.Event(runner, corev1.EventTypeNormal, "Deregistered", "Runner de-registered from GitHub")
		}
	}

	// Delete the runner Pod if it still exists.
	if runner.Status.PodName != "" {
		pod := &corev1.Pod{}
		err := r.Get(ctx, types.NamespacedName{Name: runner.Status.PodName, Namespace: runner.Namespace}, pod)
		if err == nil {
			if delErr := r.Delete(ctx, pod); delErr != nil && !apierrors.IsNotFound(delErr) {
				return ctrl.Result{}, fmt.Errorf("deleting runner pod: %w", delErr)
			}
		}
	}

	controllerutil.RemoveFinalizer(runner, finalizerName)
	if err := r.Update(ctx, runner); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// reconcileRunner handles the normal (non-deletion) reconcile path.
func (r *GitHubRunnerReconciler) reconcileRunner(ctx context.Context, log *slog.Logger, runner *idpv1alpha1.GitHubRunner) (ctrl.Result, error) {
	// Fetch PAT from Secret.
	pat, err := r.fetchPAT(ctx, runner)
	if err != nil {
		log.Error("failed to fetch PAT", "error", err)
		r.Recorder.Eventf(runner, corev1.EventTypeWarning, "CredentialError", "%v", err)
		if apierrors.IsNotFound(err) {
			if updateErr := r.setFailedStatus(ctx, runner, "CredentialsNotFound",
				fmt.Sprintf("Secret %q not found in namespace %q", runner.Spec.CredentialsSecretRef.Name, runner.Namespace)); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		if updateErr := r.setFailedStatus(ctx, runner, "CredentialError", err.Error()); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: backoffInitial}, nil
	}

	// --- Idempotent guard: if Running and Pod exists, check Pod readiness ---
	if runner.Status.Phase == idpv1alpha1.RunnerPhaseRunning && runner.Status.PodName != "" {
		pod := &corev1.Pod{}
		err := r.Get(ctx, types.NamespacedName{Name: runner.Status.PodName, Namespace: runner.Namespace}, pod)
		if err == nil {
			r.updatePodReadyCondition(runner, pod)
			if err2 := r.statusUpdate(ctx, runner); err2 != nil {
				return ctrl.Result{}, err2
			}
			// Pod is healthy — nothing to do.
			if isPodReady(pod) {
				return ctrl.Result{}, nil
			}
			// Pod exists but not ready — fall through to re-register below.
			log.Info("pod not ready, will re-register if needed", "pod", pod.Name)
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("getting pod: %w", err)
		}
		// Pod is gone — re-register.
	}

	// --- Phase: Pending → Registering ---
	if runner.Status.Phase == "" || runner.Status.Phase == idpv1alpha1.RunnerPhasePending || runner.Status.RunnerID == 0 {
		return r.startRegistration(ctx, log, runner, pat)
	}

	// --- Phase: Registering → Running ---
	if runner.Status.Phase == idpv1alpha1.RunnerPhaseRegistering {
		return r.pollForRunner(ctx, log, runner, pat)
	}

	// --- Phase: Running but Pod gone (re-register) ---
	if runner.Status.Phase == idpv1alpha1.RunnerPhaseRunning && runner.Status.RunnerID == 0 {
		return r.startRegistration(ctx, log, runner, pat)
	}

	return ctrl.Result{}, nil
}

// startRegistration fetches a registration token, creates the runner Pod, and sets phase=Registering.
func (r *GitHubRunnerReconciler) startRegistration(ctx context.Context, log *slog.Logger, runner *idpv1alpha1.GitHubRunner, pat string) (ctrl.Result, error) {
	log.Info("starting runner registration")

	runner.Status.Phase = idpv1alpha1.RunnerPhasePending
	if err := r.statusUpdate(ctx, runner); err != nil {
		return ctrl.Result{}, err
	}

	token, err := r.GitHub.GetRegistrationToken(ctx, runner.Spec.GitHubURL, pat)
	if err != nil {
		return r.handleGitHubAPIError(ctx, log, runner, "TokenAcquisitionFailed", err)
	}

	// Determine runner name.
	runnerName := runner.Spec.RunnerName
	if runnerName == "" {
		runnerName = runner.Name
	}

	// Create the runner Pod if it doesn't already exist.
	podName := runner.Name + "-runner"
	existingPod := &corev1.Pod{}
	err = r.Get(ctx, types.NamespacedName{Name: podName, Namespace: runner.Namespace}, existingPod)
	if apierrors.IsNotFound(err) {
		pod := r.buildRunnerPod(runner, podName, runnerName, token)
		if err := r.Create(ctx, pod); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating runner pod: %w", err)
		}
		log.Info("runner pod created", "pod", podName)
	} else if err != nil {
		return ctrl.Result{}, fmt.Errorf("checking for existing pod: %w", err)
	}

	runner.Status.Phase = idpv1alpha1.RunnerPhaseRegistering
	runner.Status.PodName = podName
	runner.Status.RunnerName = runnerName
	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionRegistered,
		Status:             metav1.ConditionFalse,
		Reason:             "Registering",
		Message:            "Runner Pod created, waiting for registration with GitHub",
		LastTransitionTime: metav1.Now(),
	})
	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AsExpected",
		Message:            "",
		LastTransitionTime: metav1.Now(),
	})

	if err := r.statusUpdate(ctx, runner); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue quickly to poll for runner ID.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// pollForRunner checks if the runner has appeared in GitHub's list and transitions to Running.
func (r *GitHubRunnerReconciler) pollForRunner(ctx context.Context, log *slog.Logger, runner *idpv1alpha1.GitHubRunner, pat string) (ctrl.Result, error) {
	runnerName := runner.Status.RunnerName
	if runnerName == "" {
		runnerName = runner.Name
	}

	runnerID, err := r.GitHub.FindRunnerID(ctx, runner.Spec.GitHubURL, runnerName, pat)
	if err != nil {
		return r.handleGitHubAPIError(ctx, log, runner, "RunnerLookupFailed", err)
	}

	if runnerID == 0 {
		// Not registered yet — poll again soon.
		log.Info("runner not yet visible in GitHub, will retry")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	log.Info("runner registered with GitHub", "runnerID", runnerID)

	now := metav1.Now()
	runner.Status.Phase = idpv1alpha1.RunnerPhaseRunning
	runner.Status.RunnerID = runnerID
	runner.Status.RegisteredAt = &now
	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionRegistered,
		Status:             metav1.ConditionTrue,
		Reason:             "RegistrationSucceeded",
		Message:            fmt.Sprintf("Runner registered with GitHub as ID %d", runnerID),
		LastTransitionTime: metav1.Now(),
	})
	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AsExpected",
		Message:            "",
		LastTransitionTime: metav1.Now(),
	})

	// Check pod readiness while we're here.
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: runner.Status.PodName, Namespace: runner.Namespace}, pod); err == nil {
		r.updatePodReadyCondition(runner, pod)
	}

	if err := r.statusUpdate(ctx, runner); err != nil {
		return ctrl.Result{}, err
	}

	r.Recorder.Eventf(runner, corev1.EventTypeNormal, "Registered",
		"Runner registered with GitHub as ID %d", runnerID)
	return ctrl.Result{}, nil
}

// handleGitHubAPIError sets appropriate status conditions and returns a backoff result.
func (r *GitHubRunnerReconciler) handleGitHubAPIError(ctx context.Context, log *slog.Logger, runner *idpv1alpha1.GitHubRunner, reason string, err error) (ctrl.Result, error) {
	log.Error("GitHub API error", "reason", reason, "error", err)
	r.Recorder.Eventf(runner, corev1.EventTypeWarning, reason, "%v", err)

	var authErr *AuthError
	if errors.As(err, &authErr) {
		// Auth errors do not benefit from retrying — set Failed phase.
		if updateErr := r.setFailedStatus(ctx, runner, "AuthenticationFailed", authErr.Message); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil // Do not requeue; user must fix credentials.
	}

	var rateLimitErr *RateLimitError
	requeueAfter := backoffInitial
	if errors.As(err, &rateLimitErr) {
		if d, parseErr := time.ParseDuration(rateLimitErr.RetryAfter + "s"); parseErr == nil {
			requeueAfter = d
		} else {
			requeueAfter = 60 * time.Second
		}
	}
	if requeueAfter > backoffMax {
		requeueAfter = backoffMax
	}

	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            err.Error(),
		LastTransitionTime: metav1.Now(),
	})
	if updateErr := r.statusUpdate(ctx, runner); updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// setFailedStatus sets the runner to Failed phase with a Registered=False condition.
func (r *GitHubRunnerReconciler) setFailedStatus(ctx context.Context, runner *idpv1alpha1.GitHubRunner, reason, message string) error {
	runner.Status.Phase = idpv1alpha1.RunnerPhaseFailed
	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionRegistered,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	return r.statusUpdate(ctx, runner)
}

// updatePodReadyCondition updates PodReady condition based on Pod status.
func (r *GitHubRunnerReconciler) updatePodReadyCondition(runner *idpv1alpha1.GitHubRunner, pod *corev1.Pod) {
	ready := isPodReady(pod)
	status := metav1.ConditionFalse
	reason := "PodNotReady"
	message := "Runner Pod is not ready"
	if ready {
		status = metav1.ConditionTrue
		reason = "PodIsReady"
		message = "Runner Pod is ready"
	}
	meta.SetStatusCondition(&runner.Status.Conditions, metav1.Condition{
		Type:               conditionPodReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

// buildRunnerPod constructs the Pod spec for the GitHub Actions runner.
func (r *GitHubRunnerReconciler) buildRunnerPod(runner *idpv1alpha1.GitHubRunner, podName, runnerName, registrationToken string) *corev1.Pod {
	image := runner.Spec.Image
	if image == "" {
		image = "ghcr.io/actions/runner:latest"
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "idp-grc",
		"app.kubernetes.io/name":       "github-runner",
		"idp.grc.io/runner-cr":         runner.Name,
	}

	env := []corev1.EnvVar{
		{Name: "GITHUB_URL", Value: runner.Spec.GitHubURL},
		{Name: "RUNNER_TOKEN", Value: registrationToken},
		{Name: "RUNNER_NAME", Value: runnerName},
		{Name: "RUNNER_ALLOW_RUNASROOT", Value: "true"},
	}
	if len(runner.Spec.RunnerLabels) > 0 {
		env = append(env, corev1.EnvVar{
			Name:  "RUNNER_LABELS",
			Value: strings.Join(runner.Spec.RunnerLabels, ","),
		})
	}
	if runner.Spec.RunnerGroup != "" {
		env = append(env, corev1.EnvVar{
			Name:  "RUNNER_GROUP",
			Value: runner.Spec.RunnerGroup,
		})
	}

	gracePeriod := int64(30)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: runner.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &gracePeriod,
			RestartPolicy:                 corev1.RestartPolicyAlways,
			Containers: []corev1.Container{
				{
					Name:      "runner",
					Image:     image,
					Env:       env,
					Resources: runner.Spec.Resources,
					Lifecycle: &corev1.Lifecycle{
						PreStop: &corev1.LifecycleHandler{
							Exec: &corev1.ExecAction{
								Command: []string{"/bin/sh", "-c", "./config.sh remove --token $RUNNER_TOKEN || true"},
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference so Pod is garbage-collected when CR is deleted.
	if err := controllerutil.SetControllerReference(runner, pod, r.Scheme); err != nil {
		slog.Error("failed to set controller reference on pod", "error", err)
	}
	return pod
}

// fetchPAT retrieves the Personal Access Token from the referenced Secret.
func (r *GitHubRunnerReconciler) fetchPAT(ctx context.Context, runner *idpv1alpha1.GitHubRunner) (string, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      runner.Spec.CredentialsSecretRef.Name,
		Namespace: runner.Namespace,
	}, secret)
	if err != nil {
		return "", err
	}
	token, ok := secret.Data["token"]
	if !ok {
		return "", fmt.Errorf("secret %q does not contain key \"token\"", runner.Spec.CredentialsSecretRef.Name)
	}
	return string(token), nil
}

// statusUpdate patches the runner status subresource.
func (r *GitHubRunnerReconciler) statusUpdate(ctx context.Context, runner *idpv1alpha1.GitHubRunner) error {
	return r.Status().Update(ctx, runner)
}

// isPodReady returns true if the Pod has the Ready condition set to True.
func isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// Ensure reconciler implements reconcile.Reconciler.
var _ reconcile.Reconciler = &GitHubRunnerReconciler{}
