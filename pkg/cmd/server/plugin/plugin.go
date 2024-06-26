/*
Copyright 2017, 2019 the Velero contributors.

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

package plugin

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"

	"github.com/vmware-tanzu/velero/pkg/datamover"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	bia "github.com/vmware-tanzu/velero/pkg/backup/actions"
	"github.com/vmware-tanzu/velero/pkg/client"
	velerodiscovery "github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/features"
	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
	plugincommon "github.com/vmware-tanzu/velero/pkg/plugin/framework/common"
	ria "github.com/vmware-tanzu/velero/pkg/restore/actions"
)

func NewCommand(f client.Factory) *cobra.Command {
	pluginServer := veleroplugin.NewServer()
	c := &cobra.Command{
		Use:    "run-plugins",
		Hidden: true,
		Short:  "INTERNAL COMMAND ONLY - not intended to be run directly by users",
		Run: func(c *cobra.Command, args []string) {
			pluginServer = pluginServer.
				RegisterBackupItemAction("velero.io/pv", newPVBackupItemAction).
				RegisterBackupItemAction("velero.io/pod", newPodBackupItemAction).
				RegisterBackupItemAction("velero.io/service-account", newServiceAccountBackupItemAction(f)).
				RegisterRestoreItemAction("velero.io/job", newJobRestoreItemAction).
				RegisterRestoreItemAction("velero.io/pod", newPodRestoreItemAction).
				RegisterRestoreItemAction("velero.io/pod-volume-restore", newPodVolumeRestoreItemAction(f)).
				RegisterRestoreItemAction("velero.io/init-restore-hook", newInitRestoreHookPodAction).
				RegisterRestoreItemAction("velero.io/service", newServiceRestoreItemAction).
				RegisterRestoreItemAction("velero.io/service-account", newServiceAccountRestoreItemAction).
				RegisterRestoreItemAction("velero.io/add-pvc-from-pod", newAddPVCFromPodRestoreItemAction).
				RegisterRestoreItemAction("velero.io/add-pv-from-pvc", newAddPVFromPVCRestoreItemAction).
				RegisterRestoreItemAction("velero.io/change-storage-class", newChangeStorageClassRestoreItemAction(f)).
				RegisterRestoreItemAction("velero.io/change-image-name", newChangeImageNameRestoreItemAction(f)).
				RegisterRestoreItemAction("velero.io/role-bindings", newRoleBindingItemAction).
				RegisterRestoreItemAction("velero.io/cluster-role-bindings", newClusterRoleBindingItemAction).
				RegisterRestoreItemAction("velero.io/crd-preserve-fields", newCRDV1PreserveUnknownFieldsItemAction).
				RegisterRestoreItemAction("velero.io/change-pvc-node-selector", newChangePVCNodeSelectorItemAction(f)).
				RegisterRestoreItemAction("velero.io/apiservice", newAPIServiceRestoreItemAction).
				RegisterRestoreItemAction("velero.io/admission-webhook-configuration", newAdmissionWebhookConfigurationAction).
				RegisterRestoreItemAction("velero.io/secret", newSecretRestoreItemAction(f)).
				RegisterRestoreItemAction("velero.io/dataupload", newDataUploadRetrieveAction(f)).
				RegisterDeleteItemAction("velero.io/dataupload-delete", newDateUploadDeleteItemAction(f))

			if !features.IsEnabled(velerov1api.APIGroupVersionsFeatureFlag) {
				// Do not register crd-remap-version BIA if the API Group feature flag is enabled, so that the v1 CRD can be backed up
				pluginServer = pluginServer.RegisterBackupItemAction("velero.io/crd-remap-version", newRemapCRDVersionAction(f))
			}
			pluginServer.Serve()
		},
	}
	pluginServer.BindFlags(c.Flags())
	return c
}

func newPVBackupItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return bia.NewPVCAction(logger), nil
}

func newPodBackupItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return bia.NewPodAction(logger), nil
}

func newServiceAccountBackupItemAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		// TODO(ncdc): consider a k8s style WantsKubernetesClientSet initialization approach
		clientset, err := f.KubeClient()
		if err != nil {
			return nil, err
		}

		discoveryHelper, err := velerodiscovery.NewHelper(clientset.Discovery(), logger)
		if err != nil {
			return nil, err
		}

		action, err := bia.NewServiceAccountAction(
			logger,
			bia.NewClusterRoleBindingListerMap(clientset),
			discoveryHelper)
		if err != nil {
			return nil, err
		}

		return action, nil
	}
}

func newRemapCRDVersionAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		config, err := f.ClientConfig()
		if err != nil {
			return nil, err
		}

		client, err := apiextensions.NewForConfig(config)
		if err != nil {
			return nil, err
		}

		clientset, err := f.KubeClient()
		if err != nil {
			return nil, err
		}
		discoveryHelper, err := velerodiscovery.NewHelper(clientset.Discovery(), logger)
		if err != nil {
			return nil, err
		}

		return bia.NewRemapCRDVersionAction(logger, client.ApiextensionsV1beta1().CustomResourceDefinitions(), discoveryHelper), nil
	}
}

func newJobRestoreItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewJobAction(logger), nil
}

func newPodRestoreItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewPodAction(logger), nil
}

func newInitRestoreHookPodAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewInitRestoreHookPodAction(logger), nil
}

func newPodVolumeRestoreItemAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		client, err := f.KubeClient()
		if err != nil {
			return nil, err
		}

		crClient, err := f.KubebuilderClient()
		if err != nil {
			return nil, err
		}

		return ria.NewPodVolumeRestoreAction(logger, client.CoreV1().ConfigMaps(f.Namespace()), crClient), nil
	}
}

func newServiceRestoreItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewServiceAction(logger), nil
}

func newServiceAccountRestoreItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewServiceAccountAction(logger), nil
}

func newAddPVCFromPodRestoreItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewAddPVCFromPodAction(logger), nil
}

func newAddPVFromPVCRestoreItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewAddPVFromPVCAction(logger), nil
}

func newCRDV1PreserveUnknownFieldsItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewCRDV1PreserveUnknownFieldsAction(logger), nil
}

func newChangeStorageClassRestoreItemAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		client, err := f.KubeClient()
		if err != nil {
			return nil, err
		}

		return ria.NewChangeStorageClassAction(
			logger,
			client.CoreV1().ConfigMaps(f.Namespace()),
			client.StorageV1().StorageClasses(),
		), nil
	}
}

func newChangeImageNameRestoreItemAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		client, err := f.KubeClient()
		if err != nil {
			return nil, err
		}

		return ria.NewChangeImageNameAction(
			logger,
			client.CoreV1().ConfigMaps(f.Namespace()),
		), nil
	}
}
func newRoleBindingItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewRoleBindingAction(logger), nil
}

func newClusterRoleBindingItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewClusterRoleBindingAction(logger), nil
}

func newChangePVCNodeSelectorItemAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		client, err := f.KubeClient()
		if err != nil {
			return nil, err
		}

		return ria.NewChangePVCNodeSelectorAction(
			logger,
			client.CoreV1().ConfigMaps(f.Namespace()),
			client.CoreV1().Nodes(),
		), nil
	}
}

func newAPIServiceRestoreItemAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewAPIServiceAction(logger), nil
}

func newAdmissionWebhookConfigurationAction(logger logrus.FieldLogger) (interface{}, error) {
	return ria.NewAdmissionWebhookConfigurationAction(logger), nil
}

func newSecretRestoreItemAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		client, err := f.KubebuilderClient()
		if err != nil {
			return nil, err
		}
		return ria.NewSecretAction(logger, client), nil
	}
}

func newDataUploadRetrieveAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		client, err := f.KubebuilderClient()
		if err != nil {
			return nil, err
		}

		return ria.NewDataUploadRetrieveAction(logger, client), nil
	}
}

func newDateUploadDeleteItemAction(f client.Factory) plugincommon.HandlerInitializer {
	return func(logger logrus.FieldLogger) (interface{}, error) {
		client, err := f.KubebuilderClient()
		if err != nil {
			return nil, err
		}
		return datamover.NewDataUploadDeleteAction(logger, client), nil
	}
}
