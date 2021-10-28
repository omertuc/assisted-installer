package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/kelseyhightower/envconfig"
	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	"github.com/onsi/ginkgo"
	assistedinstallercontroller "github.com/openshift/assisted-installer/src/assisted_installer_controller"
	"github.com/openshift/assisted-installer/src/inventory_client"
	"github.com/openshift/assisted-installer/src/k8s_client"
	"github.com/openshift/assisted-installer/src/ops"
	"github.com/openshift/assisted-installer/src/utils"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/secretdump"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/sirupsen/logrus"
	configv1 "github.com/openshift/api/config/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Added this way to be able to test it
var (
	exit                        = os.Exit
	waitForInstallationInterval = 1 * time.Minute
)

var Options struct {
	ControllerConfig assistedinstallercontroller.ControllerConfig
}

const maximumErrorsBeforeExit = 3

func prepareDryMock(mockk8sclient *k8s_client.MockK8SClient, logger *logrus.Logger, hostname string, mcsAccessIp string) {
	// Called by main
	mockk8sclient.EXPECT().SetProxyEnvVars().Return(nil).AnyTimes()

	// Called by GetReadyState to make sure we're online
	nodeList := v1.NodeList{}
	mockk8sclient.EXPECT().ListNodes().Return(&nodeList, nil).Times(1)

	// Called a lot
	mockk8sclient.EXPECT().CreateEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Do(func(args ...string) {
		logger.Infof("Fake creating event %+v", args)
	}).AnyTimes()

	// Called by GetReadyState to make sure we're online
	csrs := certificatesv1.CertificateSigningRequestList{}
	mockk8sclient.EXPECT().ListCsrs().Return(&csrs, nil).AnyTimes()

	bmhs := metal3v1alpha1.BareMetalHostList{}
	mockk8sclient.EXPECT().ListBMHs().Return(bmhs, nil).AnyTimes()

	machines := machinev1beta1.MachineList{}
	mockk8sclient.EXPECT().ListMachines().Return(&machines, nil).AnyTimes()

	mockk8sclient.EXPECT().IsMetalProvisioningExists().Return(true, nil).AnyTimes()

	// The controller looks at MCS pod logs to determine whether hosts downloaded ignition or not, so we fake the MCS pod logs
	fakeMcsName := "dry-mcs"
	podListMcs := []v1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: fakeMcsName}}}
	mockk8sclient.EXPECT().GetPods(gomock.Any(), map[string]string{"k8s-app": "machine-config-server"}, gomock.Any()).Return(podListMcs, nil).AnyTimes()
	mockk8sclient.EXPECT().GetPodLogs(gomock.Any(), fakeMcsName, gomock.Any()).Return(fmt.Sprintf(`%s.(Ignition)`, mcsAccessIp), nil).AnyTimes()

	// The controller compares AI host objects to cluster Node objects (Either by name or by IP) to check which AI hosts are already
	// joined as nodes. This fakes the node list so that check will pass
	nodeListPopulated := v1.NodeList{
		Items: []v1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: hostname,
				},
				Status: v1.NodeStatus{
					Conditions: []v1.NodeCondition{
						{
							Type:   v1.NodeReady,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
		},
	}
	mockk8sclient.EXPECT().ListNodes().Return(&nodeListPopulated, nil).AnyTimes()

	// The controller
	myselfName := "dry-controller"
	podListMyself := []v1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: myselfName}}}
	mockk8sclient.EXPECT().GetPods(gomock.Any(), map[string]string{"job-name": "assisted-installer-controller"}, gomock.Any()).Return(podListMyself, nil).AnyTimes()

	b := bytes.NewBufferString(`Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry
Dry`)

	mockk8sclient.EXPECT().GetPodLogsAsBuffer(gomock.Any(), gomock.Any(), gomock.Any()).Return(b, nil).AnyTimes()

	availableConditions := []configv1.ClusterOperatorStatusCondition{
				{
					Type:   configv1.OperatorAvailable,
					Status: configv1.ConditionTrue,
					Message: "All is well",
				},
				{
					Type:   configv1.OperatorProgressing,
					Status: configv1.ConditionTrue,
					Message: "All is well",
				},
				{
					Type:   configv1.OperatorDegraded,
					Status: "All is well",
				},
			}

	clusterOperator := configv1.ClusterOperator{
		Status: 	configv1.ClusterOperatorStatus{
			Conditions: availableConditions,
		},
	}
	mockk8sclient.EXPECT().GetClusterOperator(gomock.Any()).Return(&clusterOperator, nil).AnyTimes()

	clusterVersion := configv1.ClusterVersion{
		Status: configv1.ClusterVersionStatus{
			Conditions: availableConditions,
		},
	}
	mockk8sclient.EXPECT().GetClusterVersion(gomock.Any()).Return(&clusterVersion, nil).AnyTimes()

	configMap := v1.ConfigMap{
		Data: map[string]string{
			"ca-bundle.crt": 
`-----BEGIN CERTIFICATE-----
MIIExTCCAq0CFCqc5fg5zMGmG6yY/PsVukKCsTWiMA0GCSqGSIb3DQEBCwUAMB8x
CzAJBgNVBAYTAlVTMRAwDgYDVQQKDAdSZWQgSGF0MB4XDTIxMTAyOTIyNTY0MVoX
DTIyMTAyOTIyNTY0MVowHzELMAkGA1UEBhMCVVMxEDAOBgNVBAoMB1JlZCBIYXQw
ggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQDqR5zsx+2WpNTO0RBYFrQd
swo5ALC9XIj1EfcxXECdtZE/6ZBXS/bxkN7DsB5ych9GVgBrmX0093Qng9US/CXF
5vTthG/+BhH0u3+6x6bRLagqayuRWiD/mQRZ10X1EswIebH6pMXXKweLK/Sg9PlB
FtD2JNIQhirdUSMkF4ud0yoW66YE+vJGFyHBAEB5A2ws+4ymaxyVYBJVfdS8nCfU
gPee1h6DjmUO8GyeF3kY2eVERqTW6E+BhrjOB1DOcHacxj4t2CQMUJRXNqL2QMLz
n1tRyBPoVI8BjQNzI8+Hb6g6vKIFvtVoJrqeT/ASgZkEaPJt4sP1ss2ODhGUuLNB
fz/ZzFgglswJEFhBtv4J2zXkoI69KdFAxaa37qULL6MxNQ+y4LhfBc5WzqHdg4EI
5xtbuCpHErpc1VIDe1Ok3NXsFHN99tH+vwA6fcYmz9VjE/HKMHRzwNRjmDW1cdNO
uAgZTkslhfiu1rjxlIetU6LH2lBKy9UtoRjNw014F2IJeje8j1WHuR/ih705qkVf
wYG2NKMRRUV12tpKwuqX/TQFa++aB95vhjZsAtrB2P66CWROtFCjd8woHEkZEGHt
Gh8R4UXxk2VvHlglb8tvEr+n3Fuz41dLZeepZR2CzaySgjLUAqOahO3ZmEitUtiw
GB5Q3+bhB9lUVFd0IGuQEwIDAQABMA0GCSqGSIb3DQEBCwUAA4ICAQBi507wwqP+
Yc8xEeKXxazheIUuf1o9WH1XTdUJPklRdwZj7HxZa49FzammW7MWhVNbqsD6bZdp
5Iy9JCsJBP5Z6gWbP3LgypcWw4xmNiPXZw+9pbnRmIiObGvWEnHmtI6MTvAHZttd
sEnrvH7LV2Dr7TZzfV7mrOh2JgDlQ5yOvXx9x9sV9GaqGbx5tK11S//Th5TfGkXQ
CoygE/SwZPAHM4jcU//j5/QbYegtJIVFK/JQrMcc37ecwcYf0f3q5GZ/c4zUBQ7Z
BZMSGMGObxNqIIW3QsB+sZyfpZxWUanxJiKy9Uw8jBq+zswWep8WXnFtL1wyHeiB
MpYEls0yPGsCeEF3vFlpFR1Aob0nLAimAEyxf4GUZiI1CCqWzhIQ8jaiSfnsyh9f
irj1Q/xTIEK4sbyl//QXLpW/OXgXUG6WIlyvg1LPdbngfU5S8DxSXse9JHIno+cD
7Ugdiw+3c32FQnX4vqKLhtT7IClWmyTN84tcKMJVKhreQ+Yz0+eCTIwV5JQFTsRp
tGxE/NUwbjuRib3HvsiuCUIcRQKJQerdAYWob47cnIA/YH0Hngq1Ci1GtcuYJQnP
dEFgad6P3hMZTOg7yVkMOd3QtgVQ9I8dXqS2nG9EMEh97WIhi6f5ztvcQvQ5tXjh
1OZbvvo716WbONeK0GuS3WbwVTQFSUBtCA==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIExTCCAq0CFCqc5fg5zMGmG6yY/PsVukKCsTWiMA0GCSqGSIb3DQEBCwUAMB8x
CzAJBgNVBAYTAlVTMRAwDgYDVQQKDAdSZWQgSGF0MB4XDTIxMTAyOTIyNTY0MVoX
DTIyMTAyOTIyNTY0MVowHzELMAkGA1UEBhMCVVMxEDAOBgNVBAoMB1JlZCBIYXQw
ggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQDqR5zsx+2WpNTO0RBYFrQd
swo5ALC9XIj1EfcxXECdtZE/6ZBXS/bxkN7DsB5ych9GVgBrmX0093Qng9US/CXF
5vTthG/+BhH0u3+6x6bRLagqayuRWiD/mQRZ10X1EswIebH6pMXXKweLK/Sg9PlB
FtD2JNIQhirdUSMkF4ud0yoW66YE+vJGFyHBAEB5A2ws+4ymaxyVYBJVfdS8nCfU
gPee1h6DjmUO8GyeF3kY2eVERqTW6E+BhrjOB1DOcHacxj4t2CQMUJRXNqL2QMLz
n1tRyBPoVI8BjQNzI8+Hb6g6vKIFvtVoJrqeT/ASgZkEaPJt4sP1ss2ODhGUuLNB
fz/ZzFgglswJEFhBtv4J2zXkoI69KdFAxaa37qULL6MxNQ+y4LhfBc5WzqHdg4EI
5xtbuCpHErpc1VIDe1Ok3NXsFHN99tH+vwA6fcYmz9VjE/HKMHRzwNRjmDW1cdNO
uAgZTkslhfiu1rjxlIetU6LH2lBKy9UtoRjNw014F2IJeje8j1WHuR/ih705qkVf
wYG2NKMRRUV12tpKwuqX/TQFa++aB95vhjZsAtrB2P66CWROtFCjd8woHEkZEGHt
Gh8R4UXxk2VvHlglb8tvEr+n3Fuz41dLZeepZR2CzaySgjLUAqOahO3ZmEitUtiw
GB5Q3+bhB9lUVFd0IGuQEwIDAQABMA0GCSqGSIb3DQEBCwUAA4ICAQBi507wwqP+
Yc8xEeKXxazheIUuf1o9WH1XTdUJPklRdwZj7HxZa49FzammW7MWhVNbqsD6bZdp
5Iy9JCsJBP5Z6gWbP3LgypcWw4xmNiPXZw+9pbnRmIiObGvWEnHmtI6MTvAHZttd
sEnrvH7LV2Dr7TZzfV7mrOh2JgDlQ5yOvXx9x9sV9GaqGbx5tK11S//Th5TfGkXQ
CoygE/SwZPAHM4jcU//j5/QbYegtJIVFK/JQrMcc37ecwcYf0f3q5GZ/c4zUBQ7Z
BZMSGMGObxNqIIW3QsB+sZyfpZxWUanxJiKy9Uw8jBq+zswWep8WXnFtL1wyHeiB
MpYEls0yPGsCeEF3vFlpFR1Aob0nLAimAEyxf4GUZiI1CCqWzhIQ8jaiSfnsyh9f
irj1Q/xTIEK4sbyl//QXLpW/OXgXUG6WIlyvg1LPdbngfU5S8DxSXse9JHIno+cD
7Ugdiw+3c32FQnX4vqKLhtT7IClWmyTN84tcKMJVKhreQ+Yz0+eCTIwV5JQFTsRp
tGxE/NUwbjuRib3HvsiuCUIcRQKJQerdAYWob47cnIA/YH0Hngq1Ci1GtcuYJQnP
dEFgad6P3hMZTOg7yVkMOd3QtgVQ9I8dXqS2nG9EMEh97WIhi6f5ztvcQvQ5tXjh
1OZbvvo716WbONeK0GuS3WbwVTQFSUBtCA==
-----END CERTIFICATE-----
`,
		},
	}
	mockk8sclient.EXPECT().GetConfigMap(gomock.Any(), gomock.Any()).Return(&configMap, nil).AnyTimes()
}

