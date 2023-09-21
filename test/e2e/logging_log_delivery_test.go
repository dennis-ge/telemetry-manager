//go:build e2e

package e2e

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kitk8s "github.com/kyma-project/telemetry-manager/test/testkit/k8s"
	"github.com/kyma-project/telemetry-manager/test/testkit/k8s/verifiers"
	kitlog "github.com/kyma-project/telemetry-manager/test/testkit/kyma/telemetry/log"

	"github.com/kyma-project/telemetry-manager/test/testkit/mocks/backend"
	"github.com/kyma-project/telemetry-manager/test/testkit/mocks/logproducer"
	"github.com/kyma-project/telemetry-manager/test/testkit/mocks/urlprovider"

	. "github.com/kyma-project/telemetry-manager/test/testkit/matchers"
)

type OutputType string

const (
	OutputTypeHTTP   = "http"
	OutputTypeCustom = "custom"
)

var _ = Describe("Logging", Label("logging"), func() {

	Context("When a logpipeline with HTTP output exists", Ordered, func() {
		var (
			urls               *urlprovider.URLProvider
			mockDeploymentName = "log-receiver"
			mockNs             = "log-http-output"
			logProducerName    = "log-producer-http-output" //#nosec G101 -- This is a false positive
		)

		BeforeAll(func() {
			k8sObjects, logsURLProvider := makeLogDeliveryTestK8sObjects(mockNs, mockDeploymentName, logProducerName, OutputTypeHTTP)
			urls = logsURLProvider
			DeferCleanup(func() {
				Expect(kitk8s.DeleteObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
			})
			Expect(kitk8s.CreateObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
		})

		It("Should have a log backend running", Label("operational"), func() {
			logBackendShouldBeRunning(mockDeploymentName, mockNs)
		})

		It("Should have a log producer running", func() {
			deploymentShouldBeReady(logProducerName, mockNs)
		})

		It("Should verify end-to-end log delivery with http ", Label("operational"), func() {
			logsShouldBeDelivered(logProducerName, urls.MockBackendExport(mockDeploymentName))
		})
	})

	Context("When a logpipeline with custom output exists", Ordered, func() {
		var (
			urls               *urlprovider.URLProvider
			mockDeploymentName = "log-receiver"
			mockNs             = "log-custom-output"
			logProducerName    = "log-producer-custom-output" //#nosec G101 -- This is a false positive
		)

		BeforeAll(func() {
			k8sObjects, logsURLProvider := makeLogDeliveryTestK8sObjects(mockNs, mockDeploymentName, logProducerName, OutputTypeCustom)
			urls = logsURLProvider
			DeferCleanup(func() {
				Expect(kitk8s.DeleteObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
			})
			Expect(kitk8s.CreateObjects(ctx, k8sClient, k8sObjects...)).Should(Succeed())
		})

		It("Should have a log backend running", func() {
			logBackendShouldBeRunning(mockDeploymentName, mockNs)
		})

		It("Should verify end-to-end log delivery with custom output", func() {
			logsShouldBeDelivered(logProducerName, urls.MockBackendExport(mockDeploymentName))
		})
	})
})

// TODO this function is the same as deploymentShouldBeRunning except that the timeout is doubled
func logBackendShouldBeRunning(mockDeploymentName, mockNs string) {
	Eventually(func(g Gomega) {
		key := types.NamespacedName{Name: mockDeploymentName, Namespace: mockNs}
		ready, err := verifiers.IsDeploymentReady(ctx, k8sClient, key)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ready).To(BeTrue())
	}, timeout*2, interval).Should(Succeed())
}

func logsShouldBeDelivered(logProducerName string, mockBackendExportUrl string) {
	Eventually(func(g Gomega) {
		resp, err := proxyClient.Get(mockBackendExportUrl)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		g.Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		g.Expect(resp).To(HaveHTTPBody(SatisfyAll(
			ContainLogs(WithPod(logProducerName)))))
	}, timeout, interval).Should(Succeed())
}

func makeLogDeliveryTestK8sObjects(namespace string, mockDeploymentName string, logProducerName string, outputType OutputType) ([]client.Object, *urlprovider.URLProvider) {
	var (
		objs []client.Object
		urls = urlprovider.New()
	)

	mocksNamespace := kitk8s.NewNamespace(namespace)
	objs = append(objs, mocksNamespace.K8sObject())

	//// Mocks namespace objects.
	mockBackend := backend.New(mocksNamespace.Name(), mockDeploymentName, backend.SignalTypeLogs).Build()
	mockLogProducer := logproducer.New(logProducerName, mocksNamespace.Name())
	objs = append(objs, mockBackend.K8sObjects()...)
	objs = append(objs, mockLogProducer.K8sObject(kitk8s.WithLabel("app", "logging-test")))

	// Default namespace objects.
	var logPipeline *kitlog.Pipeline
	if outputType == OutputTypeHTTP {
		logPipeline = kitlog.NewPipeline("http-output-pipeline").WithSecretKeyRef(mockBackend.GetHostSecretRefKey()).WithHTTPOutput()
	} else {
		logPipeline = kitlog.NewPipeline("custom-output-pipeline").WithCustomOutput(mockBackend.ExternalService.Host()) // TODO check if it makes sense to extract the host into a Backend function
	}
	objs = append(objs, logPipeline.K8sObject())

	urls.SetMockBackendExport(mockBackend.Name(), proxyClient.ProxyURLForService(
		namespace, mockBackend.Name(), backend.TelemetryDataFilename, backend.HTTPWebPort),
	)
	return objs, urls
}
