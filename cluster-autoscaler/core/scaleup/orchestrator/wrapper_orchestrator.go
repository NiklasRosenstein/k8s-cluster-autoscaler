/*
Copyright 2024 The Kubernetes Authors.

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

package orchestrator

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/clusterstate"
	"k8s.io/autoscaler/cluster-autoscaler/context"
	"k8s.io/autoscaler/cluster-autoscaler/core/scaleup"
	ca_processors "k8s.io/autoscaler/cluster-autoscaler/processors"
	"k8s.io/autoscaler/cluster-autoscaler/processors/provreq"
	"k8s.io/autoscaler/cluster-autoscaler/processors/status"
	"k8s.io/autoscaler/cluster-autoscaler/provisioningrequest/checkcapacity"
	"k8s.io/autoscaler/cluster-autoscaler/utils/errors"
	"k8s.io/autoscaler/cluster-autoscaler/utils/taints"
	"k8s.io/client-go/rest"
	schedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

// WrapperOrchestrator is an orchestrator which wraps Scale Up for ProvisioningRequests and regular pods.
// Each loop WrapperOrchestrator split out regular and pods from ProvisioningRequest, pick one group that
// wasn't picked in the last loop and run ScaleUp for it.
type WrapperOrchestrator struct {
	// scaleUpRegularPods indicates that ScaleUp for regular pods will be run in the current CA loop, if they are present.
	scaleUpRegularPods  bool
	scaleUpOrchestrator scaleup.Orchestrator
	provReqOrchestrator scaleup.Orchestrator
}

// NewWrapperOrchestrator return WrapperOrchestrator
func NewWrapperOrchestrator(kubeConfig *rest.Config) (scaleup.Orchestrator, error) {
	provReqOrchestrator, err := checkcapacity.New(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed create ScaleUp orchestrator for ProvisioningRequests, error: %v", err)
	}
	return &WrapperOrchestrator{
		scaleUpOrchestrator: New(),
		provReqOrchestrator: provReqOrchestrator,
	}, nil
}

// Initialize initializes the orchestrator object with required fields.
func (o *WrapperOrchestrator) Initialize(
	autoscalingContext *context.AutoscalingContext,
	processors *ca_processors.AutoscalingProcessors,
	clusterStateRegistry *clusterstate.ClusterStateRegistry,
	taintConfig taints.TaintConfig,
) {
	o.scaleUpOrchestrator.Initialize(autoscalingContext, processors, clusterStateRegistry, taintConfig)
	o.provReqOrchestrator.Initialize(autoscalingContext, processors, clusterStateRegistry, taintConfig)
}

// ScaleUp run scaleUp function for regular pods of pods from ProvisioningRequest.
func (o *WrapperOrchestrator) ScaleUp(
	unschedulablePods []*apiv1.Pod,
	nodes []*apiv1.Node,
	daemonSets []*appsv1.DaemonSet,
	nodeInfos map[string]*schedulerframework.NodeInfo,
) (*status.ScaleUpStatus, errors.AutoscalerError) {
	defer func() { o.scaleUpRegularPods = !o.scaleUpRegularPods }()

	provReqPods, regularPods := splitOut(unschedulablePods)
	if len(provReqPods) == 0 {
		o.scaleUpRegularPods = true
	} else if len(regularPods) == 0 {
		o.scaleUpRegularPods = false
	}

	if o.scaleUpRegularPods {
		return o.scaleUpOrchestrator.ScaleUp(regularPods, nodes, daemonSets, nodeInfos)
	}
	return o.provReqOrchestrator.ScaleUp(provReqPods, nodes, daemonSets, nodeInfos)
}

func splitOut(unschedulablePods []*apiv1.Pod) (provReqPods, regularPods []*apiv1.Pod) {
	for _, pod := range unschedulablePods {
		if _, ok := pod.Annotations[provreq.ProvisioningRequestPodAnnotationKey]; ok {
			provReqPods = append(provReqPods, pod)
		} else {
			regularPods = append(regularPods, pod)
		}
	}
	return
}

// ScaleUpToNodeGroupMinSize tries to scale up node groups that have less nodes
// than the configured min size. The source of truth for the current node group
// size is the TargetSize queried directly from cloud providers. Returns
// appropriate status or error if an unexpected error occurred.
func (o *WrapperOrchestrator) ScaleUpToNodeGroupMinSize(
	nodes []*apiv1.Node,
	nodeInfos map[string]*schedulerframework.NodeInfo,
) (*status.ScaleUpStatus, errors.AutoscalerError) {
	return o.scaleUpOrchestrator.ScaleUpToNodeGroupMinSize(nodes, nodeInfos)
}
