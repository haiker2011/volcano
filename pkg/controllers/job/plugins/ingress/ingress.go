package ingress

import (
	"context"
	"flag"
	"fmt"

	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	Name = "ingress"
)

type ingressPlugin struct {
	// Arguments given for the plugin
	pluginArguments []string
	Clientset       pluginsinterface.PluginClientset
	// flag parse args
	publishNotReadyAddresses bool
	disableNetworkPolicy     bool
	ingressClass             string
	ingressHost              string
	ingressPath              string
	svcPort                  int
	rewriteTarget            string
	prefixPathType           string
}

// New creates env plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	ingressPlugin := ingressPlugin{pluginArguments: arguments, Clientset: client,
		ingressClass:   DefaultIngressClass,
		ingressPath:    DefaultIngressPath,
		svcPort:        DefaultSVCPort,
		prefixPathType: string(networkingv1.PathTypePrefix),
	}
	ingressPlugin.addFlags()
	return &ingressPlugin
}

func (ip *ingressPlugin) Name() string {
	return Name
}

func (ip *ingressPlugin) addFlags() {
	flagSet := flag.NewFlagSet(ip.Name(), flag.ContinueOnError)
	flagSet.BoolVar(&ip.publishNotReadyAddresses, "publish-not-ready-addresses", ip.publishNotReadyAddresses,
		"set publishNotReadyAddresses of svc to true")
	flagSet.BoolVar(&ip.disableNetworkPolicy, "disable-network-policy", ip.disableNetworkPolicy,
		"set disableNetworkPolicy of svc to true")
	flagSet.StringVar(&ip.ingressClass, "ingress-class", ip.ingressClass,
		"set ingress class, it is `nginx` by default.")
	flagSet.StringVar(&ip.ingressHost, "ingress-host", ip.ingressHost,
		"set ingress host, it is `` by default.")
	flagSet.StringVar(&ip.ingressPath, "ingress-path", ip.ingressPath,
		"set ingress path, it is `/` by default.")
	flagSet.IntVar(&ip.svcPort, "svc-port", ip.svcPort,
		"set svc port, it is `80` by default.")
	flagSet.StringVar(&ip.rewriteTarget, "rewrite-target", ip.rewriteTarget,
		"set rewrite target, it is `/` by default.")
	flagSet.StringVar(&ip.prefixPathType, "prefix-path-type", ip.prefixPathType,
		"set prefix path type, it is `PrefixPathType` by default.")

	if err := flagSet.Parse(ip.pluginArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", ip.Name(), err)
	}
}

func (ip *ingressPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	klog.V(4).Info("ingress plugin OnPodCreate")
	return nil
}

func (ip *ingressPlugin) OnJobAdd(job *batch.Job) error {
	klog.V(4).Info("ingress plugin OnJobAdd")
	if job.Status.ControlledResources["plugin-"+ip.Name()] == ip.Name() {
		return nil
	}

	// Create ConfigMap of hosts for Pods to mount.
	if err := ip.createIngressIfNotExist(job); err != nil {
		return err
	}

	if err := ip.createServiceIfNotExist(job); err != nil {
		return err
	}

	job.Status.ControlledResources["plugin-"+ip.Name()] = ip.Name()

	return nil
}

func (ip *ingressPlugin) OnJobDelete(job *batch.Job) error {
	klog.V(4).Info("ingress plugin OnJobDelete")
	if job.Status.ControlledResources["plugin-"+ip.Name()] != ip.Name() {
		return nil
	}

	if err := ip.Clientset.KubeClients.NetworkingV1().NetworkPolicies(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete Service of Job %v/%v: %v", job.Namespace, job.Name, err)
			return err
		}
	}

	if err := ip.Clientset.KubeClients.CoreV1().Services(job.Namespace).Delete(context.TODO(), ip.svcName(job), metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to delete Service of Job %v/%v: %v", job.Namespace, job.Name, err)
			return err
		}
	}
	delete(job.Status.ControlledResources, "plugin-"+ip.Name())

	return nil
}

func (ip *ingressPlugin) OnJobUpdate(job *batch.Job) error {
	klog.V(4).Info("ingress plugin OnJobUpdate")
	return nil
}

func (ip *ingressPlugin) createServiceIfNotExist(job *batch.Job) error {
	// If Service does not exist, create one for Job.
	if _, err := ip.Clientset.KubeClients.CoreV1().Services(job.Namespace).Get(context.TODO(), ip.svcName(job), metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get Service for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		svc := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: job.Namespace,
				Name:      ip.svcName(job),
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: v1.ServiceSpec{
				Selector: map[string]string{
					batch.JobNameKey:      job.Name,
					batch.JobNamespaceKey: job.Namespace,
				},
				Ports: []v1.ServicePort{
					{
						Name:       "http",
						Port:       80,
						TargetPort: intstr.FromInt(ip.svcPort),
					},
				},
				PublishNotReadyAddresses: ip.publishNotReadyAddresses,
			},
		}

		if _, e := ip.Clientset.KubeClients.CoreV1().Services(job.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create Service for Job <%s/%s>: %v", job.Namespace, job.Name, e)
			return e
		}
		job.Status.ControlledResources["plugin-"+ip.Name()] = ip.Name()
	}

	return nil
}

func (ip *ingressPlugin) createIngressIfNotExist(job *batch.Job) error {
	// If Ingress does not exist, create one for Job.
	if _, err := ip.Clientset.KubeClients.NetworkingV1().Ingresses(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.V(3).Infof("Failed to get Ingress for Job <%s/%s>: %v",
				job.Namespace, job.Name, err)
			return err
		}

		annotations := map[string]string{
			// "kubernetes.io/ingress.class": ip.ingressClass,
		}
		if ip.rewriteTarget != "" && ip.ingressClass == DefaultIngressClass {
			annotations["nginx.ingress.kubernetes.io/rewrite-target"] = ip.rewriteTarget
		}

		prefixPathType := networkingv1.PathType(ip.prefixPathType)
		ing := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   job.Namespace,
				Name:        job.Name,
				Annotations: annotations,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(job, helpers.JobKind),
				},
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &ip.ingressClass,
				Rules: []networkingv1.IngressRule{
					{
						Host: ip.ingressHost,
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{
										Path:     ip.ingressPath,
										PathType: &prefixPathType,
										Backend: networkingv1.IngressBackend{
											Service: &networkingv1.IngressServiceBackend{
												Name: ip.svcName(job),
												Port: networkingv1.ServiceBackendPort{
													Name: "http",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		if _, e := ip.Clientset.KubeClients.NetworkingV1().Ingresses(job.Namespace).Create(context.TODO(), ing, metav1.CreateOptions{}); e != nil {
			klog.V(3).Infof("Failed to create Ingress for Job <%s/%s>: %v", job.Namespace, job.Name, e)
			return e
		}

		job.Status.ControlledResources["plugin-"+ip.Name()] = ip.Name()
	}

	return nil
}

func (ip *ingressPlugin) svcName(job *batch.Job) string {
	return fmt.Sprintf("%s-%s", job.Name, ip.Name())
}