func RebootComplete() bool {
	if _, err := os.Stat(Options.ControllerConfig.DryFakeRebootMarkerPath); err == nil {
		return true
	}

	return false
}

func main() {
	logger := logrus.New()

	err := envconfig.Process("myapp", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	if Options.ControllerConfig.DryRunEnabled {
		// In dry run mode, we need to wait for the reboot to complete before starting the controller
		for !RebootComplete() {
			time.Sleep(time.Second * 1)
		}
	}

	logger.Infof("Start running Assisted-Controller. Configuration is:\n %s", secretdump.DumpSecretStruct(Options.ControllerConfig))

	var kc k8s_client.K8SClient
	if !Options.ControllerConfig.DryRunEnabled {
		kc, err = k8s_client.NewK8SClient("", logger)
		if err != nil {
			log.Fatalf("Failed to create k8 client %v", err)
		}
	} else {
		mockController := gomock.NewController(ginkgo.GinkgoT())
		kc = k8s_client.NewMockK8SClient(mockController)
		mock, _ := kc.(*k8s_client.MockK8SClient)
		prepareDryMock(mock, logger, Options.ControllerConfig.DryRunHostname, Options.ControllerConfig.DryMcsAccessIp)
	}

	err = kc.SetProxyEnvVars()
	if err != nil {
		log.Fatalf("Failed to set env vars for installer-controller pod %v", err)
	}

	client, err := inventory_client.CreateInventoryClient(Options.ControllerConfig.ClusterID,
		Options.ControllerConfig.URL, Options.ControllerConfig.PullSecretToken, Options.ControllerConfig.SkipCertVerification,
		Options.ControllerConfig.CACertPath, logger, utils.ProxyFromEnvVars)
	if err != nil {
		log.Fatalf("Failed to create inventory client %v", err)
	}

	assistedController := assistedinstallercontroller.NewController(logger,
		Options.ControllerConfig,
		ops.NewOps(logger, false),
		client,
		kc,
	)

	var wg sync.WaitGroup
	mainContext, mainContextCancel := context.WithCancel(context.Background())

	// No need to cancel with context, will finish quickly
	// we should fix try to fix dns service issue as soon as possible
	if !Options.ControllerConfig.DryRunEnabled {
		// This check is unnecessary in dry run mode, and mocking for it is complicated
		go assistedController.HackDNSAddressConflict(&wg)
		wg.Add(1)
	}

	assistedController.SetReadyState()

	// While adding new routine don't miss to add wg.add(1)
	// without adding it will panic

	defer func() {
		// stop all go routines
		mainContextCancel()
		logger.Infof("Waiting for all go routines to finish")
		wg.Wait()
		logger.Infof("Finished all")
	}()

	go assistedController.WaitAndUpdateNodesStatus(mainContext, &wg)
	wg.Add(1)
	go assistedController.PostInstallConfigs(mainContext, &wg)
	wg.Add(1)
	go assistedController.UpdateBMHs(mainContext, &wg)
	wg.Add(1)

	go assistedController.UploadLogs(mainContext, &wg)
	wg.Add(1)

	// monitoring installation by cluster status
	waitForInstallation(client, logger, assistedController.Status)
}

// waitForInstallation monitor cluster status and is blocking main from cancelling all go routine s
// if cluster status is (cancelled,installed) there is no need to continue and we will exit.
// if cluster is in error, in addition to stop waiting we need to set error status to tell upload logs to send must-gather.
// if we have maximumErrorsBeforeExit GetClusterNotFound/GetClusterUnauthorized errors in a row we force exiting controller
func waitForInstallation(client inventory_client.InventoryClient, log logrus.FieldLogger, status *assistedinstallercontroller.ControllerStatus) {
	log.Infof("monitor cluster installation status")
	reqCtx := utils.GenerateRequestContext()
	errCounter := 0

	for {
		time.Sleep(waitForInstallationInterval)
		cluster, err := client.GetCluster(reqCtx)
		if err != nil {
			// In case cluster was deleted or controller is not authorised
			// we should exit controller after maximumErrorsBeforeExit errors
			// in case cluster was deleted we should exit immediately
			switch err.(type) {
			case *installer.V2GetClusterNotFound:
				errCounter = errCounter + maximumErrorsBeforeExit
				log.WithError(err).Errorf("Cluster was not found in inventory or user is not authorized")
			case *installer.V2GetClusterUnauthorized:
				errCounter++
				log.WithError(err).Errorf("User is not authenticated to perform the operation")
			}

			// if we get maximumErrorsBeforeExit errors in a row
			// there is no point to try to reach assisted service
			// we should exit with 0 cause in case of another exit status
			// job will restart assisted-controller.
			if errCounter >= maximumErrorsBeforeExit {
				log.Infof("Got more than %d errors from assisted service in a row, exiting", maximumErrorsBeforeExit)
				exit(0)
			}
			continue
		}
		// reset error counter in case no error occurred
		errCounter = 0
		finished := handleClusterStatus(*cluster.Status, log, status)
		if finished {
			return
		}
	}
}

func handleClusterStatus(clusterStatus string, log logrus.FieldLogger, status *assistedinstallercontroller.ControllerStatus) bool {
	switch clusterStatus {
	case models.ClusterStatusError:
		log.Infof("Cluster installation failed.")
		status.Error()
		return true
	case models.ClusterStatusCancelled:
		log.Infof("Cluster installation aborted. Signal the status")
		return true
	case models.ClusterStatusInstalled:
		log.Infof("Cluster installation successfully finished.")
		return true
	}
	return false
}
