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

package controller_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	idpv1alpha1 "github.com/davidsugianto/idp-grc/api/v1alpha1"
	"github.com/davidsugianto/idp-grc/internal/controller"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	ctx        context.Context
	cancel     context.CancelFunc
	scheme     *k8sruntime.Scheme
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())

	scheme = k8sruntime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(idpv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(corev1.AddToScheme(scheme)).To(Succeed())

	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(projectRoot, "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
	})
	Expect(err).NotTo(HaveOccurred())

	// Use the fake GitHub client injected per-test via TestGitHubClient wrapper.
	// For suite-level setup we use a no-op client; individual tests override via server.
	err = (&controller.GitHubRunnerReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("githubrunner-controller-test"),
		GitHub:   controller.NewGitHubClient(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	Expect(testEnv.Stop()).To(Succeed())
})

// waitForPhase blocks until the GitHubRunner reaches the expected phase or times out.
func waitForPhase(ns, name string, phase idpv1alpha1.RunnerPhase, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func() idpv1alpha1.RunnerPhase {
		runner := &idpv1alpha1.GitHubRunner{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, runner); err != nil {
			return ""
		}
		return runner.Status.Phase
	}, timeout, 500*time.Millisecond).Should(Equal(phase))
}

// waitForCondition blocks until the named condition has the expected status.
func waitForCondition(ns, name, condType string, status metav1.ConditionStatus, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func() metav1.ConditionStatus {
		runner := &idpv1alpha1.GitHubRunner{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, runner); err != nil {
			return ""
		}
		c := meta.FindStatusCondition(runner.Status.Conditions, condType)
		if c == nil {
			return ""
		}
		return c.Status
	}, timeout, 500*time.Millisecond).Should(Equal(status))
}
