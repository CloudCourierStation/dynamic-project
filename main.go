package main

import (
	"dynamic-project/pkg/apis"
	"flag"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog"
	"path/filepath"
)

func main() {
	var err error
	var config *rest.Config
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(可选) kubeconfig 文件的绝对路径")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "kubeconfig 文件的绝对路径")
	}
	flag.Parse()
	if config, err = rest.InClusterConfig(); err != nil {
		// 使用 KubeConfig 文件创建集群配置 Config 对象
		if config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig); err != nil {
			panic(err.Error())
		}
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return
	}
	dynamicSharedInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 0, "default", nil)
	// 初始化controller，传入client、informer,
	controller := apis.NewController(dynamicClient, dynamicSharedInformerFactory)
	// 直接启动controller
	err = controller.Run()
	if err != nil {
		klog.Errorln("fail run controller")
		return
	}
}
