package apis

import (
	"context"
	v1 "dynamic-project/pkg/apis/v1"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"time"
)

var stopCh chan struct{}

const controllerAgentName = "vm-dynamic-controller"

type Controller struct {
	dynamicClient dynamic.Interface
	workqueue     workqueue.RateLimitingInterface
	vmSynced      cache.InformerSynced
	vmInformer    cache.SharedIndexInformer
	vmLister      dynamiclister.Lister
	recorder      record.EventRecorder
}

func NewController(
	dynamicClient dynamic.Interface,
	factory dynamicinformer.DynamicSharedInformerFactory) *Controller {
	informer := factory.ForResource(v1.VMGVR).Informer()
	// 创建事件广播器
	eventBroadcaster := record.NewBroadcaster()
	// 将从广播器接收到的事件发送给日志记录器，进行日志记录
	eventBroadcaster.StartStructuredLogging(0)
	// 日志记录器，能够产生事件，并发送给广播器处理
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	// 只是封装起来事件广播器，并未使用，如果需要使用可以直接使用c.recorder.Event调用即可
	controller := &Controller{
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "virtualmachines"),
		vmSynced:      informer.HasSynced,
		vmInformer:    informer,
		vmLister:      dynamiclister.New(informer.GetIndexer(), v1.VMGVR),
		recorder:      recorder,
		dynamicClient: dynamicClient,
	}
	// 添加回调事件
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.enqueueFoo(obj)
		},
	})

	return controller
}

func (c *Controller) enqueueFoo(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) Run() error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	go c.vmInformer.Run(stopCh)
	// 启动worker,每个worker一个goroutine
	go wait.Until(c.runWorker, time.Second, stopCh)
	stopCh = make(chan struct{})
	// 等待cache同步
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.vmSynced); !ok {
		klog.Errorln("failed to wait for caches to sync")
		return nil
	}
	// 等待退出信号
	<-stopCh
	klog.Infoln("Shutting down workers")
	return nil
}

// worker就是一个循环不断调用processNextWorkItem
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	// 从工作队列获取对象
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			// 处理失败再次加入队列
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// 处理成功不入队
		c.workqueue.Forget(obj)
		klog.Infoln("Successfully synced '%s'", key)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return err
	}
	unStruct, err := c.vmLister.Namespace(namespace).Get(name)
	newBytes, err := json.Marshal(unStruct)
	if err != nil {
		utilruntime.HandleError(err)
		return err
	}
	vm := &v1.VirtualMachine{}
	if err = json.Unmarshal(newBytes, vm); err != nil {
		utilruntime.HandleError(err)
		return err
	}
	if vm.Spec.Status == "creating" {
		err = c.Creating(vm)
		if err != nil {
			utilruntime.HandleError(err)
			return err
		}
	}
	return nil
}

func (c *Controller) Creating(vm *v1.VirtualMachine) error {
	// 假设虚拟机都可以创建成功，不成功的话就直接更新为fail啥的就行了，或者返回err重新加入队列当中，不断地去进行创建操作
	patchData := []byte(`{"spec": {"status": "complete"}}`)
	// patch更新CR
	_, err := c.dynamicClient.Resource(v1.VMGVR).Namespace("default").Patch(context.Background(),vm.Name,types.MergePatchType,patchData,metav1.PatchOptions{})
	if err != nil {
		klog.Errorln(err)
		return err
	}
	return nil
}
