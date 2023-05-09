package sshpiper

import (
	"context"
	"flag"

	piperv1beta1 "github.com/tg123/sshpiper/plugin/kubernetes/apis/sshpiper/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	Name = "sshpiper"
)

type sshpiperPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string
	Clientset       pluginsinterface.PluginClientset
	// flag parse args
	secretName     string
	pipeName       string
	sshPrivateKey  string
	authorizedKeys string
	toHost         string
	toUser         string
}

// New creates env plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	sshpiperPlugin := sshpiperPlugin{pluginArguments: arguments, Clientset: client}
	sshpiperPlugin.addFlags()
	return &sshpiperPlugin
}

func (spp *sshpiperPlugin) Name() string {
	return Name
}

// add flags
func (spp *sshpiperPlugin) addFlags() {
	flagSet := flag.NewFlagSet(spp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&spp.secretName, "secret-name", spp.secretName, "set secret name, it is `` by default.")
	flagSet.StringVar(&spp.pipeName, "pipe-name", spp.pipeName, "set pipe name, it is `` by default.")
	flagSet.StringVar(&spp.sshPrivateKey, "ssh-private-key", spp.sshPrivateKey, "set ssh private key, it is `` by default.")
	flagSet.StringVar(&spp.authorizedKeys, "authorized-keys", spp.authorizedKeys, "set authorized keys, it is `` by default.")
	flagSet.StringVar(&spp.toHost, "to-host", spp.toHost, "set to host, it is `` by default.")
	flagSet.StringVar(&spp.toUser, "to-user", spp.toUser, "set to user, it is `` by default.")
}

func (spp *sshpiperPlugin) OnPodCreate(pod *corev1.Pod, job *batch.Job) error {
	klog.V(4).Info("sshpiper plugin OnPodCreate")
	return nil
}

func (spp *sshpiperPlugin) OnJobAdd(job *batch.Job) error {
	klog.V(4).Info("sshpiper plugin OnJobAdd")
	if job.Status.ControlledResources["plugin-"+spp.Name()] == spp.Name() {
		return nil
	}
	// Create Secret for Job.
	if err := spp.createSecretIfNotExist(job); err != nil {
		return err
	}
	// Create Pipe for Job.
	if err := spp.createPipeIfNotExist(job); err != nil {
		return err
	}

	job.Status.ControlledResources["plugin-"+spp.Name()] = spp.Name()
	return nil
}

func (spp *sshpiperPlugin) OnJobDelete(job *batch.Job) error {
	klog.V(4).Info("sshpiper plugin OnJobDelete")
	if job.Status.ControlledResources["plugin-"+spp.Name()] != spp.Name() {
		return nil
	}
	// Delete Pipe for Job.
	if err := spp.Clientset.SSHPiperClient.SshpiperV1beta1().Pipes(job.Namespace).Delete(context.TODO(), spp.pipeName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to delete Pipe for Job <%s/%s>: %v", job.Namespace, job.Name, err)
			return err
		}
	}
	// Delete Secret for Job.
	if err := spp.Clientset.KubeClients.CoreV1().Secrets(job.Namespace).Delete(context.TODO(), spp.secretName, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to delete Secret for Job <%s/%s>: %v", job.Namespace, job.Name, err)
			return err
		}
	}

	delete(job.Status.ControlledResources, "plugin-"+spp.Name())
	return nil
}

func (spp *sshpiperPlugin) OnJobUpdate(job *batch.Job) error {
	klog.V(4).Info("sshpiper plugin OnJobUpdate")
	return nil
}

func (spp *sshpiperPlugin) createSecretIfNotExist(job *batch.Job) error {
	// If Secret does not exist, create one for Job.
	if _, err := spp.Clientset.KubeClients.CoreV1().Secrets(job.Namespace).Get(context.TODO(), spp.secretName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get Secret for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      spp.secretName,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Data: map[string][]byte{
				"ssh-private-key": []byte(spp.sshPrivateKey),
			},

			Type: corev1.SecretTypeSSHAuth,
		}

		if _, e := spp.Clientset.KubeClients.CoreV1().Secrets(job.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create Secret for Job <%s/%s>: %v", job.Namespace, job.Name, e)
			return e
		}
		job.Status.ControlledResources["plugin-"+spp.Name()] = spp.Name()

	}

	return nil
}

func (spp *sshpiperPlugin) createPipeIfNotExist(job *batch.Job) error {
	if _, err := spp.Clientset.SSHPiperClient.SshpiperV1beta1().Pipes(job.Namespace).Get(context.TODO(), spp.pipeName, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get Pipe for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		pipe := &piperv1beta1.Pipe{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      spp.pipeName,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: piperv1beta1.PipeSpec{
				From: []piperv1beta1.FromSpec{
					{
						Username:           ".*",
						UsernameRegexMatch: true,
						AuthorizedKeysData: spp.authorizedKeys,
					},
				},
				To: piperv1beta1.ToSpec{
					Host:     spp.toHost,
					Username: spp.toUser,
					PrivateKeySecret: corev1.LocalObjectReference{
						Name: spp.secretName,
					},
					IgnoreHostkey: true,
				},
			},
		}

		if _, e := spp.Clientset.SSHPiperClient.SshpiperV1beta1().Pipes(job.Namespace).Create(context.TODO(), pipe, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create Pipe for Job <%s/%s>: %v", job.Namespace, job.Name, e)
			return e
		}
		job.Status.ControlledResources["plugin-"+spp.Name()] = spp.Name()

	}
	return nil
}
