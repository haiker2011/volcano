package magic

import (
	v1 "k8s.io/api/core/v1"

	"k8s.io/klog/v2"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	Name = "magic"
)

type magicPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string

	Clientset pluginsinterface.PluginClientset
}

// New creates env plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	magicPlugin := magicPlugin{}

	return &magicPlugin
}

func (ep *magicPlugin) Name() string {
	return Name
}

func (ep *magicPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	klog.V(4).Info("magic plugin OnPodCreate")
	return nil
}

func (ep *magicPlugin) OnJobAdd(job *batch.Job) error {
	klog.V(4).Info("magic plugin OnJobAdd")
	return nil
}

func (ep *magicPlugin) OnJobDelete(job *batch.Job) error {
	klog.V(4).Info("magic plugin OnJobDelete")
	return nil
}

func (ep *magicPlugin) OnJobUpdate(job *batch.Job) error {
	klog.V(4).Info("magic plugin OnJobUpdate")
	return nil
}
